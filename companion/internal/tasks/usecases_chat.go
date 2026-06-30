package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/identityctx"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// ChatInput entrada para el endpoint de chat conversacional.
type ChatInput struct {
	TaskID         *uuid.UUID // nil = crear tarea nueva
	ChatID         *uuid.UUID // nil = resolver por task_id o crear task nueva
	UserID         string
	OrgID          string
	AuthScopes     []string
	Message        string
	Channel        string // "api", "watcher", "whatsapp", product-specific channels, etc.
	ProductSurface string // opcional: "companion" | "ponti" | "pymes". Afecta routing del agent.
	TenantID       string // requerido cuando EmployeeID resuelve un Virtual Employee.
	EmployeeID     string // opcional: Virtual Employee persistente para esta task/conversación.
	AgentID        string // opcional legacy: Agent tecnico persistente para esta task/conversación.
	RouteHint      string // opcional: pista de pantalla/módulo para ruteo operativo.
	Handoff        json.RawMessage
	Workspace      json.RawMessage // contexto operativo de pantalla; gana sobre handoff.workspace
	Identity       identityctx.IdentityContext
}

// ChatResult resultado del chat.
type ChatResult struct {
	Task      domain.Task
	Messages  []domain.TaskMessage
	RunID     string
	EmployeeID string
	AgentID   string
	ToolCalls []OrchestratorToolCall
}

// agentConversationContextKey nombre del field en task.context_json que guarda
// el agent_conversations.id asociado a la task. Permite reusar la misma
// conversation_id en mensajes sucesivos del mismo task.
const agentConversationContextKey = "agent_conversation_id"
const agentContextKey = "agent_id"
const employeeContextKey = "employee_id"

// workspaceContextKey nombre del field en task.context_json que guarda el
// workspace operativo del último chat. Permite reusar el mismo workspace en
// turnos siguientes si el caller no lo reenvía.
const workspaceContextKey = "workspace"

