package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/devpablocristo/companion/internal/nexusclient"

	"github.com/devpablocristo/companion/internal/memory"
	memdomain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
	"github.com/devpablocristo/companion/internal/watchers"
)

// ToolHandler ejecuta un tool y devuelve resultado como string JSON.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// ToolKit contiene todas las herramientas del compañero.
type ToolKit struct {
	Schemas  []ToolSchema
	Handlers map[string]ToolHandler
	policies map[string]toolPolicy
	metadata map[string]ToolMetadata
}

type toolPolicy struct {
	RequiresTenant   bool
	RequiresUser     bool
	RequiresTask     bool
	RequiredAnyScope []string
}

type ToolMetadata struct {
	Name                  string   `json:"name,omitempty"`
	Operation             string   `json:"operation,omitempty"`
	CapabilityID          string   `json:"capability_id,omitempty"`
	CapabilityVersion     string   `json:"capability_version,omitempty"`
	Product               string   `json:"product,omitempty"`
	ActionType            string   `json:"action_type,omitempty"`
	SideEffectClass       string   `json:"side_effect_class,omitempty"`
	RiskClass             string   `json:"risk_class,omitempty"`
	NexusActionType       string   `json:"nexus_action_type,omitempty"`
	RequiresNexusApproval bool     `json:"requires_nexus_approval,omitempty"`
	EvidenceRequired      []string `json:"evidence_required,omitempty"`
	RollbackSupported     bool     `json:"rollback_supported,omitempty"`
	RollbackCapabilityID  string   `json:"rollback_capability_id,omitempty"`
	CompensationStrategy  string   `json:"compensation_strategy,omitempty"`
	CostClass             string   `json:"cost_class,omitempty"`
	RateLimitClass        string   `json:"rate_limit_class,omitempty"`
	Timeout               string   `json:"timeout,omitempty"`
	Preconditions         []string `json:"preconditions,omitempty"`
	Postconditions        []string `json:"postconditions,omitempty"`
	ObservabilityTags     []string `json:"observability_tags,omitempty"`
}

const (
	scopeCompanionNexusAdmin   = "companion:nexus:admin"
	scopeCompanionWatchersRead = "companion:watchers:read"
)

