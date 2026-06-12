package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

// Repository define el port de persistencia para audit trail (append-only).
type Repository interface {
	Append(ctx context.Context, e auditdomain.RequestEvent) error
	ListByRequestID(ctx context.Context, requestID uuid.UUID) ([]auditdomain.RequestEvent, error)
}

// --- Implementación PostgreSQL ---

type PostgresRepository struct {
	db           *sharedpostgres.DB
	signingKey   []byte
	signingKeyID string
}

type Option func(*PostgresRepository)

func WithSigner(key, keyID string) Option {
	return func(r *PostgresRepository) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		r.signingKey = []byte(key)
		r.signingKeyID = strings.TrimSpace(keyID)
		if r.signingKeyID == "" {
			r.signingKeyID = "default"
		}
	}
}

func NewPostgresRepository(db *sharedpostgres.DB, opts ...Option) *PostgresRepository {
	r := &PostgresRepository{db: db}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *PostgresRepository) Append(ctx context.Context, e auditdomain.RequestEvent) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	e.CreatedAt = e.CreatedAt.UTC().Truncate(time.Microsecond)
	if e.Data == nil {
		e.Data = make(map[string]any)
	}
	if e.ChainScope == "" {
		e.ChainScope = e.RequestID.String()
	}
	tx, err := r.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin audit append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, e.ChainScope); err != nil {
		return fmt.Errorf("lock audit chain: %w", err)
	}
	if e.PreviousHash == "" {
		_ = tx.QueryRow(ctx, `
			SELECT event_hash FROM request_events
			WHERE chain_scope = $1 AND event_hash IS NOT NULL
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, e.ChainScope).Scan(&e.PreviousHash)
	}
	payloadHash, eventHash, signature, err := r.sealEvent(e)
	if err != nil {
		return err
	}
	e.PayloadHash = payloadHash
	e.EventHash = eventHash
	e.Signature = signature
	if len(r.signingKey) > 0 && e.SignatureKeyID == "" {
		e.SignatureKeyID = r.signingKeyID
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO request_events (
			id, request_id, event_type, actor_type, actor_id, summary, data, created_at,
			chain_scope, previous_hash, payload_hash, event_hash, signature_key_id, signature
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`, e.ID, e.RequestID, e.EventType, e.ActorType, e.ActorID, e.Summary, e.Data, e.CreatedAt,
		e.ChainScope, emptyToNil(e.PreviousHash), e.PayloadHash, e.EventHash, emptyToNil(e.SignatureKeyID), emptyToNil(e.Signature))
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit audit append: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListByRequestID(ctx context.Context, requestID uuid.UUID) ([]auditdomain.RequestEvent, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, request_id, event_type, actor_type, actor_id, summary, data, created_at,
		       chain_scope, previous_hash, payload_hash, event_hash, signature_key_id, signature
		FROM request_events WHERE request_id = $1
		ORDER BY created_at ASC, id ASC
	`, requestID)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	out := make([]auditdomain.RequestEvent, 0)
	for rows.Next() {
		var e auditdomain.RequestEvent
		var chainScope, previousHash, payloadHash, eventHash, signatureKeyID, signature *string
		if err := rows.Scan(
			&e.ID, &e.RequestID, &e.EventType, &e.ActorType, &e.ActorID, &e.Summary, &e.Data, &e.CreatedAt,
			&chainScope, &previousHash, &payloadHash, &eventHash, &signatureKeyID, &signature,
		); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		e.ChainScope = stringFromPtr(chainScope)
		e.PreviousHash = stringFromPtr(previousHash)
		e.PayloadHash = stringFromPtr(payloadHash)
		e.EventHash = stringFromPtr(eventHash)
		e.SignatureKeyID = stringFromPtr(signatureKeyID)
		e.Signature = stringFromPtr(signature)
		out = append(out, e)
	}
	return out, rows.Err()
}

type IntegrityCheck struct {
	ID             uuid.UUID
	Scope          string
	ScopeID        string
	Status         string
	CheckedEvents  int
	FirstEventHash string
	LastEventHash  string
	ErrorMessage   string
	CheckedAt      time.Time
}

func (r *PostgresRepository) RecordIntegrityCheck(ctx context.Context, check IntegrityCheck) error {
	if check.ID == uuid.Nil {
		check.ID = uuid.New()
	}
	if check.CheckedAt.IsZero() {
		check.CheckedAt = time.Now().UTC()
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO audit_integrity_checks
			(id, scope, scope_id, status, checked_events, first_event_hash, last_event_hash, error_message, checked_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, check.ID, check.Scope, check.ScopeID, check.Status, check.CheckedEvents,
		emptyToNil(check.FirstEventHash), emptyToNil(check.LastEventHash), emptyToNil(check.ErrorMessage), check.CheckedAt)
	if err != nil {
		return fmt.Errorf("record audit integrity check: %w", err)
	}
	return nil
}

func (r *PostgresRepository) VerifySignatures(events []auditdomain.RequestEvent) error {
	if len(r.signingKey) == 0 {
		return nil
	}
	for _, event := range events {
		if strings.TrimSpace(event.Signature) == "" {
			return fmt.Errorf("audit event %s is missing signature", event.ID)
		}
		mac := hmac.New(sha256.New, r.signingKey)
		_, _ = mac.Write([]byte(event.EventHash))
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(event.Signature)) {
			return fmt.Errorf("audit event %s signature mismatch", event.ID)
		}
	}
	return nil
}

func (r *PostgresRepository) sealEvent(e auditdomain.RequestEvent) (payloadHash, eventHash, signature string, err error) {
	payloadHash, err = ComputePayloadHash(e)
	if err != nil {
		return "", "", "", err
	}
	eventHash, err = ComputeEventHash(e, payloadHash)
	if err != nil {
		return "", "", "", err
	}
	if len(r.signingKey) == 0 {
		return payloadHash, eventHash, "", nil
	}
	mac := hmac.New(sha256.New, r.signingKey)
	_, _ = mac.Write([]byte(eventHash))
	signature = hex.EncodeToString(mac.Sum(nil))
	return payloadHash, eventHash, signature, nil
}

func ComputePayloadHash(e auditdomain.RequestEvent) (string, error) {
	raw, err := json.Marshal(eventPayload(e))
	if err != nil {
		return "", fmt.Errorf("marshal audit event payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func ComputeEventHash(e auditdomain.RequestEvent, payloadHash string) (string, error) {
	chainMaterial := map[string]any{
		"chain_scope":   e.ChainScope,
		"event_id":      e.ID.String(),
		"payload_hash":  payloadHash,
		"previous_hash": e.PreviousHash,
	}
	chainRaw, err := json.Marshal(chainMaterial)
	if err != nil {
		return "", fmt.Errorf("marshal audit event chain: %w", err)
	}
	sum := sha256.Sum256(chainRaw)
	return hex.EncodeToString(sum[:]), nil
}

func eventPayload(e auditdomain.RequestEvent) map[string]any {
	return map[string]any{
		"id":         e.ID.String(),
		"request_id": e.RequestID.String(),
		"event_type": e.EventType,
		"actor_type": e.ActorType,
		"actor_id":   e.ActorID,
		"summary":    e.Summary,
		"data":       e.Data,
		"created_at": e.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
	}
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Compilar verifica que PostgresRepository implementa Repository.
var _ Repository = (*PostgresRepository)(nil)
