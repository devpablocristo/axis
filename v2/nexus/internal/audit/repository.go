package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const auditEventColumns = `
	id, org_id, chain_scope, virployee_id, subject_type, subject_id,
	event_type, actor_type, actor_id, summary, data, created_at,
	previous_hash, payload_hash, event_hash, signature_key_id, signature`

// Repository is the append-only persistence port for the audit ledger.
type Repository struct {
	pool         *pgxpool.Pool
	signingKey   []byte
	signingKeyID string
}

type Option func(*Repository)

// WithSigner enables HMAC-SHA256 signing of every event. When the key is empty
// the ledger still chains + hashes events, it just leaves the signature blank
// (local-first: production sets NEXUS_V2_SIGNING_KEY via Secret Manager).
func WithSigner(key, keyID string) Option {
	return func(r *Repository) {
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

func NewRepository(pool *pgxpool.Pool, opts ...Option) *Repository {
	r := &Repository{pool: pool}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Signed reports whether this repository seals events with a signature.
func (r *Repository) Signed() bool { return len(r.signingKey) > 0 }

// Append seals an event (payload hash → chained event hash → optional signature)
// and inserts it. A per-scope advisory lock serializes concurrent appends to the
// same chain so PreviousHash always points at the true tip.
func (r *Repository) Append(ctx context.Context, e auditdomain.AuditEvent) (auditdomain.AuditEvent, error) {
	idempotent := e.ID != uuid.Nil
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Data == nil {
		e.Data = make(map[string]any)
	}
	if e.ChainScope == "" {
		e.ChainScope = auditdomain.ChainScopeFor(e.OrgID, e.VirployeeID)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return auditdomain.AuditEvent{}, fmt.Errorf("begin audit append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if idempotent {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "audit-idempotency:"+e.ID.String()); err != nil {
			return auditdomain.AuditEvent{}, fmt.Errorf("lock audit idempotency key: %w", err)
		}
		existing, err := scanAuditEvent(tx.QueryRow(ctx, `SELECT `+auditEventColumns+` FROM audit_events WHERE id=$1`, e.ID))
		switch {
		case err == nil && sameAuditAppendRequest(existing, e):
			return existing, nil
		case err == nil:
			return auditdomain.AuditEvent{}, domainerr.Conflict("Idempotency-Key was already used for a different audit event")
		case !errors.Is(err, pgx.ErrNoRows):
			return auditdomain.AuditEvent{}, fmt.Errorf("read idempotent audit event: %w", err)
		}
	}

	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	e.CreatedAt = e.CreatedAt.UTC().Truncate(time.Microsecond)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, e.ChainScope); err != nil {
		return auditdomain.AuditEvent{}, fmt.Errorf("lock audit chain: %w", err)
	}
	if e.PreviousHash == "" {
		_ = tx.QueryRow(ctx, `
			SELECT event_hash FROM audit_events
			WHERE chain_scope = $1 AND event_hash IS NOT NULL
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, e.ChainScope).Scan(&e.PreviousHash)
	}

	payloadHash, eventHash, signature, err := r.sealEvent(e)
	if err != nil {
		return auditdomain.AuditEvent{}, err
	}
	e.PayloadHash = payloadHash
	e.EventHash = eventHash
	e.Signature = signature
	if len(r.signingKey) > 0 {
		e.SignatureKeyID = r.signingKeyID
	}

	data, err := json.Marshal(e.Data)
	if err != nil {
		return auditdomain.AuditEvent{}, fmt.Errorf("marshal audit data: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, org_id, chain_scope, virployee_id, subject_type, subject_id,
			event_type, actor_type, actor_id, summary, data, created_at,
			previous_hash, payload_hash, event_hash, signature_key_id, signature
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15,$16,$17)
	`, e.ID, e.OrgID, e.ChainScope, e.VirployeeID, e.SubjectType, e.SubjectID,
		e.EventType, e.ActorType, e.ActorID, e.Summary, data, e.CreatedAt,
		emptyToNil(e.PreviousHash), e.PayloadHash, e.EventHash, emptyToNil(e.SignatureKeyID), emptyToNil(e.Signature))
	if err != nil {
		return auditdomain.AuditEvent{}, fmt.Errorf("append audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return auditdomain.AuditEvent{}, fmt.Errorf("commit audit append: %w", err)
	}
	return e, nil
}

// ListByScope returns every event in a chain, oldest first (chain order).
func (r *Repository) ListByScope(ctx context.Context, chainScope string) ([]auditdomain.AuditEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+auditEventColumns+`
		FROM audit_events WHERE chain_scope = $1
		ORDER BY created_at ASC, id ASC
	`, chainScope)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	out := make([]auditdomain.AuditEvent, 0)
	for rows.Next() {
		e, err := scanAuditEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListVirployeeIDsBySubject locates every independent ledger chain that
// contributed to one subject. It returns identifiers only; callers still use
// Replay so each chain is independently hash/signature verified.
func (r *Repository) ListVirployeeIDsBySubject(ctx context.Context, orgID, subjectID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT virployee_id
		FROM audit_events
		WHERE org_id=$1 AND subject_id=$2
		ORDER BY virployee_id`, strings.TrimSpace(orgID), strings.TrimSpace(subjectID))
	if err != nil {
		return nil, fmt.Errorf("list audit subject chains: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var virployeeID string
		if err := rows.Scan(&virployeeID); err != nil {
			return nil, fmt.Errorf("scan audit subject chain: %w", err)
		}
		out = append(out, virployeeID)
	}
	return out, rows.Err()
}

// VerifySignatures checks the HMAC signature of every event. A no-op when no
// signing key is configured; when it is, a missing or mismatched signature fails.
func (r *Repository) VerifySignatures(events []auditdomain.AuditEvent) error {
	if len(r.signingKey) == 0 {
		return nil
	}
	for _, event := range events {
		signature := strings.TrimSpace(event.Signature)
		keyID := strings.TrimSpace(event.SignatureKeyID)
		if signature == "" && keyID == "" {
			// Signing is optional and can be enabled after a chain already exists.
			// Historical unsigned entries remain protected by the append-only hash
			// chain; only entries that claim a signature must authenticate.
			continue
		}
		if signature == "" || keyID == "" {
			return fmt.Errorf("audit event %s is missing signature", event.ID)
		}
		mac := hmac.New(sha256.New, r.signingKey)
		_, _ = mac.Write([]byte(event.EventHash))
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(signature)) {
			return fmt.Errorf("audit event %s signature mismatch", event.ID)
		}
	}
	return nil
}

func (r *Repository) sealEvent(e auditdomain.AuditEvent) (payloadHash, eventHash, signature string, err error) {
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

// ComputePayloadHash is SHA-256 over the canonical content of the event (not the
// chain material). Any edit to the recorded content changes this hash.
func ComputePayloadHash(e auditdomain.AuditEvent) (string, error) {
	raw, err := json.Marshal(eventPayload(e))
	if err != nil {
		return "", fmt.Errorf("marshal audit event payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ComputeEventHash is SHA-256 over the chain material: it binds the event to its
// scope and to the previous event, forming the tamper-evident chain.
func ComputeEventHash(e auditdomain.AuditEvent, payloadHash string) (string, error) {
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

func eventPayload(e auditdomain.AuditEvent) map[string]any {
	return map[string]any{
		"id":           e.ID.String(),
		"org_id":       e.OrgID,
		"virployee_id": e.VirployeeID,
		"subject_type": e.SubjectType,
		"subject_id":   e.SubjectID,
		"event_type":   e.EventType,
		"actor_type":   e.ActorType,
		"actor_id":     e.ActorID,
		"summary":      e.Summary,
		"data":         e.Data,
		"created_at":   e.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
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

type rowScanner interface{ Scan(...any) error }

func scanAuditEvent(row rowScanner) (auditdomain.AuditEvent, error) {
	var e auditdomain.AuditEvent
	var data []byte
	var subjectType, subjectID, previousHash, payloadHash, eventHash, signatureKeyID, signature *string
	err := row.Scan(
		&e.ID, &e.OrgID, &e.ChainScope, &e.VirployeeID, &subjectType, &subjectID,
		&e.EventType, &e.ActorType, &e.ActorID, &e.Summary, &data, &e.CreatedAt,
		&previousHash, &payloadHash, &eventHash, &signatureKeyID, &signature,
	)
	if err != nil {
		return auditdomain.AuditEvent{}, err
	}
	e.SubjectType = stringFromPtr(subjectType)
	e.SubjectID = stringFromPtr(subjectID)
	e.PreviousHash = stringFromPtr(previousHash)
	e.PayloadHash = stringFromPtr(payloadHash)
	e.EventHash = stringFromPtr(eventHash)
	e.SignatureKeyID = stringFromPtr(signatureKeyID)
	e.Signature = stringFromPtr(signature)
	if len(data) > 0 {
		if err := json.Unmarshal(data, &e.Data); err != nil {
			return auditdomain.AuditEvent{}, fmt.Errorf("unmarshal audit data: %w", err)
		}
	}
	if e.Data == nil {
		e.Data = make(map[string]any)
	}
	return e, nil
}

func sameAuditAppendRequest(existing, requested auditdomain.AuditEvent) bool {
	if existing.OrgID != requested.OrgID || existing.ChainScope != requested.ChainScope ||
		existing.VirployeeID != requested.VirployeeID || existing.SubjectType != requested.SubjectType ||
		existing.SubjectID != requested.SubjectID || existing.EventType != requested.EventType ||
		existing.ActorType != requested.ActorType || existing.ActorID != requested.ActorID ||
		existing.Summary != requested.Summary {
		return false
	}
	existingData, existingErr := json.Marshal(existing.Data)
	requestedData, requestedErr := json.Marshal(requested.Data)
	return existingErr == nil && requestedErr == nil && string(existingData) == string(requestedData)
}