// NewToolKit crea el kit de tools con las dependencias inyectadas.
func NewToolKit(rc *nexusclient.Client, memUC *memory.Usecases, watcherUC *watchers.Usecases) *ToolKit {
	tk := &ToolKit{
		Handlers: make(map[string]ToolHandler),
		policies: make(map[string]toolPolicy),
		metadata: make(map[string]ToolMetadata),
	}

	// --- get_overview: resumen de estado ---
	tk.add(ToolSchema{
		Name:        "get_overview",
		Description: "Obtiene un resumen del estado actual: aprobaciones pendientes y alertas activas.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, toolPolicy{RequiresTenant: true}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		var parts []string
		id := IdentityFromContext(ctx)
		if strings.TrimSpace(id.OrgID) == "" {
			return `{"error":"customer org context required"}`, nil
		}

		// Aprobaciones pendientes
		if rc != nil && hasAnyScope(id.AuthScopes, scopeCompanionNexusAdmin) {
			st, raw, err := rc.ListPendingApprovals(ctx)
			if err == nil && st == 200 {
				parts = append(parts, summarizeApprovals(raw))
			}
		}

		// Watchers activos
		if watcherUC != nil {
			wList, err := watcherUC.List(ctx, id.OrgID)
			if err == nil {
				active := 0
				for _, w := range wList {
					if w.Enabled {
						active++
					}
				}
				parts = append(parts, fmt.Sprintf("Watchers activos: %d de %d configurados", active, len(wList)))
			}
		}

		if len(parts) == 0 {
			return `{"status": "sin datos disponibles"}`, nil
		}
		result := map[string]any{"overview": parts}
		b, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal overview result: %w", err)
		}
		return string(b), nil
	})

	// --- check_approvals: listar aprobaciones pendientes ---
	tk.add(ToolSchema{
		Name:        "check_approvals",
		Description: "Lista las aprobaciones pendientes que el usuario puede aprobar o rechazar.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, toolPolicy{RequiresTenant: true, RequiredAnyScope: []string{scopeCompanionNexusAdmin}}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if rc == nil {
			return `{"approvals": [], "message": "nexus no configurado"}`, nil
		}
		st, raw, err := rc.ListPendingApprovals(ctx)
		if err != nil {
			return "", fmt.Errorf("list approvals: %w", err)
		}
		if st != 200 {
			return fmt.Sprintf(`{"error": "nexus respondió con status %d"}`, st), nil
		}
		return string(raw), nil
	})

	// --- list_policies ---
	tk.add(ToolSchema{
		Name:        "list_policies",
		Description: "Lista las reglas de decisiones sensibles activas.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, toolPolicy{RequiresTenant: true, RequiredAnyScope: []string{scopeCompanionNexusAdmin}}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if rc == nil {
			return `{"policies": []}`, nil
		}
		st, raw, err := rc.ListPolicies(ctx)
		if err != nil {
			return "", fmt.Errorf("list policies: %w", err)
		}
		if st != 200 {
			return fmt.Sprintf(`{"error": "status %d"}`, st), nil
		}
		return string(raw), nil
	})

	// --- list_watchers ---
	tk.add(ToolSchema{
		Name:        "list_watchers",
		Description: "Lista las alertas automáticas configuradas (watchers).",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, toolPolicy{RequiresTenant: true, RequiredAnyScope: []string{scopeCompanionWatchersRead}}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if watcherUC == nil {
			return `{"watchers": []}`, nil
		}
		id := IdentityFromContext(ctx)
		if strings.TrimSpace(id.OrgID) == "" {
			return `{"error":"customer org context required"}`, nil
		}
		wList, err := watcherUC.List(ctx, id.OrgID)
		if err != nil {
			return "", fmt.Errorf("list watchers: %w", err)
		}
		b, err := json.Marshal(map[string]any{"watchers": wList})
		if err != nil {
			return "", fmt.Errorf("marshal watchers: %w", err)
		}
		return string(b), nil
	})

	// --- remember ---
	tk.add(ToolSchema{
		Name:        "remember",
		Description: "Guarda un hecho o preferencia para recordar en el futuro.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":     map[string]any{"type": "string", "description": "Clave identificadora (ej: preferred_contact, business_hours)"},
				"content": map[string]any{"type": "string", "description": "Contenido a recordar"},
				"scope":   map[string]any{"type": "string", "description": "user o org", "enum": []string{"user", "org"}},
			},
			"required": []string{"key", "content"},
		},
	}, toolPolicy{RequiresTenant: true, RequiresUser: true}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var input struct {
			Key     string `json:"key"`
			Content string `json:"content"`
			Scope   string `json:"scope"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if memUC == nil {
			return `{"error": "memory no configurado"}`, nil
		}
		id := IdentityFromContext(ctx)
		scope := memdomain.ScopeUser
		scopeID := tenantUserMemoryScopeID(id.OrgID, id.UserID)
		kind := memdomain.MemoryUserPreference
		if input.Scope == "org" {
			scope = memdomain.ScopeOrg
			scopeID = id.OrgID
			kind = memdomain.MemoryPlaybook
		}
		if scopeID == "" {
			return `{"error":"identity context required"}`, nil
		}
		_, err := memUC.Upsert(ctx, memory.UpsertInput{
			OrgID:           id.OrgID,
			UserID:          id.UserID,
			ProductSurface:  productSurfaceFromIdentity(id),
			Kind:            kind,
			MemoryType:      memdomain.TypeForKind(kind),
			ScopeType:       scope,
			ScopeID:         scopeID,
			Key:             input.Key,
			ContentText:     input.Content,
			ProvenanceJSON:  json.RawMessage(`{"source":"llm_tool","tool":"remember"}`),
			Confidence:      1,
			RetentionPolicy: "default",
		})
		if err != nil {
			return "", fmt.Errorf("remember: %w", err)
		}
		return `{"result": "guardado"}`, nil
	})

	// --- recall ---
	tk.add(ToolSchema{
		Name:        "recall",
		Description: "Busca en la memoria hechos o preferencias guardados previamente.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scope": map[string]any{"type": "string", "description": "user o org", "enum": []string{"user", "org"}},
			},
		},
	}, toolPolicy{RequiresTenant: true, RequiresUser: true}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var input struct {
			Scope string `json:"scope"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			input.Scope = "user"
		}
		if memUC == nil {
			return `{"memories": []}`, nil
		}
		id := IdentityFromContext(ctx)
		scope := memdomain.ScopeUser
		scopeID := tenantUserMemoryScopeID(id.OrgID, id.UserID)
		kind := memdomain.MemoryUserPreference
		if input.Scope == "org" {
			scope = memdomain.ScopeOrg
			scopeID = id.OrgID
			kind = memdomain.MemoryPlaybook
		}
		if scopeID == "" {
			return `{"memories": [], "error":"identity context required"}`, nil
		}
		entries, err := memUC.Find(ctx, memory.FindQuery{
			OrgID:          id.OrgID,
			UserID:         id.UserID,
			ProductSurface: productSurfaceFromIdentity(id),
			ScopeType:      scope,
			ScopeID:        scopeID,
			Kind:           kind,
			Limit:          10,
		})
		if err != nil {
			return "", fmt.Errorf("recall: %w", err)
		}
		type item struct {
			Key     string `json:"key"`
			Content string `json:"content"`
		}
		var items []item
		for _, e := range entries {
			items = append(items, item{Key: e.Key, Content: e.ContentText})
		}
		b, err := json.Marshal(map[string]any{"memories": items})
		if err != nil {
			return "", fmt.Errorf("marshal memories: %w", err)
		}
		return string(b), nil
	})

	return tk
}

