package artifactindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/runtimeclient"
	"github.com/google/uuid"
)

const Dimensions = 768

const embeddingBatchSize = 64

type RuntimeEmbedder struct {
	client *runtimeclient.Client
	quota  quotas.QuotaPort
	ledger quotas.UsageLedgerPort
}

func NewRuntimeEmbedder(client *runtimeclient.Client) *RuntimeEmbedder {
	return &RuntimeEmbedder{client: client}
}

func (e *RuntimeEmbedder) SetQuotaPorts(quota quotas.QuotaPort, ledger quotas.UsageLedgerPort) {
	e.quota, e.ledger = quota, ledger
}

func (e *RuntimeEmbedder) Embed(ctx context.Context, scope artifacts.Scope, chunks []artifacts.Chunk) ([]artifacts.Embedding, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("runtime embedder is not configured")
	}
	idempotencyKey, units := embeddingReservation(chunks)
	if err := e.consume(ctx, scope, idempotencyKey, units); err != nil {
		return nil, err
	}
	out := make([]artifacts.Embedding, 0, len(chunks))
	model := ""
	for start := 0; start < len(chunks); start += embeddingBatchSize {
		end := start + embeddingBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		texts := make([]string, 0, end-start)
		for _, chunk := range chunks[start:end] {
			texts = append(texts, chunk.Text)
		}
		result, err := e.client.Embed(ctx, runtimeclient.EmbedRequest{Texts: texts, TaskType: runtimeclient.EmbeddingTaskDocument})
		if err != nil {
			return nil, err
		}
		if result.Dimensions != Dimensions || len(result.Embeddings) != len(texts) || (model != "" && model != result.Model) {
			return nil, errors.New("runtime returned invalid embedding shape")
		}
		model = result.Model
		for i, values := range result.Embeddings {
			if len(values) != Dimensions {
				return nil, errors.New("runtime returned invalid embedding dimensions")
			}
			out = append(out, artifacts.Embedding{ChunkID: chunks[start+i].ID, Values: values, Model: result.Model})
		}
	}
	e.record(ctx, scope, idempotencyKey, units, model)
	return out, nil
}

func (e *RuntimeEmbedder) EmbedQuery(ctx context.Context, scope artifacts.Scope, text string) ([]float32, string, error) {
	if e == nil || e.client == nil {
		return nil, "", errors.New("runtime embedder is not configured")
	}
	idempotencyKey := uuid.NewString()
	units := estimatedEmbeddingTokens(text)
	if err := e.consume(ctx, scope, idempotencyKey, units); err != nil {
		return nil, "", err
	}
	result, err := e.client.Embed(ctx, runtimeclient.EmbedRequest{Texts: []string{text}, TaskType: runtimeclient.EmbeddingTaskQuery})
	if err != nil {
		return nil, "", err
	}
	if result.Dimensions != Dimensions || len(result.Embeddings) != 1 || len(result.Embeddings[0]) != Dimensions {
		return nil, "", errors.New("runtime returned invalid query embedding shape")
	}
	e.record(ctx, scope, idempotencyKey, units, result.Model)
	return result.Embeddings[0], result.Model, nil
}

func (e *RuntimeEmbedder) consume(ctx context.Context, scope artifacts.Scope, idempotencyKey string, units int64) error {
	if e.quota == nil {
		return nil
	}
	_, err := e.quota.Consume(ctx, quotas.ConsumeRequest{
		Key:            quotas.Key{TenantID: scope.TenantID, ProductSurface: scope.ProductSurface, Area: quotas.AreaEmbeddings},
		IdempotencyKey: idempotencyKey, SubjectType: "repository_generation", SubjectID: scope.RepositoryGeneration, Units: units,
	})
	return err
}

func (e *RuntimeEmbedder) record(ctx context.Context, scope artifacts.Scope, idempotencyKey string, units int64, model string) {
	if e.ledger == nil {
		return
	}
	_ = e.ledger.RecordUsage(ctx, quotas.Usage{
		Key:            quotas.Key{TenantID: scope.TenantID, ProductSurface: scope.ProductSurface, Area: quotas.AreaEmbeddings},
		IdempotencyKey: idempotencyKey + ":actual", SubjectType: "repository_generation", SubjectID: scope.RepositoryGeneration,
		Units: units, Model: model, Metadata: map[string]any{"estimated": true},
	})
}

func embeddingReservation(chunks []artifacts.Chunk) (string, int64) {
	hash := sha256.New()
	var units int64
	for _, chunk := range chunks {
		_, _ = hash.Write([]byte(chunk.ID))
		_, _ = hash.Write([]byte{0})
		units += estimatedEmbeddingTokens(chunk.Text)
	}
	return hex.EncodeToString(hash.Sum(nil)), units
}

func estimatedEmbeddingTokens(value string) int64 {
	length := int64(len(strings.TrimSpace(value)))
	if length == 0 {
		return 0
	}
	return (length + 3) / 4
}
