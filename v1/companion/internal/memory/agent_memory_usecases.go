package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/devpablocristo/companion/internal/memory/usecases/domain"
	"github.com/google/uuid"
)

// agentMemRepo es el subset del repositorio que necesita AgentMemoryUC.
// Lo declaramos local para evitar acoplar Usecases viejo y el nuevo a la misma
// interfaz inflada.
type agentMemRepo interface {
	CreateAgentConversation(ctx context.Context, in domain.AgentConversation) (domain.AgentConversation, error)
	GetAgentConversation(ctx context.Context, id uuid.UUID) (domain.AgentConversation, error)
	ListAgentConversations(ctx context.Context, orgID, userID string, limit int) ([]domain.AgentConversation, error)
	AppendAgentMessage(ctx context.Context, in domain.AgentConversationMessage) (domain.AgentConversationMessage, error)
	ListAgentMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]domain.AgentConversationMessage, error)
}

// AgentMemoryUC orquesta agent_conversations + agent_conversation_messages.
// Es el reemplazo Companion-native de pymes-ai.ai_conversations:
// scoped por (org_id, user_id, product_surface), durable, independiente de
// Tasks.
type AgentMemoryUC struct {
	repo agentMemRepo
}

// NewAgentMemoryUC crea una nueva instancia.
func NewAgentMemoryUC(repo agentMemRepo) *AgentMemoryUC {
	return &AgentMemoryUC{repo: repo}
}

// StartConversation crea una nueva conversación y devuelve el ID. Es idempotente
// solo desde el lado del caller (no busca conversación previa). Para reutilizar
// una conversación existente, usar AppendMessage directamente con el ID guardado.
func (u *AgentMemoryUC) StartConversation(ctx context.Context, orgID, userID, productSurface, title string) (uuid.UUID, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return uuid.Nil, fmt.Errorf("org_id is required")
	}
	productSurface = strings.TrimSpace(productSurface)
	if productSurface == "" {
		productSurface = "companion"
	}
	conv, err := u.repo.CreateAgentConversation(ctx, domain.AgentConversation{
		OrgID:          orgID,
		UserID:         strings.TrimSpace(userID),
		ProductSurface: productSurface,
		Title:          strings.TrimSpace(title),
		Source:         domain.AgentSourceCompanionNative,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("start conversation: %w", err)
	}
	return conv.ID, nil
}

// AppendMessage agrega un mensaje (user/assistant/system/tool) a una
// conversación existente. role debe ser uno de los aceptados por el CHECK
// constraint del schema.
func (u *AgentMemoryUC) AppendMessage(ctx context.Context, conversationID uuid.UUID, orgID, role, content string) error {
	if conversationID == uuid.Nil {
		return fmt.Errorf("conversation_id is required")
	}
	role = strings.TrimSpace(role)
	switch role {
	case "user", "assistant", "system", "tool":
	default:
		return fmt.Errorf("invalid role: %q", role)
	}
	if _, err := u.repo.AppendAgentMessage(ctx, domain.AgentConversationMessage{
		ConversationID: conversationID,
		OrgID:          strings.TrimSpace(orgID),
		Role:           role,
		Content:        content,
	}); err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}

// ListMessages devuelve los últimos `limit` mensajes de una conversación, en
// orden cronológico.
func (u *AgentMemoryUC) ListMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]domain.AgentConversationMessage, error) {
	return u.repo.ListAgentMessages(ctx, conversationID, limit)
}

// ListConversations devuelve hasta `limit` conversaciones recientes de un
// usuario dentro de una org. Si userID está vacío, devuelve todas las del
// tenant (caso admin/console).
func (u *AgentMemoryUC) ListConversations(ctx context.Context, orgID, userID string, limit int) ([]domain.AgentConversation, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, fmt.Errorf("org_id is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return u.repo.ListAgentConversations(ctx, orgID, strings.TrimSpace(userID), limit)
}

// GetConversation devuelve una conversación por id.
func (u *AgentMemoryUC) GetConversation(ctx context.Context, id uuid.UUID) (domain.AgentConversation, error) {
	return u.repo.GetAgentConversation(ctx, id)
}
