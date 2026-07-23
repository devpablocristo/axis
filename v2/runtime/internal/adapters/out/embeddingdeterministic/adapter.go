package embeddingdeterministic

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"unicode"

	"github.com/devpablocristo/runtime-v2/internal/embeddings"
)

const DeterministicDevelopmentModel = "axis-deterministic-development-v1"

// Deterministic is a local-development embedding provider. It uses signed
// feature hashing over normalized tokens, so equal terms produce comparable
// vectors without a network dependency. It is intentionally not a production
// model; wire only selects it outside production when explicitly enabled.
type Adapter struct{ dimensions int }

func New(dimensions int) (*Adapter, error) {
	if dimensions <= 0 {
		return nil, errors.New("deterministic embedding dimensions must be positive")
	}
	return &Adapter{dimensions: dimensions}, nil
}

func (d *Adapter) Model() string   { return DeterministicDevelopmentModel }
func (d *Adapter) Dimensions() int { return d.dimensions }

func (d *Adapter) Embed(_ context.Context, request embeddings.EmbeddingRequest) ([]float32, error) {
	if request.TaskType != embeddings.TaskDocument && request.TaskType != embeddings.TaskQuery {
		return nil, errors.New("unsupported embedding task type")
	}
	tokens := tokenize(request.Text)
	if len(tokens) == 0 {
		return nil, errors.New("embedding text is required")
	}
	values := make([]float32, d.dimensions)
	for _, token := range tokens {
		digest := sha256.Sum256([]byte(token))
		index := binary.BigEndian.Uint64(digest[:8]) % uint64(d.dimensions)
		weight := float32(1)
		if digest[8]&1 == 1 {
			weight = -1
		}
		values[index] += weight
	}
	var normSquared float64
	for _, value := range values {
		normSquared += float64(value * value)
	}
	if normSquared == 0 {
		return nil, errors.New("embedding text produced no features")
	}
	norm := float32(math.Sqrt(normSquared))
	for index := range values {
		values[index] /= norm
	}
	return values, nil
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(strings.TrimSpace(text)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}
