package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/productlimits"
	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

const maxToolRounds = 5

// defaultMaxTokens es el tope de tokens de salida por defecto cuando el perfil
// del agente no fija uno propio en su LLMConfig.
const defaultMaxTokens = 1024

// Orchestrator coordina LLM + tools + context para producir la respuesta del compañero.
type Orchestrator struct {
	provider          LLMProvider
	toolkit           *ToolKit
	ports             ContextPorts
	traces            TraceRepository // opcional; nil = no persiste (uso en tests)
	controls          RuntimeControls
	observer          ObservabilityRecorder
	costs             CostLedger
	agents            AgentResolver
	employees         VirployeeResolver
	agentProfiles     AgentProfileResolver
	installationGuard ProductInstallationGuard
	rateLimiter       productlimits.Limiter
	defaultAutonomy   AutonomyLevel // "" → A2 (default conservador)
	model             string
}

type ProductInstallationGuard interface {
	RequireActiveInstallation(ctx context.Context, orgID, productSurface, reason string) error
}

// NewOrchestrator crea el orquestador del runtime.
func NewOrchestrator(provider LLMProvider, toolkit *ToolKit, ports ContextPorts) *Orchestrator {
	return &Orchestrator{
		provider: provider,
		toolkit:  toolkit,
		ports:    ports,
	}
}

// SetTraceRepository inyecta el repositorio de persistencia de traces. Opcional.
func (o *Orchestrator) SetTraceRepository(repo TraceRepository) {
	o.traces = repo
}

// SetRuntimeControls inyecta políticas y contabilidad de runtime por tenant.
func (o *Orchestrator) SetRuntimeControls(repo RuntimeControls) {
	o.controls = repo
}

func (o *Orchestrator) SetObservabilityRecorder(repo ObservabilityRecorder) {
	o.observer = repo
}

func (o *Orchestrator) SetCostLedger(ledger CostLedger) {
	o.costs = ledger
}

func (o *Orchestrator) SetAgentResolver(resolver AgentResolver) {
	o.agents = resolver
}

func (o *Orchestrator) SetVirployeeResolver(resolver VirployeeResolver) {
	o.employees = resolver
}

func (o *Orchestrator) SetAgentProfileResolver(resolver AgentProfileResolver) {
	o.agentProfiles = resolver
}

func (o *Orchestrator) SetProductInstallationGuard(guard ProductInstallationGuard) {
	o.installationGuard = guard
}

func (o *Orchestrator) SetRateLimiter(limiter productlimits.Limiter) {
	o.rateLimiter = limiter
}

// SetDefaultAutonomy fija el nivel de autonomía por defecto del runtime.
// "" se trata como A2. Niveles fuera de A0..A5 se ignoran (queda A2).
func (o *Orchestrator) SetDefaultAutonomy(level AutonomyLevel) {
	o.defaultAutonomy = level
}

// SetModel fija el nombre del modelo configurado para trazabilidad.
func (o *Orchestrator) SetModel(model string) {
	o.model = model
}

// RunInput entrada para ejecutar el orquestador.
type RunInput struct {
	UserID         string
	OrgID          string
	AuthScopes     []string
	Identity       identityctx.IdentityContext
	ProductSurface string
	TenantID       string
	VirployeeID    string
	AgentID        string
	Message        string
	RouteHint      string
	Handoff        json.RawMessage
	Workspace      json.RawMessage          // contexto operativo de pantalla; gana sobre handoff.workspace
	Messages       []taskdomain.TaskMessage // hilo completo hasta ahora
	TaskID         *uuid.UUID               // opcional: vincula el trace a una task
}

// RunResult resultado del orquestador.
type RunResult struct {
	Reply string
	Trace RunTrace
}

