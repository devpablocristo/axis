package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

// ---------------------------------------------------------------------------
// agent_conversations / agent_conversation_messages / agent_memory_facts /
// agent_user_profiles — persistencia para la migración pymes-ai → Companion.
//
// Filosofía: cada método es atómico (Upsert + Get + Find básicos). Las
// usecases componen flujos más complejos. Repository nunca decide reglas
// de negocio.
// ---------------------------------------------------------------------------

// CreateAgentConversation persiste una nueva conversación y devuelve la fila
// con id/created_at/updated_at poblados.
func (r *PostgresRepository) CreateAgentConversation(ctx context.Context, in domain.AgentConversation) (domain.AgentConversation, error) {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if in.Source == "" {
		in.Source = domain.AgentSourceCompanionNative
	}
	if len(in.MetadataJSON) == 0 {
		in.MetadataJSON = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	in.CreatedAt = now
	in.UpdatedAt = now

	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO agent_conversations
			(id, org_id, user_id, product_surface, title, summary, source, metadata_json, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, in.ID, in.OrgID, in.UserID, in.ProductSurface, in.Title, in.Summary, in.Source, in.MetadataJSON, in.CreatedAt, in.UpdatedAt)
	if err != nil {
		return domain.AgentConversation{}, fmt.Errorf("insert agent_conversation: %w", err)
	}
	return in, nil
}

// GetAgentConversation devuelve la conversación por id, o pgx.ErrNoRows si no
// existe. El caller debe validar org_id contra su identity.
func (r *PostgresRepository) GetAgentConversation(ctx context.Context, id uuid.UUID) (domain.AgentConversation, error) {
	var c domain.AgentConversation
	err := r.db.Pool().QueryRow(ctx, `
		SELECT id, org_id, user_id, product_surface, title, summary, source, metadata_json, created_at, updated_at
		FROM agent_conversations WHERE id = $1
	`, id).Scan(&c.ID, &c.OrgID, &c.UserID, &c.ProductSurface, &c.Title, &c.Summary, &c.Source, &c.MetadataJSON, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return domain.AgentConversation{}, err
	}
	return c, nil
}

// ListAgentConversations devuelve hasta `limit` conversaciones recientes de un
// (org, user). Si userID está vacío filtra solo por org. Sirve para que el
// chat reanude historiales y para la console.
func (r *PostgresRepository) ListAgentConversations(ctx context.Context, orgID, userID string, limit int) ([]domain.AgentConversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	query := `
		SELECT id, org_id, user_id, product_surface, title, summary, source, metadata_json, created_at, updated_at
		FROM agent_conversations
		WHERE org_id = $1`
	args := []any{orgID}
	if userID != "" {
		query += ` AND user_id = $2`
		args = append(args, userID)
	}
	query += ` ORDER BY updated_at DESC LIMIT ` + fmt.Sprint(limit)
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agent_conversations: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentConversation
	for rows.Next() {
		var c domain.AgentConversation
		if err := rows.Scan(&c.ID, &c.OrgID, &c.UserID, &c.ProductSurface, &c.Title, &c.Summary, &c.Source, &c.MetadataJSON, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan agent_conversation: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AppendAgentMessage agrega un mensaje a una conversación existente y bumpea
// el updated_at del padre. Es la operación más común del chat flow.
func (r *PostgresRepository) AppendAgentMessage(ctx context.Context, in domain.AgentConversationMessage) (domain.AgentConversationMessage, error) {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if len(in.MetadataJSON) == 0 {
		in.MetadataJSON = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	in.CreatedAt = now

	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return domain.AgentConversationMessage{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_conversation_messages
			(id, conversation_id, org_id, role, content, metadata_json, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, in.ID, in.ConversationID, in.OrgID, in.Role, in.Content, in.MetadataJSON, in.CreatedAt); err != nil {
		return domain.AgentConversationMessage{}, fmt.Errorf("insert agent_message: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_conversations SET updated_at = $1 WHERE id = $2`, now, in.ConversationID); err != nil {
		return domain.AgentConversationMessage{}, fmt.Errorf("touch agent_conversation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AgentConversationMessage{}, fmt.Errorf("commit append: %w", err)
	}
	return in, nil
}

// ListAgentMessages devuelve mensajes de una conversación en orden cronológico.
func (r *PostgresRepository) ListAgentMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]domain.AgentConversationMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, conversation_id, org_id, role, content, metadata_json, created_at
		FROM agent_conversation_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
		LIMIT `+fmt.Sprint(limit), conversationID)
	if err != nil {
		return nil, fmt.Errorf("list agent_messages: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentConversationMessage
	for rows.Next() {
		var m domain.AgentConversationMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.OrgID, &m.Role, &m.Content, &m.MetadataJSON, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan agent_message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpsertAgentMemoryFact upserta un fact por (org_id, fact_key). La fila
// ganadora preserva el created_at y actualiza el resto.
func (r *PostgresRepository) UpsertAgentMemoryFact(ctx context.Context, in domain.AgentMemoryFact) (domain.AgentMemoryFact, error) {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if in.Confidence == 0 {
		in.Confidence = 1.0
	}
	if in.Source == "" {
		in.Source = domain.AgentSourceCompanionNative
	}
	if len(in.FactValue) == 0 {
		in.FactValue = json.RawMessage(`{}`)
	}
	if len(in.MetadataJSON) == 0 {
		in.MetadataJSON = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	in.UpdatedAt = now

	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO agent_memory_facts
			(id, org_id, conversation_id, fact_key, fact_value, confidence, source, metadata_json, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (org_id, fact_key) DO UPDATE
		SET fact_value    = EXCLUDED.fact_value,
		    confidence    = EXCLUDED.confidence,
		    conversation_id = EXCLUDED.conversation_id,
		    metadata_json = EXCLUDED.metadata_json,
		    source        = EXCLUDED.source,
		    updated_at    = EXCLUDED.updated_at
		RETURNING id, created_at, updated_at
	`, in.ID, in.OrgID, in.ConversationID, in.FactKey, in.FactValue, in.Confidence, in.Source, in.MetadataJSON, now, now).
		Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt)
	if err != nil {
		return domain.AgentMemoryFact{}, fmt.Errorf("upsert agent_memory_fact: %w", err)
	}
	return in, nil
}

// GetAgentMemoryFact devuelve un fact por (org_id, fact_key).
func (r *PostgresRepository) GetAgentMemoryFact(ctx context.Context, orgID, factKey string) (domain.AgentMemoryFact, error) {
	var f domain.AgentMemoryFact
	err := r.db.Pool().QueryRow(ctx, `
		SELECT id, org_id, conversation_id, fact_key, fact_value, confidence, source, metadata_json, created_at, updated_at
		FROM agent_memory_facts WHERE org_id = $1 AND fact_key = $2
	`, orgID, factKey).Scan(&f.ID, &f.OrgID, &f.ConversationID, &f.FactKey, &f.FactValue, &f.Confidence, &f.Source, &f.MetadataJSON, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return domain.AgentMemoryFact{}, err
	}
	return f, nil
}

// ListAgentMemoryFacts devuelve hasta `limit` facts del tenant ordenados por
// updated_at desc.
func (r *PostgresRepository) ListAgentMemoryFacts(ctx context.Context, orgID string, limit int) ([]domain.AgentMemoryFact, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, org_id, conversation_id, fact_key, fact_value, confidence, source, metadata_json, created_at, updated_at
		FROM agent_memory_facts
		WHERE org_id = $1
		ORDER BY updated_at DESC
		LIMIT `+fmt.Sprint(limit), orgID)
	if err != nil {
		return nil, fmt.Errorf("list agent_memory_facts: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentMemoryFact
	for rows.Next() {
		var f domain.AgentMemoryFact
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ConversationID, &f.FactKey, &f.FactValue, &f.Confidence, &f.Source, &f.MetadataJSON, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan agent_memory_fact: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpsertAgentUserProfile crea o actualiza el blob de perfil para
// (org_id, user_id). Pisa el profile_json anterior; el caller compone el JSON
// final si quiere preservar campos.
func (r *PostgresRepository) UpsertAgentUserProfile(ctx context.Context, in domain.AgentUserProfile) (domain.AgentUserProfile, error) {
	if in.Source == "" {
		in.Source = domain.AgentSourceCompanionNative
	}
	if len(in.ProfileJSON) == 0 {
		in.ProfileJSON = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	in.UpdatedAt = now

	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO agent_user_profiles
			(org_id, user_id, profile_json, source, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (org_id, user_id) DO UPDATE
		SET profile_json = EXCLUDED.profile_json,
		    source       = EXCLUDED.source,
		    updated_at   = EXCLUDED.updated_at
		RETURNING created_at, updated_at
	`, in.OrgID, in.UserID, in.ProfileJSON, in.Source, now, now).
		Scan(&in.CreatedAt, &in.UpdatedAt)
	if err != nil {
		return domain.AgentUserProfile{}, fmt.Errorf("upsert agent_user_profile: %w", err)
	}
	return in, nil
}

// GetAgentUserProfile devuelve el perfil de un usuario en un org.
func (r *PostgresRepository) GetAgentUserProfile(ctx context.Context, orgID, userID string) (domain.AgentUserProfile, error) {
	var p domain.AgentUserProfile
	err := r.db.Pool().QueryRow(ctx, `
		SELECT org_id, user_id, profile_json, source, created_at, updated_at
		FROM agent_user_profiles WHERE org_id = $1 AND user_id = $2
	`, orgID, userID).Scan(&p.OrgID, &p.UserID, &p.ProfileJSON, &p.Source, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.AgentUserProfile{}, err
	}
	return p, nil
}

// IsAgentMemoryNotFound checkea si un Get devolvió "no encontrado" usando los
// sentinels de pgx. Útil para callers que quieren convertir a un 404 HTTP.
func IsAgentMemoryNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
