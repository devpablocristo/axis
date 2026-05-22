package requests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	requestdomain "github.com/devpablocristo/nexus/governance/internal/requests/usecases/domain"
)

// ErrAttestationNotFound indica que no existe attestation para esa request.
var ErrAttestationNotFound = domainerr.NotFound("attestation not found")

// AttestationStore es el port para persistir y leer attestations.
type AttestationStore interface {
	Create(ctx context.Context, a requestdomain.Attestation) (requestdomain.Attestation, error)
	GetByRequestID(ctx context.Context, requestID uuid.UUID) (requestdomain.Attestation, error)
}

// PostgresAttestationStore implementa AttestationStore con PostgreSQL.
type PostgresAttestationStore struct {
	pool *pgxpool.Pool
}

// NewPostgresAttestationStore crea un nuevo store de attestations.
func NewPostgresAttestationStore(pool *pgxpool.Pool) *PostgresAttestationStore {
	return &PostgresAttestationStore{pool: pool}
}

// Create persiste una nueva attestation.
func (s *PostgresAttestationStore) Create(ctx context.Context, a requestdomain.Attestation) (requestdomain.Attestation, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.ProviderRefs == nil {
		a.ProviderRefs = make(map[string]any)
	}
	if a.Metadata == nil {
		a.Metadata = make(map[string]any)
	}

	providerRefsJSON, err := json.Marshal(a.ProviderRefs)
	if err != nil {
		return requestdomain.Attestation{}, fmt.Errorf("marshal provider_refs: %w", err)
	}
	metadataJSON, err := json.Marshal(a.Metadata)
	if err != nil {
		return requestdomain.Attestation{}, fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO attestations (id, request_id, status, provider_refs, signature, attester, metadata, created_at, verified, verification_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, a.ID, a.RequestID, a.Status, providerRefsJSON, a.Signature, a.Attester, metadataJSON, a.CreatedAt, a.Verified, a.VerificationError)
	if err != nil {
		return requestdomain.Attestation{}, fmt.Errorf("insert attestation: %w", err)
	}
	return a, nil
}

// GetByRequestID obtiene la attestation de una request.
func (s *PostgresAttestationStore) GetByRequestID(ctx context.Context, requestID uuid.UUID) (requestdomain.Attestation, error) {
	var a requestdomain.Attestation
	var providerRefsJSON, metadataJSON []byte

	err := s.pool.QueryRow(ctx, `
		SELECT id, request_id, status, provider_refs, signature, attester, metadata, created_at, verified, verification_error
		FROM attestations WHERE request_id = $1
	`, requestID).Scan(
		&a.ID, &a.RequestID, &a.Status, &providerRefsJSON, &a.Signature, &a.Attester, &metadataJSON, &a.CreatedAt,
		&a.Verified, &a.VerificationError,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return requestdomain.Attestation{}, ErrAttestationNotFound
		}
		return requestdomain.Attestation{}, fmt.Errorf("get attestation: %w", err)
	}

	if len(providerRefsJSON) > 0 {
		if err := json.Unmarshal(providerRefsJSON, &a.ProviderRefs); err != nil {
			return requestdomain.Attestation{}, fmt.Errorf("unmarshal provider_refs: %w", err)
		}
	}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &a.Metadata); err != nil {
			return requestdomain.Attestation{}, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return a, nil
}

// Compilar verifica la interfaz.
var _ AttestationStore = (*PostgresAttestationStore)(nil)