// Run ejecuta el loop principal: context → LLM → tools → LLM → respuesta.
func (o *Orchestrator) Run(ctx context.Context, in RunInput) (RunResult, error) {
	requestIdentity := runIdentity(in)
	productSurface := requestIdentity.ProductSurface
	in.UserID = requestIdentity.EffectiveActorID()
	in.OrgID = requestIdentity.CustomerOrgID
	in.AuthScopes = append([]string(nil), requestIdentity.Scopes...)
	in.ProductSurface = productSurface
	identity := BuildIdentityChainFromContext(requestIdentity)
	if in.TaskID != nil && *in.TaskID != uuid.Nil {
		identity.TaskID = in.TaskID.String()
	}
	route := RouteAgentWithContext(in.Message, productSurface, in.RouteHint, in.Handoff, o.toolkit, identity, o.defaultAutonomy)
	if o.installationGuard != nil {
		if err := o.installationGuard.RequireActiveInstallation(ctx, in.OrgID, productSurface, "runtime_run"); err != nil {
			trace := o.rejectedProductInstallationTrace(ctx, in, identity, route, err.Error())
			return RunResult{Reply: "No puedo operar con ese producto porque no tiene una instalación activa para esta organización.", Trace: trace}, nil
		}
	}
	if err := productlimits.Enforce(ctx, o.rateLimiter, productlimits.Key{
		OrgID:          in.OrgID,
		ProductSurface: productSurface,
		Area:           productlimits.AreaRuntime,
	}, productlimits.DefaultLimit(productlimits.AreaRuntime)); err != nil {
		trace := o.rejectedRateLimitTrace(ctx, in, identity, route, err.Error())
		return RunResult{Reply: "La organización alcanzó el límite temporal de uso de runtime para este producto.", Trace: trace}, nil
	}
	if virployeeID := strings.TrimSpace(in.VirployeeID); virployeeID != "" {
		tenantID := strings.TrimSpace(in.TenantID)
		identity.VirployeeID = virployeeID
		if o.employees == nil {
			trace := o.rejectedVirployeeTrace(ctx, in, identity, route, "virployee resolver is not configured")
			return RunResult{Reply: "No puedo operar con ese Virployee porque el resolver no está configurado para este runtime.", Trace: trace}, nil
		}
		virployee, err := o.employees.ResolveRuntimeVirployee(ctx, tenantID, in.OrgID, productSurface, virployeeID)
		if err != nil {
			trace := o.rejectedVirployeeTrace(ctx, in, identity, route, "virployee not available: "+err.Error())
			return RunResult{Reply: "No puedo operar con ese Virployee para esta organización.", Trace: trace}, nil
		}
		identity.VirployeeID = virployee.VirployeeID
		var event *GuardrailEvent
		route, event = applyRuntimeVirployee(route, virployee)
		if event != nil {
			trace := o.rejectedVirployeeTrace(ctx, in, identity, route, event.Reason)
			trace.GuardrailEvents = []GuardrailEvent{*event}
			return RunResult{Reply: "No puedo operar con ese Virployee bajo la configuración actual.", Trace: trace}, nil
		}
		if strings.TrimSpace(virployee.ProfileID) != "" {
			if o.agentProfiles == nil {
				trace := o.rejectedVirployeeTrace(ctx, in, identity, route, "virployee profile resolver is not configured")
				return RunResult{Reply: "No puedo operar con ese Virployee porque su perfil no está configurado para este runtime.", Trace: trace}, nil
			}
			profile, err := o.agentProfiles.ResolveRuntimeAgentProfile(ctx, virployee.ProfileID)
			if err != nil {
				trace := o.rejectedVirployeeTrace(ctx, in, identity, route, "virployee profile not available: "+err.Error())
				return RunResult{Reply: "No puedo operar con ese Virployee porque su perfil no está disponible.", Trace: trace}, nil
			}
			route, event = applyRuntimeAgentProfile(route, profile)
			if profile.MaxAutonomy != "" {
				route.Autonomy = lowerAutonomy(route.Autonomy, profile.MaxAutonomy)
				route.Profile.MaxAutonomy = route.Autonomy
			}
			if event != nil {
				trace := o.rejectedVirployeeTrace(ctx, in, identity, route, event.Reason)
				trace.GuardrailEvents = []GuardrailEvent{*event}
				return RunResult{Reply: "No puedo operar con ese Virployee bajo la configuración actual.", Trace: trace}, nil
			}
		}
	}
	if agentID := strings.TrimSpace(in.AgentID); agentID != "" && strings.TrimSpace(in.VirployeeID) == "" {
		identity.AgentID = agentID
		if o.agents == nil {
			route.Profile.AgentID = agentID
			trace := o.rejectedAgentTrace(ctx, in, identity, route, "agent resolver is not configured")
			return RunResult{Reply: "No puedo operar con ese empleado IA porque la flota no está configurada para este runtime.", Trace: trace}, nil
		}
		agent, err := o.agents.ResolveRuntimeAgent(ctx, in.OrgID, productSurface, agentID)
		if err != nil {
			route.Profile.AgentID = agentID
			trace := o.rejectedAgentTrace(ctx, in, identity, route, "agent not available: "+err.Error())
			return RunResult{Reply: "No puedo operar con ese empleado IA para esta organización.", Trace: trace}, nil
		}
		var event *GuardrailEvent
		route, event = applyRuntimeAgent(route, agent)
		if agent.MaxAutonomy != "" {
			route.Autonomy = lowerAutonomy(route.Autonomy, agent.MaxAutonomy)
			route.Profile.MaxAutonomy = route.Autonomy
		}
		identity.AgentID = agent.AgentID
		if event != nil {
			trace := o.rejectedAgentTrace(ctx, in, identity, route, event.Reason)
			trace.GuardrailEvents = []GuardrailEvent{*event}
			return RunResult{Reply: "No puedo operar con ese empleado IA bajo la configuración actual.", Trace: trace}, nil
		}
		if strings.TrimSpace(agent.ProfileID) != "" {
			if o.agentProfiles == nil {
				trace := o.rejectedAgentTrace(ctx, in, identity, route, "agent profile resolver is not configured")
				return RunResult{Reply: "No puedo operar con ese empleado IA porque su perfil no está configurado para este runtime.", Trace: trace}, nil
			}
			profile, err := o.agentProfiles.ResolveRuntimeAgentProfile(ctx, agent.ProfileID)
			if err != nil {
				trace := o.rejectedAgentTrace(ctx, in, identity, route, "agent profile not available: "+err.Error())
				return RunResult{Reply: "No puedo operar con ese empleado IA porque su perfil no está disponible.", Trace: trace}, nil
			}
			route, event = applyRuntimeAgentProfile(route, profile)
			if profile.MaxAutonomy != "" {
				route.Autonomy = lowerAutonomy(route.Autonomy, profile.MaxAutonomy)
				route.Profile.MaxAutonomy = route.Autonomy
			}
			if event != nil {
				trace := o.rejectedAgentTrace(ctx, in, identity, route, event.Reason)
				trace.GuardrailEvents = []GuardrailEvent{*event}
				return RunResult{Reply: "No puedo operar con ese empleado IA bajo la configuración actual.", Trace: trace}, nil
			}
		}
	}
	// El perfil del agente puede fijar su propio modelo y tope de tokens en
	// su LLMConfig; si los deja vacíos/cero caemos al default global del runtime.
	modelName := firstNonEmpty(strings.TrimSpace(route.Profile.LLM.Model), o.model, DefaultGeminiModel)
	maxTokens := effectiveMaxTokens(route.Profile.LLM.MaxTokens)
	ctx, runSpan := startRunSpan(ctx, in.OrgID, productSurface, identity.AgentID, modelName)
	defer runSpan.End()
	policy := defaultRuntimePolicy(in.OrgID)
	currentUsage := TenantRuntimeUsage{OrgID: in.OrgID, Period: runtimeUsagePeriod(time.Now())}
	if o.controls != nil && in.OrgID != "" {
		if loaded, err := o.controls.GetRuntimePolicy(ctx, in.OrgID); err == nil {
			policy = loaded
		} else if !errors.Is(err, ErrRuntimePolicyNotFound) {
			slog.Error("runtime_policy_lookup_failed", "customer_org_id", in.OrgID, "error", err)
			policy.Enabled = false
			policy.KillSwitch = true
		}
		if usage, err := o.controls.GetRuntimeUsage(ctx, in.OrgID, currentUsage.Period); err == nil {
			currentUsage = usage
		} else {
			slog.Error("runtime_usage_lookup_failed", "customer_org_id", in.OrgID, "period", currentUsage.Period, "error", err)
		}
	}
	if policy.ControlPlane.MonthlyCostBudgetCents > 0 && o.costs != nil && in.OrgID != "" {
		summary, err := o.costs.GetCostSummary(ctx, in.OrgID, productSurface, currentUsage.Period, 1)
		if err != nil {
			slog.Error("runtime_cost_budget_lookup_failed", "customer_org_id", in.OrgID, "period", currentUsage.Period, "error", err)
			policy.Enabled = false
			policy.KillSwitch = true
		} else if summary.EstimatedCostCents >= policy.ControlPlane.MonthlyCostBudgetCents {
			trace := o.rejectedBudgetTrace(ctx, in, identity, route, modelName, GuardrailEvent{
				Type:   "tenant_runtime_budget",
				Target: "cost",
				Reason: "monthly cost budget exhausted",
			}, "monthly_cost_budget_exhausted")
			return RunResult{Reply: "La organización alcanzó el presupuesto mensual de costo para Companion.", Trace: trace}, nil
		}
	}
	if productPolicy, ok := productRuntimePolicyFor(policy, productSurface); ok && o.costs != nil && in.OrgID != "" {
		if productPolicy.MonthlyCostBudgetCents > 0 || productPolicy.MonthlyToolCallBudget > 0 {
			summary, err := o.costs.GetCostSummary(ctx, in.OrgID, productSurface, currentUsage.Period, 1)
			if err != nil {
				slog.Error("runtime_product_budget_lookup_failed", "customer_org_id", in.OrgID, "product_surface", productSurface, "period", currentUsage.Period, "error", err)
				policy.Enabled = false
				policy.KillSwitch = true
			} else if productPolicy.MonthlyCostBudgetCents > 0 && summary.EstimatedCostCents >= productPolicy.MonthlyCostBudgetCents {
				trace := o.rejectedBudgetTrace(ctx, in, identity, route, modelName, GuardrailEvent{
					Type:   "product_runtime_budget",
					Target: "cost:" + productSurface,
					Reason: "monthly product cost budget exhausted",
				}, "monthly_product_cost_budget_exhausted")
				return RunResult{Reply: "La organización alcanzó el presupuesto mensual de costo para este producto.", Trace: trace}, nil
			} else if productPolicy.MonthlyToolCallBudget > 0 && summary.ToolCalls >= productPolicy.MonthlyToolCallBudget {
				trace := o.rejectedBudgetTrace(ctx, in, identity, route, modelName, GuardrailEvent{
					Type:   "product_runtime_budget",
					Target: "tools:" + productSurface,
					Reason: "monthly product tool call budget exhausted",
				}, "monthly_product_tool_budget_exhausted")
				return RunResult{Reply: "La organización alcanzó el presupuesto mensual de tools para este producto.", Trace: trace}, nil
			}
		}
	}
	decision := applyRuntimePolicy(policy, currentUsage, route, modelName)
	route = decision.Route
	runWorkspace := effectiveWorkspace(in)
	promptVersion := effectivePromptVersion(route)
	trace := RunTrace{
		RunID:          uuid.NewString(),
		IdentityChain:  identity,
		Intent:         route.Intent,
		ProductSurface: route.Product,
		AutonomyLevel:  route.Autonomy,
		PromptVersion:  promptVersion,
		Model:          modelName,
		StartedAt:      time.Now().UTC(),
	}
	o.recordObservabilityEvent(ctx, trace, in, "run", "started", map[string]any{
		"model":                  modelName,
		"prompt_version":         promptVersion,
		"agent_profile":          route.Profile,
		"agent_id":               identity.AgentID,
		"allowed_tools":          route.AllowedTools,
		"workspace":              runWorkspace,
		"runtime_policy_version": policy.SettingsVersion,
		"control_plane": map[string]any{
			"max_risk_class":        policy.ControlPlane.MaxRiskClass,
			"trace_level":           policy.ControlPlane.Observability.TraceLevel,
			"redaction_mode":        policy.ControlPlane.Observability.RedactionMode,
			"replay_enabled":        policy.ControlPlane.Observability.ReplayEnabled,
			"capture_prompts":       policy.ControlPlane.Observability.CapturePrompts,
			"capture_tool_payloads": policy.ControlPlane.Observability.CaptureToolPayloads,
		},
	})
	if decision.Event != nil {
		trace.GuardrailEvents = append(trace.GuardrailEvents, *decision.Event)
		o.recordObservabilityEvent(ctx, trace, in, "guardrail", runtimeGuardrailEventName(*decision.Event), map[string]any{
			"target": decision.Event.Target,
			"reason": decision.Event.Reason,
		})
		if decision.Reply != "" {
			trace.CompletedAt = time.Now().UTC()
			slog.Warn("runtime_tenant_policy_rejected", "run_id", trace.RunID, "type", decision.Event.Type, "reason", decision.Event.Reason)
			o.finishTrace(ctx, trace, in, decision.Event.Reason)
			return RunResult{Reply: decision.Reply, Trace: trace}, nil
		}
	}
	var allowedSchemas []ToolSchema
	if o.toolkit != nil {
		allowedSchemas = filterSchemasForRoute(o.toolkit.SchemasFor(identity, route.Intent), route)
	}
	if event := CheckPromptInjection(in.Message); event != nil {
		trace.GuardrailEvents = append(trace.GuardrailEvents, *event)
		o.recordObservabilityEvent(ctx, trace, in, "guardrail", "prompt_injection", map[string]any{
			"target": event.Target,
			"reason": event.Reason,
		})
		trace.CompletedAt = time.Now().UTC()
		slog.Warn("runtime_guardrail_rejected", "run_id", trace.RunID, "type", event.Type, "reason", event.Reason)
		o.finishTrace(ctx, trace, in, "")
		return RunResult{
			Reply: "No puedo continuar con instrucciones que intentan modificar mis reglas internas. Si necesitás hacer una acción concreta, reformulá el pedido con el objetivo de negocio.",
			Trace: trace,
		}, nil
	}

	// 1. Ensamblar contexto
	assembled := AssembleContext(ctx, o.ports, in.UserID, in.OrgID, productSurface, in.AuthScopes, in.TaskID, in.Messages)

	// 2. Construir mensajes para el LLM
	systemPrompt := SystemPrompt()
	if profilePrompt := strings.TrimSpace(route.Profile.SystemPrompt); profilePrompt != "" {
		systemPrompt += "\n\nAgent profile instructions:\n" + profilePrompt
	}
	systemPrompt += "\n\nRuntime control plane:\n" + runtimeSummary(trace.IdentityChain, route)
	if assembled.Summary != "" {
		systemPrompt += "\n\nContexto actual:\n" + assembled.Summary
	}
	if handoff := compactHandoffForPrompt(in.Handoff); handoff != "" {
		systemPrompt += "\n\nContexto de la pantalla/producto:\n" + handoff
	}
	if guidance := pontiRuntimeGuidance(productSurface, in.RouteHint, in.Message, allowedSchemas); guidance != "" {
		systemPrompt += "\n\nGuía operativa Ponti:\n" + guidance
	}

	llmMessages := make([]LLMMessage, 0, len(assembled.History)+1)
	llmMessages = append(llmMessages, assembled.History...)
	llmMessages = append(llmMessages, LLMMessage{Role: "user", Content: in.Message})

	// 3. Loop de tool calling (máximo maxToolRounds rondas)
	for round := 0; round < maxToolRounds; round++ {
		recordChatInputUsage(&trace.Usage, systemPrompt, llmMessages)
		trace.Usage.AddLLMCall()
		o.recordObservabilityEvent(ctx, trace, in, "llm", "request", map[string]any{
			"round":        round,
			"model":        modelName,
			"tools_count":  len(allowedSchemas),
			"max_tokens":   maxTokens,
			"input_tokens": trace.Usage.EstimatedInputTokens,
		})
		resp, err := o.provider.Chat(ctx, ChatRequest{
			SystemPrompt: systemPrompt,
			Messages:     llmMessages,
			Tools:        allowedSchemas,
			MaxTokens:    maxTokens,
		})
		if err != nil {
			slog.Error("llm_chat_failed", "round", round, "error", err)
			trace.CompletedAt = time.Now().UTC()
			o.finishTrace(ctx, trace, in, err.Error())
			return RunResult{Trace: trace}, fmt.Errorf("gemini chat failed: %w", err)
		}
		trace.Usage.AddOutput(resp.Text)
		for _, tc := range resp.ToolCalls {
			trace.Usage.AddOutput(tc.Name)
			trace.Usage.AddOutput(string(tc.Args))
		}

		// Si no hay tool calls pero Ponti necesita evidencia operativa,
		// ejecutamos una lectura compacta antes de redactar la respuesta final.
		if len(resp.ToolCalls) == 0 {
			if forced, ok := o.pontiForcedReadToolCall(in, allowedSchemas, route, round, len(trace.ToolCalls)); ok {
				slog.Info("ponti_forced_read_tool_call", "tool", forced.Name, "route_hint", in.RouteHint, "intent", route.Intent)
				resp.ToolCalls = []LLMToolCall{forced}
				trace.Usage.AddOutput(forced.Name)
				trace.Usage.AddOutput(string(forced.Args))
				if strings.TrimSpace(resp.Text) == "" {
					resp.Text = "Voy a consultar Ponti antes de responder."
				}
			} else {
				reply := resp.Text
				if reply == "" {
					reply = "No pude generar una respuesta en este momento."
				}
				trace.CompletedAt = time.Now().UTC()
				o.finishTrace(ctx, trace, in, "")
				return RunResult{Reply: reply, Trace: trace}, nil
			}
		}

		// Hay tool calls: ejecutar y agregar resultados
		// Agregar mensaje del asistente con tool calls
		llmMessages = append(llmMessages, LLMMessage{
			Role:      "assistant",
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
		})

		// Ejecutar cada tool y agregar resultado
		for _, tc := range resp.ToolCalls {
			slog.Info("tool_call", "tool", tc.Name, "round", round)
			toolStart := time.Now()
			if event := ValidateToolPolicy(tc.Name, tc.Args, trace.IdentityChain, route, o.toolkit); event != nil {
				slog.Warn("tool_call_guardrail_rejected", "tool", tc.Name, "type", event.Type, "reason", event.Reason)
				trace.GuardrailEvents = append(trace.GuardrailEvents, *event)
				o.recordObservabilityEvent(ctx, trace, in, "tool", "rejected", map[string]any{
					"tool":   tc.Name,
					"reason": event.Reason,
					"args":   json.RawMessage(tc.Args),
				})
				trace.ToolCalls = append(trace.ToolCalls, ToolTrace{
					Name:           tc.Name,
					ToolCallID:     tc.ID,
					Allowed:        false,
					DecisionReason: event.Reason,
					DurationMS:     time.Since(toolStart).Milliseconds(),
				})
				llmMessages = append(llmMessages, LLMMessage{
					Role:       "tool",
					Content:    fmt.Sprintf(`{"error":"tool call rejected: %s"}`, event.Reason),
					ToolCallID: tc.ID,
				})
				continue
			}

			// Inyectar identidad en context para que tools usen IDs reales.
			toolCtx := WithIdentityContext(ctx, requestIdentity)
			if in.TaskID != nil {
				toolCtx = WithTaskID(toolCtx, *in.TaskID)
			}
			toolCtx = WithAllowedTools(toolCtx, route.AllowedTools)
			toolCtx = WithWorkspace(toolCtx, runWorkspace)
			toolCtx, cancel := context.WithTimeout(toolCtx, 15*time.Second)
			result := o.toolkit.ExecuteTool(toolCtx, tc.Name, tc.Args)
			cancel()
			durationMS := time.Since(toolStart).Milliseconds()
			trace.Usage.AddToolCall(result)
			trace.ToolCalls = append(trace.ToolCalls, ToolTrace{
				Name:           tc.Name,
				ToolCallID:     tc.ID,
				Allowed:        true,
				DecisionReason: "allowed_by_runtime_policy",
				DurationMS:     durationMS,
				Result:         compactToolResult(result),
			})
			metadata, _ := o.toolkit.ToolMetadata(tc.Name)
			o.recordObservabilityEvent(ctx, trace, in, "tool", "executed", map[string]any{
				"tool":               tc.Name,
				"tool_call_id":       tc.ID,
				"duration_ms":        durationMS,
				"capability_id":      metadata.CapabilityID,
				"capability_version": metadata.CapabilityVersion,
				"risk_class":         metadata.RiskClass,
				"side_effect":        metadata.SideEffectClass,
				"requires_nexus":     metadata.RequiresNexusApproval,
				"args":               json.RawMessage(tc.Args),
				"result":             result,
			})

			llmMessages = append(llmMessages, LLMMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	// Si llegamos acá, agotamos las rondas
	slog.Warn("orchestrator_max_rounds_reached", "rounds", maxToolRounds)
	trace.CompletedAt = time.Now().UTC()
	o.finishTrace(ctx, trace, in, "max_tool_rounds_exhausted")
	return RunResult{Trace: trace}, fmt.Errorf("gemini tool loop exhausted after %d rounds", maxToolRounds)
}

func compactHandoffForPrompt(raw json.RawMessage) string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// effectiveMaxTokens devuelve el tope de tokens del perfil cuando es positivo,
// o el default del runtime cuando el perfil no lo configura.
func effectiveMaxTokens(profileMaxTokens int) int {
	if profileMaxTokens > 0 {
		return profileMaxTokens
	}
	return defaultMaxTokens
}

func effectivePromptVersion(route AgentRoute) string {
	profileID := strings.TrimSpace(route.Profile.ID)
	version := strings.TrimSpace(route.Profile.Version)
	if profileID == "" || version == "" || strings.TrimSpace(route.Profile.ProfileSnapshotID) == "" {
		return SystemPromptVersion
	}
	return SystemPromptVersion + "+" + profileID + ":" + version
}

func compactToolResult(result string) json.RawMessage {
	result = strings.TrimSpace(result)
	if result == "" {
		return nil
	}
	if len(result) > 4000 {
		result = result[:4000]
	}
	if json.Valid([]byte(result)) {
		return json.RawMessage(result)
	}
	raw, _ := json.Marshal(map[string]any{"text": result})
	return raw
}

func (o *Orchestrator) rejectedProductInstallationTrace(ctx context.Context, in RunInput, identity IdentityChain, route AgentRoute, reason string) RunTrace {
	now := time.Now().UTC()
	trace := RunTrace{
		RunID:          uuid.NewString(),
		IdentityChain:  identity,
		Intent:         route.Intent,
		ProductSurface: route.Product,
		AutonomyLevel:  route.Autonomy,
		PromptVersion:  SystemPromptVersion,
		Model:          firstNonEmpty(o.model, DefaultGeminiModel),
		StartedAt:      now,
		CompletedAt:    now,
		GuardrailEvents: []GuardrailEvent{{
			Type:   "product_installation",
			Target: "product:" + route.Product,
			Reason: reason,
		}},
	}
	o.recordObservabilityEvent(ctx, trace, in, "guardrail", "product_installation_required", map[string]any{
		"target":          "product:" + route.Product,
		"reason":          reason,
		"org_id":          in.OrgID,
		"product_surface": route.Product,
	})
	o.finishTrace(ctx, trace, in, reason)
	return trace
}

func (o *Orchestrator) rejectedRateLimitTrace(ctx context.Context, in RunInput, identity IdentityChain, route AgentRoute, reason string) RunTrace {
	now := time.Now().UTC()
	trace := RunTrace{
		RunID:          uuid.NewString(),
		IdentityChain:  identity,
		Intent:         route.Intent,
		ProductSurface: route.Product,
		AutonomyLevel:  route.Autonomy,
		PromptVersion:  SystemPromptVersion,
		Model:          firstNonEmpty(o.model, DefaultGeminiModel),
		StartedAt:      now,
		CompletedAt:    now,
		GuardrailEvents: []GuardrailEvent{{
			Type:   "product_rate_limit",
			Target: "runtime:" + route.Product,
			Reason: reason,
		}},
	}
	o.recordObservabilityEvent(ctx, trace, in, "guardrail", "product_rate_limit", map[string]any{
		"target":          "runtime:" + route.Product,
		"reason":          reason,
		"org_id":          in.OrgID,
		"product_surface": route.Product,
	})
	o.finishTrace(ctx, trace, in, reason)
	return trace
}

func (o *Orchestrator) rejectedBudgetTrace(ctx context.Context, in RunInput, identity IdentityChain, route AgentRoute, modelName string, event GuardrailEvent, errMsg string) RunTrace {
	now := time.Now().UTC()
	trace := RunTrace{
		RunID:           uuid.NewString(),
		IdentityChain:   identity,
		Intent:          route.Intent,
		ProductSurface:  route.Product,
		AutonomyLevel:   route.Autonomy,
		PromptVersion:   SystemPromptVersion,
		Model:           modelName,
		StartedAt:       now,
		CompletedAt:     now,
		GuardrailEvents: []GuardrailEvent{event},
	}
	o.recordObservabilityEvent(ctx, trace, in, "guardrail", runtimeGuardrailEventName(event), map[string]any{
		"target":          event.Target,
		"reason":          event.Reason,
		"org_id":          in.OrgID,
		"product_surface": route.Product,
	})
	o.finishTrace(ctx, trace, in, errMsg)
	return trace
}

func (o *Orchestrator) rejectedAgentTrace(ctx context.Context, in RunInput, identity IdentityChain, route AgentRoute, reason string) RunTrace {
	now := time.Now().UTC()
	trace := RunTrace{
		RunID:          uuid.NewString(),
		IdentityChain:  identity,
		Intent:         route.Intent,
		ProductSurface: route.Product,
		AutonomyLevel:  route.Autonomy,
		PromptVersion:  SystemPromptVersion,
		Model:          firstNonEmpty(o.model, DefaultGeminiModel),
		StartedAt:      now,
		CompletedAt:    now,
	}
	if reason != "" {
		trace.GuardrailEvents = append(trace.GuardrailEvents, GuardrailEvent{Type: "agent_fleet", Target: "agent:" + identity.AgentID, Reason: reason})
	}
	o.recordObservabilityEvent(ctx, trace, in, "guardrail", "agent_fleet", map[string]any{
		"agent_id": identity.AgentID,
		"reason":   reason,
	})
	o.finishTrace(ctx, trace, in, reason)
	return trace
}

func (o *Orchestrator) rejectedVirployeeTrace(ctx context.Context, in RunInput, identity IdentityChain, route AgentRoute, reason string) RunTrace {
	now := time.Now().UTC()
	trace := RunTrace{
		RunID:          uuid.NewString(),
		IdentityChain:  identity,
		Intent:         route.Intent,
		ProductSurface: route.Product,
		AutonomyLevel:  route.Autonomy,
		PromptVersion:  SystemPromptVersion,
		Model:          firstNonEmpty(o.model, DefaultGeminiModel),
		StartedAt:      now,
		CompletedAt:    now,
	}
	if reason != "" {
		trace.GuardrailEvents = append(trace.GuardrailEvents, GuardrailEvent{Type: "virployee", Target: "virployee:" + identity.VirployeeID, Reason: reason})
	}
	o.recordObservabilityEvent(ctx, trace, in, "guardrail", "virployee", map[string]any{
		"virployee_id": identity.VirployeeID,
		"reason":       reason,
	})
	o.finishTrace(ctx, trace, in, reason)
	return trace
}

func runtimeGuardrailEventName(event GuardrailEvent) string {
	switch event.Type {
	case "tenant_runtime_budget", "product_runtime_budget":
		return event.Type
	default:
		return "runtime_policy"
	}
}

func runIdentity(in RunInput) identityctx.IdentityContext {
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
	if surface := in.ProductSurface; surface != "" {
		id.ProductSurface = surface
	}
	return id.WithProductSurface(id.ProductSurface)
}

// persistTrace guarda el trace si hay repo configurado. Falla en silencio (con log) para
// no bloquear la respuesta al usuario por un problema de persistencia.
func (o *Orchestrator) persistTrace(ctx context.Context, trace RunTrace, in RunInput, errMsg string) {
	if o.traces == nil {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := o.traces.Save(saveCtx, trace, in.OrgID, in.UserID, in.TaskID, errMsg); err != nil {
		slog.Error("run_trace_persist_failed", "run_id", trace.RunID, "error", err)
	}
}

func (o *Orchestrator) finishTrace(ctx context.Context, trace RunTrace, in RunInput, errMsg string) {
	o.recordObservabilityEvent(ctx, trace, in, "run", "completed", map[string]any{
		"error":            errMsg,
		"usage":            trace.Usage,
		"tool_calls":       len(trace.ToolCalls),
		"guardrail_events": len(trace.GuardrailEvents),
		"duration_ms":      runDurationMS(trace),
	})
	o.persistTrace(ctx, trace, in, errMsg)
	recordRunMetrics(ctx, trace, in.OrgID)
	o.recordCostEvents(ctx, trace, in)
	if o.controls == nil || in.OrgID == "" {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := o.controls.AddRuntimeUsage(recordCtx, in.OrgID, runtimeUsagePeriod(trace.StartedAt), trace.Usage); err != nil {
		slog.Error("runtime_usage_record_failed", "run_id", trace.RunID, "customer_org_id", in.OrgID, "error", err)
	}
}

func runDurationMS(trace RunTrace) int64 {
	if trace.StartedAt.IsZero() || trace.CompletedAt.IsZero() {
		return 0
	}
	duration := trace.CompletedAt.Sub(trace.StartedAt).Milliseconds()
	if duration < 0 {
		return 0
	}
	return duration
}

func (o *Orchestrator) recordObservabilityEvent(ctx context.Context, trace RunTrace, in RunInput, eventType, eventName string, payload map[string]any) {
	if o.observer == nil {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := o.observer.RecordObservabilityEvent(recordCtx, newObservabilityEvent(trace, in, eventType, eventName, payload)); err != nil {
		slog.Error("observability_event_record_failed", "run_id", trace.RunID, "event", eventName, "error", err)
	}
}

func (o *Orchestrator) recordCostEvents(ctx context.Context, trace RunTrace, in RunInput) {
	if o.costs == nil || strings.TrimSpace(in.OrgID) == "" {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := o.costs.RecordCostEvent(recordCtx, costEventForRun(trace, in)); err != nil {
		slog.Error("cost_event_record_failed", "run_id", trace.RunID, "event", "run", "error", err)
	}
	if len(trace.ToolCalls) > 0 {
		if err := o.costs.RecordCostEvent(recordCtx, costEventForTools(trace, in)); err != nil {
			slog.Error("cost_event_record_failed", "run_id", trace.RunID, "event", "tool", "error", err)
		}
	}
}

func recordChatInputUsage(usage *RunUsage, systemPrompt string, messages []LLMMessage) {
	if usage == nil {
		return
	}
	usage.AddInput(systemPrompt)
	for _, msg := range messages {
		usage.AddInput(msg.Content)
		for _, tc := range msg.ToolCalls {
			usage.AddInput(tc.Name)
			usage.AddInput(string(tc.Args))
		}
	}
}