// Chat combina crear/reusar tarea + agregar mensaje del usuario.
// Es el endpoint principal para la interfaz conversacional del suscriptor.
func (u *Usecases) Chat(ctx context.Context, in ChatInput) (ChatResult, error) {
	if in.Message == "" {
		return ChatResult{}, fmt.Errorf("message is required")
	}
	in.Identity = chatIdentity(in)
	in.UserID = in.Identity.EffectiveActorID()
	in.OrgID = in.Identity.CustomerOrgID
	in.AuthScopes = append([]string(nil), in.Identity.Scopes...)
	in.ProductSurface = in.Identity.ProductSurface
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.EmployeeID = strings.TrimSpace(in.EmployeeID)
	in.AgentID = strings.TrimSpace(in.AgentID)

	var t domain.Task
	var err error
	newTask := false

	if in.TaskID != nil {
		// Reusar tarea existente
		t, err = u.repo.GetTaskByID(ctx, *in.TaskID)
		if err != nil {
			return ChatResult{}, err
		}
		if in.OrgID != "" && t.OrgID != "" && t.OrgID != in.OrgID {
			return ChatResult{}, ErrNotFound
		}
		if in.AgentID == "" {
			in.AgentID = extractTaskAgentID(t.ContextJSON)
		}
		if in.EmployeeID == "" {
			in.EmployeeID = extractTaskEmployeeID(t.ContextJSON)
		}
	} else if in.ChatID != nil {
		// Reusar tarea existente a partir del identificador público de conversación.
		t, err = u.repo.GetTaskByAgentConversationID(ctx, *in.ChatID)
		if err != nil {
			return ChatResult{}, err
		}
		if in.OrgID != "" && t.OrgID != "" && t.OrgID != in.OrgID {
			return ChatResult{}, ErrNotFound
		}
		if in.AgentID == "" {
			in.AgentID = extractTaskAgentID(t.ContextJSON)
		}
		if in.EmployeeID == "" {
			in.EmployeeID = extractTaskEmployeeID(t.ContextJSON)
		}
	} else {
		// Crear tarea nueva con el primer mensaje como título
		title := in.Message
		if len(title) > 80 {
			title = title[:80]
		}
		channel := in.Channel
		if channel == "" {
			channel = "api"
		}
		contextJSON := json.RawMessage(`{}`)
		if in.AgentID != "" {
			if updated, ok := mergeTaskAgentID(contextJSON, in.AgentID); ok {
				contextJSON = updated
			}
		}
		if in.EmployeeID != "" {
			if updated, ok := mergeTaskEmployeeID(contextJSON, in.EmployeeID); ok {
				contextJSON = updated
			}
		}
		if updated, ok := mergeTaskRuntimeContext(contextJSON, in.ProductSurface, in.RouteHint, channel); ok {
			contextJSON = updated
		}
		t, err = u.repo.CreateTask(ctx, domain.Task{
			Title:       title,
			OrgID:       in.OrgID,
			Status:      domain.TaskStatusNew,
			Priority:    "normal",
			CreatedBy:   in.UserID,
			Channel:     channel,
			ContextJSON: contextJSON,
		})
		if err != nil {
			return ChatResult{}, fmt.Errorf("create chat task: %w", err)
		}
		newTask = true
		slog.Info("companion chat started", "task_id", t.ID.String(), "user_id", in.UserID)
	}

	// Conversación durable en agent_conversations (best-effort). Si arrancamos
	// task nueva: creamos conversation y stasheamos su id en task.context_json
	// para reusar en mensajes sucesivos. Si task existente: reusamos el id ya
	// guardado.
	convID := u.ensureAgentConversation(ctx, &t, in, newTask)
	u.ensureTaskAgent(ctx, &t, in.AgentID)
	u.ensureTaskEmployee(ctx, &t, in.EmployeeID)

	// Workspace operativo: si el caller no lo reenvía en este turno, reusamos
	// el último persistido en task.context_json; si lo reenvía, lo stasheamos
	// para los turnos siguientes (mismo patrón que agent_conversation_id).
	if len(in.Workspace) == 0 {
		in.Workspace = extractTaskWorkspace(t.ContextJSON)
	} else {
		u.ensureTaskWorkspace(ctx, &t, in.Workspace)
	}

	// Agregar mensaje del usuario
	_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     t.ID,
		AuthorType: "user",
		AuthorID:   in.UserID,
		Body:       in.Message,
	})
	if err != nil {
		return ChatResult{}, fmt.Errorf("insert chat message: %w", err)
	}
	u.persistAgentMessage(ctx, convID, t.OrgID, "user", in.Message)

	// Si hay orchestrator, generar respuesta del compañero
	runID := ""
	toolCalls := []OrchestratorToolCall{}
	if u.orchestrator != nil {
		existingMsgs, listErr := u.repo.ListMessagesByTaskID(ctx, t.ID)
		if listErr != nil {
			slog.Error("chat list messages for orchestrator", "error", listErr)
		} else {
			orgID := in.OrgID
			if orgID == "" {
				orgID = t.OrgID
			}
			taskID := t.ID
			result, runErr := u.orchestrator.Run(ctx, OrchestratorInput{
				UserID:         in.UserID,
				OrgID:          orgID,
				AuthScopes:     in.AuthScopes,
				Identity:       in.Identity,
				Message:        in.Message,
				RouteHint:      in.RouteHint,
				Handoff:        in.Handoff,
				Workspace:      in.Workspace,
				Messages:       existingMsgs,
				TaskID:         &taskID,
				ProductSurface: in.ProductSurface,
				TenantID:       in.TenantID,
				EmployeeID:     in.EmployeeID,
				AgentID:        in.AgentID,
			})
			if runErr != nil {
				slog.Error("orchestrator failed", "error", runErr)
				return ChatResult{}, fmt.Errorf("run companion runtime: %w", runErr)
			} else {
				in.EmployeeID = result.EmployeeID
				in.AgentID = result.AgentID
				runID = result.RunID
				toolCalls = result.ToolCalls
				u.ensureTaskEmployee(ctx, &t, in.EmployeeID)
				u.ensureTaskAgent(ctx, &t, in.AgentID)
			}
			if runErr == nil && result.Reply != "" {
				// Guardar respuesta del compañero como mensaje del sistema
				_, insertErr := u.repo.InsertMessage(ctx, domain.TaskMessage{
					TaskID:     t.ID,
					AuthorType: "system",
					AuthorID:   in.Identity.CompanionPrincipal,
					Body:       result.Reply,
				})
				if insertErr != nil {
					slog.Error("insert orchestrator reply", "error", insertErr)
				}
				u.persistAgentMessage(ctx, convID, t.OrgID, "assistant", result.Reply)
			}
		}
	}

	// Devolver hilo completo (incluyendo respuesta del compañero si hubo)
	msgs, err := u.repo.ListMessagesByTaskID(ctx, t.ID)
	if err != nil {
		return ChatResult{}, fmt.Errorf("list chat messages: %w", err)
	}

	return ChatResult{Task: t, Messages: msgs, RunID: runID, EmployeeID: in.EmployeeID, AgentID: in.AgentID, ToolCalls: toolCalls}, nil
}

