package memories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type EmbeddingPort interface {
	EmbedDocument(context.Context, string) ([]float32, string, error)
	EmbedQuery(context.Context, string) ([]float32, string, error)
}

type UseCases struct {
	repo     *Repository
	curator  MemoryCuratorPort
	embedder EmbeddingPort
}

func NewUseCases(repo *Repository) *UseCases {
	return &UseCases{repo: repo, curator: NewDefaultCurator(repo)}
}

func (u *UseCases) SetCurator(curator MemoryCuratorPort) {
	if curator != nil {
		u.curator = curator
	}
}

func (u *UseCases) SetEmbedder(embedder EmbeddingPort) { u.embedder = embedder }
func (u *UseCases) authorize(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string) error {
	return u.repo.Authorized(ctx, strings.TrimSpace(tenant), virployee, strings.TrimSpace(actor), strings.ToLower(strings.TrimSpace(role)))
}

// Authorize exposes the same per-virployee role gate the human write paths use
// (owner/admin, or the assigned supervisor), so other modules that install
// memories on a human's behalf (e.g. learning's accept) enforce it too.
func (u *UseCases) Authorize(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string) error {
	return u.authorize(ctx, tenant, virployee, actor, role)
}
func (u *UseCases) Create(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string, in CreateInput) (Memory, error) {
	tenant = strings.TrimSpace(tenant)
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Memory{}, err
	}
	in.Provenance, in.ActorID = "human", actor
	curated, err := u.curator.Curate(ctx, tenant, virployee, uuid.Nil, in)
	if err != nil {
		return Memory{}, err
	}
	m, err := u.repo.Create(ctx, tenant, virployee, curated)
	return redact(m, false), err
}

// CreateSystem is the internal-only write port. It is deliberately not exposed by Handler.
func (u *UseCases) CreateSystem(ctx context.Context, tenant string, virployee uuid.UUID, actor, source string, in CreateInput) (Memory, error) {
	tenant = strings.TrimSpace(tenant)
	in.Provenance, in.ActorID, in.SourceReference = "system", actor, source
	curated, err := u.curator.Curate(ctx, tenant, virployee, uuid.Nil, in)
	if err != nil {
		return Memory{}, err
	}
	return u.repo.Create(ctx, tenant, virployee, curated)
}
func (u *UseCases) Get(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role string) (Memory, error) {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Memory{}, err
	}
	m, err := u.repo.Get(ctx, tenant, virployee, id)
	return redact(m, true), err
}
func (u *UseCases) List(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string, in ListInput) (Page, error) {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Page{}, err
	}
	p, err := u.repo.List(ctx, tenant, virployee, in)
	if err != nil {
		return Page{}, err
	}
	for i := range p.Items {
		p.Items[i] = redact(p.Items[i], false)
	}
	return p, nil
}
func (u *UseCases) Update(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role string, in UpdateInput) (Memory, error) {
	tenant = strings.TrimSpace(tenant)
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Memory{}, err
	}
	in.ActorID = actor
	curated, err := u.curator.Curate(ctx, tenant, virployee, id, CreateInput{
		Title: in.Title, Type: in.Type, Content: in.Content, Sensitivity: in.Sensitivity,
		Provenance: "human", ActorID: actor,
	})
	if err != nil {
		return Memory{}, err
	}
	m, err := u.repo.Update(ctx, tenant, virployee, id, curated, in.ExpectedVersion)
	return redact(m, false), err
}
func (u *UseCases) Recall(ctx context.Context, tenant string, virployee uuid.UUID, actor, role, query string, limit int) ([]Reference, error) {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return nil, err
	}
	items, err := u.recall(ctx, tenant, virployee, query, limit)
	if err != nil {
		return nil, err
	}
	refs := make([]Reference, len(items))
	for i := range items {
		refs[i] = items[i].Reference
	}
	return refs, nil
}
func (u *UseCases) RecallInternal(ctx context.Context, tenant string, virployee uuid.UUID, query string, limit int) ([]Recalled, error) {
	return u.recall(ctx, tenant, virployee, query, limit)
}

func (u *UseCases) recall(ctx context.Context, tenant string, virployee uuid.UUID, query string, limit int) ([]Recalled, error) {
	if u.embedder == nil {
		return u.repo.Recall(ctx, strings.TrimSpace(tenant), virployee, query, limit, nil, "")
	}
	vector, model, err := u.embedder.EmbedQuery(ctx, query)
	if err != nil {
		// Recall degrades to tenant-scoped FTS; unsafe/unapproved memories remain
		// excluded by the repository in both paths.
		slog.WarnContext(ctx, "memory_query_embedding_failed_fallback_fts")
		return u.repo.Recall(ctx, strings.TrimSpace(tenant), virployee, query, limit, nil, "")
	}
	return u.repo.Recall(ctx, strings.TrimSpace(tenant), virployee, query, limit, vector, model)
}

func (u *UseCases) Review(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role, decision, note string) (Memory, error) {
	tenant = strings.TrimSpace(tenant)
	actor = strings.TrimSpace(actor)
	if actor == "" || strings.HasPrefix(strings.ToLower(actor), "service:") {
		return Memory{}, domainerr.Forbidden("memory review requires a human actor")
	}
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Memory{}, err
	}
	current, err := u.repo.Get(ctx, tenant, virployee, id)
	if err != nil {
		return Memory{}, err
	}
	if current.ReviewState == ReviewQuarantined && strings.TrimSpace(note) == "" {
		return Memory{}, domainerr.Validation("review note is required for quarantined memory")
	}
	memory, err := u.repo.Review(ctx, tenant, virployee, id, strings.TrimSpace(actor), strings.ToLower(strings.TrimSpace(decision)))
	return redact(memory, false), err
}

func (u *UseCases) IndexMemory(ctx context.Context, tenant string, id uuid.UUID, version int) (Memory, error) {
	if u.embedder == nil {
		return Memory{}, domainerr.Validation("memory embedder is not configured")
	}
	memory, err := u.repo.IndexCandidate(ctx, strings.TrimSpace(tenant), id, version)
	if err != nil {
		return Memory{}, err
	}
	vector, model, err := u.embedder.EmbedDocument(ctx, memory.Title+"\n"+memory.Content)
	if err != nil {
		return Memory{}, err
	}
	if err := u.repo.StoreEmbedding(ctx, strings.TrimSpace(tenant), memory, vector, model); err != nil {
		return Memory{}, err
	}
	memory.EmbeddingModel, memory.EmbeddingVersion = model, EmbeddingVersion
	return memory, nil
}

func (u *UseCases) DecayDue(ctx context.Context, limit int) (int64, error) {
	return u.repo.DecayDue(ctx, limit)
}
func (u *UseCases) Lifecycle(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role, action string) error {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return err
	}
	return u.repo.Lifecycle(ctx, tenant, virployee, id, action, actor)
}
func (u *UseCases) Purge(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role string) error {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return err
	}
	return u.repo.Purge(ctx, tenant, virployee, id, actor)
}
func ContextHash(refs []Reference) string {
	raw, _ := json.Marshal(refs)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
