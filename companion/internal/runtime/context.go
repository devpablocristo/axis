package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/nexusclient"
	"github.com/google/uuid"

	memdomain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// --- Identidad en context para tools ---

type identityKey struct{}

// Identity representa el usuario y organización del request actual.
type Identity struct {
	UserID             string
	OrgID              string
	ActorType          string
	CompanionPrincipal string
	OnBehalfOf         string
	ProductSurface     string
	TaskID             string
	PlanStepID         string
	IdempotencyKey     string
	AllowedTools       []string
	AuthScopes         []string
	ServicePrincipal   bool
}

// WithIdentity inyecta identidad en el context.
func WithIdentity(ctx context.Context, userID, orgID string, scopes ...string) context.Context {
	return WithIdentityForProduct(ctx, userID, orgID, DefaultProductSurface, scopes...)
}

func WithIdentityForProduct(ctx context.Context, userID, orgID, productSurface string, scopes ...string) context.Context {
	productSurface = strings.TrimSpace(productSurface)
	if productSurface == "" {
		productSurface = DefaultProductSurface
	}
	return context.WithValue(ctx, identityKey{}, Identity{
		UserID:             strings.TrimSpace(userID),
		OrgID:              strings.TrimSpace(orgID),
		CompanionPrincipal: CompanionPrincipal,
		ProductSurface:     productSurface,
		AuthScopes:         append([]string(nil), scopes...),
	})
}

func WithIdentityContext(ctx context.Context, id identityctx.IdentityContext) context.Context {
	id = id.WithProductSurface(id.ProductSurface)
	return context.WithValue(ctx, identityKey{}, Identity{
		UserID:             id.EffectiveActorID(),
		OrgID:              id.CustomerOrgID,
		ActorType:          id.ActorType,
		CompanionPrincipal: id.CompanionPrincipal,
		OnBehalfOf:         id.OnBehalfOf,
		ProductSurface:     id.ProductSurface,
		AuthScopes:         append([]string(nil), id.Scopes...),
		ServicePrincipal:   id.ServicePrincipal,
	})
}

func WithTaskID(ctx context.Context, taskID uuid.UUID) context.Context {
	if taskID == uuid.Nil {
		return ctx
	}
	id := IdentityFromContext(ctx)
	id.TaskID = taskID.String()
	return context.WithValue(ctx, identityKey{}, id)
}

func WithAllowedTools(ctx context.Context, allowedTools []string) context.Context {
	id := IdentityFromContext(ctx)
	id.AllowedTools = append([]string(nil), allowedTools...)
	return context.WithValue(ctx, identityKey{}, id)
}