// ensureAgentConversation obtiene o crea la conversación durable asociada a la
// task. Si la task ya tenía un id stasheado en context_json, lo reusa. Si no,
// crea una nueva y persiste el id en context_json. Best-effort: nunca falla el
// chat, solo logea.
func (u *Usecases) ensureAgentConversation(ctx context.Context, t *domain.Task, in ChatInput, newTask bool) uuid.UUID {
	if u.agentMemory == nil {
		return uuid.Nil
	}
	if !newTask {
		if existing := extractAgentConversationID(t.ContextJSON); existing != uuid.Nil {
			return existing
		}
	}
	productSurface := in.ProductSurface
	if productSurface == "" {
		productSurface = "companion"
	}
	convID, err := u.agentMemory.StartConversation(ctx, t.OrgID, in.UserID, productSurface, t.Title)
	if err != nil {
		slog.Error("agent memory start conversation", "error", err, "task_id", t.ID)
		return uuid.Nil
	}
	if updated, ok := mergeAgentConversationID(t.ContextJSON, convID); ok {
		t.ContextJSON = updated
		if _, err := u.repo.UpdateTask(ctx, *t); err != nil {
			slog.Error("update task context with conversation_id", "error", err, "task_id", t.ID)
		}
	}
	return convID
}

func (u *Usecases) ensureTaskAgent(ctx context.Context, t *domain.Task, agentID string) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || extractTaskAgentID(t.ContextJSON) == agentID {
		return
	}
	if updated, ok := mergeTaskAgentID(t.ContextJSON, agentID); ok {
		t.ContextJSON = updated
		if _, err := u.repo.UpdateTask(ctx, *t); err != nil {
			slog.Error("update task context with agent_id", "error", err, "task_id", t.ID)
		}
	}
}

func (u *Usecases) ensureTaskEmployee(ctx context.Context, t *domain.Task, employeeID string) {
	employeeID = strings.TrimSpace(employeeID)
	if employeeID == "" || extractTaskEmployeeID(t.ContextJSON) == employeeID {
		return
	}
	if updated, ok := mergeTaskEmployeeID(t.ContextJSON, employeeID); ok {
		t.ContextJSON = updated
		if _, err := u.repo.UpdateTask(ctx, *t); err != nil {
			slog.Error("update task context with employee_id", "error", err, "task_id", t.ID)
		}
	}
}

func (u *Usecases) ensureTaskWorkspace(ctx context.Context, t *domain.Task, workspace json.RawMessage) {
	updated, ok := mergeTaskWorkspace(t.ContextJSON, workspace)
	if !ok || string(updated) == string(t.ContextJSON) {
		return
	}
	t.ContextJSON = updated
	if _, err := u.repo.UpdateTask(ctx, *t); err != nil {
		slog.Error("update task context with workspace", "error", err, "task_id", t.ID)
	}
}