func (tk *ToolKit) add(schema ToolSchema, policy toolPolicy, handler ToolHandler) {
	if tk.Handlers == nil {
		tk.Handlers = make(map[string]ToolHandler)
	}
	if tk.policies == nil {
		tk.policies = make(map[string]toolPolicy)
	}
	tk.Schemas = append(tk.Schemas, schema)
	tk.Handlers[schema.Name] = handler
	tk.policies[schema.Name] = policy
}

func (tk *ToolKit) setMetadata(name string, metadata ToolMetadata) {
	if tk == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if tk.metadata == nil {
		tk.metadata = make(map[string]ToolMetadata)
	}
	metadata.Name = name
	metadata.EvidenceRequired = cleanStringList(metadata.EvidenceRequired)
	metadata.Preconditions = cleanStringList(metadata.Preconditions)
	metadata.Postconditions = cleanStringList(metadata.Postconditions)
	metadata.ObservabilityTags = cleanStringList(metadata.ObservabilityTags)
	tk.metadata[name] = metadata
}

func (tk *ToolKit) ToolMetadata(name string) (ToolMetadata, bool) {
	if tk == nil || tk.metadata == nil {
		return ToolMetadata{}, false
	}
	metadata, ok := tk.metadata[strings.TrimSpace(name)]
	return metadata, ok
}

func cleanStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (tk *ToolKit) SchemasFor(identity IdentityChain, intent string) []ToolSchema {
	if tk == nil {
		return nil
	}
	out := make([]ToolSchema, 0, len(tk.Schemas))
	for _, schema := range tk.Schemas {
		if tk.CanUseTool(schema.Name, identity) {
			out = append(out, schema)
		}
	}
	return out
}

func (tk *ToolKit) CanUseTool(name string, identity IdentityChain) bool {
	if tk == nil {
		return false
	}
	policy, ok := tk.policies[name]
	if !ok {
		return true
	}
	if policy.RequiresTenant && strings.TrimSpace(identity.Tenant) == "" {
		return false
	}
	if policy.RequiresUser && strings.TrimSpace(identity.InitiatingUser) == "" {
		return false
	}
	if policy.RequiresTask && strings.TrimSpace(identity.TaskID) == "" {
		return false
	}
	if len(policy.RequiredAnyScope) > 0 && !hasAnyScope(identity.AuthScopes, policy.RequiredAnyScope...) {
		return false
	}
	return true
}

func productSurfaceFromIdentity(id Identity) string {
	if value := strings.TrimSpace(id.ProductSurface); value != "" {
		return value
	}
	return DefaultProductSurface
}

func summarizeApprovals(raw []byte) string {
	var approvals struct {
		Data []struct {
			ID         string `json:"id"`
			ActionType string `json:"action_type"`
			RiskLevel  string `json:"risk_level"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &approvals); err != nil {
		return "Aprobaciones pendientes: resumen no disponible"
	}
	count := len(approvals.Data)
	if count == 0 {
		return "Aprobaciones pendientes: 0"
	}
	byRisk := make(map[string]int)
	for _, approval := range approvals.Data {
		risk := strings.TrimSpace(approval.RiskLevel)
		if risk == "" {
			risk = "unknown"
		}
		byRisk[risk]++
	}
	return fmt.Sprintf("Aprobaciones pendientes: %d (por riesgo: %v)", count, byRisk)
}

// ExecuteTool ejecuta un tool por nombre. Regla dura: loguea pero nunca expone errores internos.
func (tk *ToolKit) ExecuteTool(ctx context.Context, name string, args json.RawMessage) string {
	handler, ok := tk.Handlers[name]
	if !ok {
		return fmt.Sprintf(`{"error": "tool %q no reconocido"}`, name)
	}
	result, err := handler(ctx, args)
	if err != nil {
		slog.Error("tool_execution_failed", "tool", name, "error", err)
		// Regla dura: no exponer error interno al LLM
		return fmt.Sprintf(`{"error": "no se pudo ejecutar %s en este momento"}`, name)
	}
	return result
}
