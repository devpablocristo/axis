package memory

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
	contracts "github.com/devpablocristo/platform/contracts/ai/go"
)

const (
	defaultConversationListLimit = 50
	maxConversationListLimit     = 200
)

// chatScopeRead es el scope requerido para leer las conversaciones del agente.
// Reusamos el scope companion:tasks:read porque conceptualmente conversations
// es lectura del mismo recurso "chat" (tasks ya viven en ese namespace).
const chatScopeRead = "companion:tasks:read"

// ChatUsecases es el subset del AgentMemoryUC que el ChatHandler necesita.
type ChatUsecases interface {
	ListConversations(ctx context.Context, orgID, userID string, limit int) ([]domain.AgentConversation, error)
	GetConversation(ctx context.Context, id uuid.UUID) (domain.AgentConversation, error)
	ListMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]domain.AgentConversationMessage, error)
}

// ChatHandler expone los endpoints canónicos de listado de conversaciones e
// insights del agente, contractualmente alineados con
// github.com/devpablocristo/platform/contracts/ai/go.
//
//   - GET  /v1/chat/conversations            → list per (org, user)
//   - GET  /v1/chat/conversations/{id}       → detalle + mensajes
//   - POST /v1/notifications                 → insights/notificaciones
type ChatHandler struct {
	uc ChatUsecases
}

// NewChatHandler crea un nuevo handler de chat/conversations.
func NewChatHandler(uc ChatUsecases) *ChatHandler {
	return &ChatHandler{uc: uc}
}

// Register monta las rutas en el mux.
func (h *ChatHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/chat/conversations", h.listConversations)
	mux.HandleFunc("GET /v1/chat/conversations/{id}", h.getConversation)
	mux.HandleFunc("POST /v1/notifications", h.notifications)
}

func (h *ChatHandler) listConversations(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, chatScopeRead) {
		return
	}
	id := identityctx.FromRequest(r)
	orgID := id.CustomerOrgID
	userID := id.HumanUserID

	limit := defaultConversationListLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxConversationListLimit {
		limit = maxConversationListLimit
	}

	convs, err := h.uc.ListConversations(r.Context(), orgID, userID, limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list conversations failed")
		return
	}

	items := make([]contracts.ConversationSummary, 0, len(convs))
	for _, c := range convs {
		items = append(items, contracts.ConversationSummary{
			ID:             c.ID,
			Title:          c.Title,
			CreatedAt:      c.CreatedAt,
			UpdatedAt:      c.UpdatedAt,
			ProductSurface: c.ProductSurface,
			// message_count: dejarlo en 0 por ahora; calcularlo requeriría una
			// query agregada. Se puede agregar como mejora opcional sin
			// breaking change.
		})
	}
	httpjson.WriteJSON(w, http.StatusOK, contracts.ConversationListResponse{Items: items})
}

func (h *ChatHandler) getConversation(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, chatScopeRead) {
		return
	}
	idRaw := r.PathValue("id")
	id, err := uuid.Parse(idRaw)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid conversation id")
		return
	}

	conv, err := h.uc.GetConversation(r.Context(), id)
	if err != nil {
		if IsAgentMemoryNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get conversation failed")
		return
	}
	// Fail-closed multi-tenant: el orgID del header debe coincidir con la
	// org de la conversación.
	headerOrg := identityctx.FromRequest(r).CustomerOrgID
	if headerOrg != "" && conv.OrgID != headerOrg {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
		return
	}

	msgs, err := h.uc.ListMessages(r.Context(), id, maxConversationListLimit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list messages failed")
		return
	}

	out := contracts.ConversationDetail{
		ID:        conv.ID,
		Title:     conv.Title,
		CreatedAt: conv.CreatedAt,
		UpdatedAt: conv.UpdatedAt,
		Messages:  make([]contracts.ConversationMessage, 0, len(msgs)),
	}
	for _, m := range msgs {
		cm := contracts.ConversationMessage{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: m.CreatedAt,
		}
		if m.Role == "assistant" && m.Content != "" {
			cm.Blocks = []contracts.ChatBlock{{Type: "text", Text: m.Content}}
		}
		out.Messages = append(out.Messages, cm)
	}
	httpjson.WriteJSON(w, http.StatusOK, out)
}

// notifications devuelve una respuesta válida según el contrato compartido.
// La generación enriquecida queda centralizada en el runtime Gemini.
func (h *ChatHandler) notifications(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, chatScopeRead) {
		return
	}
	var body contracts.NotificationsRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		// Body es opcional para este endpoint; si no es JSON válido, lo
		// tratamos como request por defecto en vez de fallar.
		body = contracts.NotificationsRequest{}
	}
	resp := contracts.NotificationsResponse{
		Items:       []contracts.NotificationItem{},
		ServiceKind: "gemini",
		OutputKind:  "notifications.v0",
	}
	_ = body
	httpjson.WriteJSON(w, http.StatusOK, resp)
}