func WithPlanStepExecution(ctx context.Context, stepID uuid.UUID, idempotencyKey string) context.Context {
	id := IdentityFromContext(ctx)
	if stepID != uuid.Nil {
		id.PlanStepID = stepID.String()
	}
	id.IdempotencyKey = strings.TrimSpace(idempotencyKey)
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFromContext extrae la identidad del context.
func IdentityFromContext(ctx context.Context) Identity {
	id, _ := ctx.Value(identityKey{}).(Identity)
	return id
}

// ContextPorts interfaces que el context assembler necesita.
type ContextPorts struct {
	NexusClient *nexusclient.Client
	MemoryFind  func(ctx context.Context, orgID, userID, productSurface string, scopeType memdomain.ScopeType, scopeID string, kind memdomain.MemoryKind, limit int) ([]memdomain.MemoryEntry, error)
	TaskPlanGet func(ctx context.Context, taskID uuid.UUID) (taskdomain.TaskPlan, error)
}

// AssembledContext contexto ensamblado para el LLM.
type AssembledContext struct {
	Summary string
	History []LLMMessage
}

// AssembleContext arma el contexto relevante para una conversación.
func AssembleContext(ctx context.Context, ports ContextPorts, userID, orgID, productSurface string, authScopes []string, taskID *uuid.UUID, messages []taskdomain.TaskMessage) AssembledContext {
	var parts []string
	if strings.TrimSpace(productSurface) == "" {
		productSurface = DefaultProductSurface
	}

	// 1. Memoria del usuario (preferencias)
	if ports.MemoryFind != nil {
		if scopeID := tenantUserMemoryScopeID(orgID, userID); scopeID != "" {
			userMem, err := ports.MemoryFind(ctx, orgID, userID, productSurface, memdomain.ScopeUser, scopeID, memdomain.MemoryUserPreference, 5)
			if err == nil && len(userMem) > 0 {
				var prefs []string
				for _, m := range userMem {
					if m.ContentText != "" {
						prefs = append(prefs, fmt.Sprintf("- %s: %s", m.Key, m.ContentText))
					}
				}
				if len(prefs) > 0 {
					parts = append(parts, "Preferencias del usuario:\n"+strings.Join(prefs, "\n"))
				}
			}
		}

		// Memoria de la org (hechos del negocio)
		if strings.TrimSpace(orgID) != "" {
			orgMem, err := ports.MemoryFind(ctx, orgID, userID, productSurface, memdomain.ScopeOrg, orgID, memdomain.MemoryPlaybook, 5)
			if err == nil && len(orgMem) > 0 {
				var facts []string
				for _, m := range orgMem {
					if m.ContentText != "" {
						facts = append(facts, "- "+m.ContentText)
					}
				}
				if len(facts) > 0 {
					parts = append(parts, "Hechos del negocio:\n"+strings.Join(facts, "\n"))
				}
			}
		}
	}

	// 2. Plan durable de la task actual
	if ports.TaskPlanGet != nil && taskID != nil && *taskID != uuid.Nil {
		plan, err := ports.TaskPlanGet(ctx, *taskID)
		if err == nil && strings.TrimSpace(plan.Objective) != "" {
			if summary := summarizeTaskPlan(plan); summary != "" {
				parts = append(parts, summary)
			}
		}
	}

	// 3. Aprobaciones pendientes
	if ports.NexusClient != nil && strings.TrimSpace(orgID) != "" && hasAnyScope(authScopes, scopeCompanionNexusAdmin) {
		st, raw, err := ports.NexusClient.ListPendingApprovals(ctx)
		if err == nil && st == 200 && len(raw) > 0 {
			var approvals struct {
				Data []struct {
					ID         string `json:"id"`
					ActionType string `json:"action_type"`
					Reason     string `json:"reason"`
					RiskLevel  string `json:"risk_level"`
				} `json:"data"`
			}
			if jsonErr := json.Unmarshal(raw, &approvals); jsonErr == nil && len(approvals.Data) > 0 {
				var items []string
				for _, a := range approvals.Data {
					short := a.ID
					if len(short) > 8 {
						short = short[:8]
					}
					items = append(items, fmt.Sprintf("- [%s] %s (riesgo: %s, razón: %s)", short, a.ActionType, a.RiskLevel, a.Reason))
				}
				parts = append(parts, fmt.Sprintf("Aprobaciones pendientes (%d):\n%s", len(items), strings.Join(items, "\n")))
			}
		}
	}

	// 4. Historial de mensajes → formato LLM
	var history []LLMMessage
	limit := 20
	start := 0
	if len(messages) > limit {
		start = len(messages) - limit
	}
	for _, m := range messages[start:] {
		role := "user"
		if m.AuthorType == "system" || m.AuthorType == "assistant" {
			role = "assistant"
		}
		history = append(history, LLMMessage{Role: role, Content: m.Body})
	}

	summary := ""
	if len(parts) > 0 {
		summary = strings.Join(parts, "\n\n")
	}

	return AssembledContext{
		Summary: summary,
		History: history,
	}
}

func summarizeTaskPlan(plan taskdomain.TaskPlan) string {
	var lines []string
	lines = append(lines, "Plan durable de la task:")
	lines = append(lines, "- Objetivo: "+plan.Objective)
	if plan.Status != "" {
		lines = append(lines, "- Estado del plan: "+plan.Status)
	}
	if plan.Strategy != "" {
		lines = append(lines, "- Estrategia: "+plan.Strategy)
	}
	if plan.NextAction != "" {
		lines = append(lines, "- Próxima acción: "+plan.NextAction)
	}
	if plan.Blocker != "" {
		lines = append(lines, "- Bloqueo: "+plan.Blocker)
	}
	if len(plan.Steps) > 0 {
		lines = append(lines, "- Pasos:")
		for _, step := range plan.Steps {
			line := fmt.Sprintf("  - [%s] %s", step.Status, step.Title)
			if step.ExpectedOutcome != "" {
				line += " → " + step.ExpectedOutcome
			}
			if step.Postcondition != "" {
				line += " (verificar: " + step.Postcondition + ")"
			}
			if step.Blocker != "" {
				line += " (bloqueado: " + step.Blocker + ")"
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func hasAnyScope(scopes []string, required ...string) bool {
	have := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope = strings.TrimSpace(scope); scope != "" {
			have[scope] = struct{}{}
		}
	}
	for _, scope := range required {
		if _, ok := have[scope]; ok {
			return true
		}
	}
	return false
}