func (u *Usecases) persistAgentMessage(ctx context.Context, convID uuid.UUID, orgID, role, content string) {
	if u.agentMemory == nil || convID == uuid.Nil || content == "" {
		return
	}
	if err := u.agentMemory.AppendMessage(ctx, convID, orgID, role, content); err != nil {
		slog.Error("agent memory append message", "error", err, "conversation_id", convID, "role", role)
	}
}

func chatIdentity(in ChatInput) identityctx.IdentityContext {
	id := in.Identity
	if id.CustomerOrgID == "" {
		id.CustomerOrgID = in.OrgID
	}
	if id.HumanUserID == "" {
		id.HumanUserID = in.UserID
	}
	if len(id.Scopes) == 0 {
		id.Scopes = append([]string(nil), in.AuthScopes...)
	}
	if id.ProductSurface == "" {
		id.ProductSurface = in.ProductSurface
	}
	return id.WithProductSurface(id.ProductSurface)
}

func extractAgentConversationID(raw json.RawMessage) uuid.UUID {
	if len(raw) == 0 {
		return uuid.Nil
	}
	var holder map[string]any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return uuid.Nil
	}
	v, ok := holder[agentConversationContextKey].(string)
	if !ok {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

func extractTaskAgentID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var holder map[string]any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return ""
	}
	value, _ := holder[agentContextKey].(string)
	return strings.TrimSpace(value)
}

func extractTaskEmployeeID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var holder map[string]any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return ""
	}
	value, _ := holder[employeeContextKey].(string)
	return strings.TrimSpace(value)
}

func mergeTaskAgentID(raw json.RawMessage, agentID string) (json.RawMessage, bool) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return raw, false
	}
	holder := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	holder[agentContextKey] = agentID
	out, err := json.Marshal(holder)
	if err != nil {
		return nil, false
	}
	return out, true
}

func mergeTaskEmployeeID(raw json.RawMessage, employeeID string) (json.RawMessage, bool) {
	employeeID = strings.TrimSpace(employeeID)
	if employeeID == "" {
		return raw, false
	}
	holder := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	holder[employeeContextKey] = employeeID
	out, err := json.Marshal(holder)
	if err != nil {
		return nil, false
	}
	return out, true
}

func mergeTaskRuntimeContext(raw json.RawMessage, productSurface, routeHint, channel string) (json.RawMessage, bool) {
	holder := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	changed := false
	if value := strings.TrimSpace(productSurface); value != "" {
		holder["product_surface"] = value
		changed = true
	}
	if value := strings.TrimSpace(routeHint); value != "" {
		holder["route_hint"] = value
		holder["run_type"] = value
		changed = true
	}
	if value := strings.TrimSpace(channel); value != "" {
		holder["channel"] = value
		changed = true
	}
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(holder)
	if err != nil {
		return nil, false
	}
	return out, true
}

func extractTaskWorkspace(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var holder map[string]json.RawMessage
	if err := json.Unmarshal(raw, &holder); err != nil {
		return nil
	}
	workspace, ok := holder[workspaceContextKey]
	if !ok || len(workspace) == 0 || string(workspace) == "null" {
		return nil
	}
	return workspace
}

func mergeTaskWorkspace(raw json.RawMessage, workspace json.RawMessage) (json.RawMessage, bool) {
	var decoded map[string]any
	if len(workspace) == 0 || json.Unmarshal(workspace, &decoded) != nil || len(decoded) == 0 {
		return raw, false
	}
	holder := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	holder[workspaceContextKey] = decoded
	out, err := json.Marshal(holder)
	if err != nil {
		return nil, false
	}
	return out, true
}

func mergeAgentConversationID(raw json.RawMessage, convID uuid.UUID) (json.RawMessage, bool) {
	holder := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	holder[agentConversationContextKey] = convID.String()
	out, err := json.Marshal(holder)
	if err != nil {
		return nil, false
	}
	return out, true
}
