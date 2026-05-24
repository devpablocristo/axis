package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/identityctx"
	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

const maxToolRounds = 5

// Orchestrator coordina LLM + tools + context para producir la respuesta del compañero.
type Orchestrator struct {
	provider        LLMProvider
	toolkit         *ToolKit
	ports           ContextPorts
	traces          TraceRepository // opcional; nil = no persiste (uso en tests)
	governance      RuntimeGovernance
	defaultAutonomy AutonomyLevel // "" → A2 (default conservador)
	model           string
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

// SetRuntimeGovernance inyecta políticas y contabilidad de runtime por tenant.
func (o *Orchestrator) SetRuntimeGovernance(repo RuntimeGovernance) {
	o.governance = repo
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
	Message        string
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
	route := RouteAgent(in.Message, productSurface, o.toolkit, identity, o.defaultAutonomy)
	modelName := firstNonEmpty(o.model, DefaultGeminiModel)
	policy := defaultRuntimePolicy(in.OrgID)
	currentUsage := TenantRuntimeUsage{OrgID: in.OrgID, Period: runtimeUsagePeriod(time.Now())}
	if o.governance != nil && in.OrgID != "" {
		if loaded, err := o.governance.GetRuntimePolicy(ctx, in.OrgID); err == nil {
			policy = loaded
		} else if !errors.Is(err, ErrRuntimePolicyNotFound) {
			slog.Error("runtime_policy_lookup_failed", "customer_org_id", in.OrgID, "error", err)
			policy.Enabled = false
			policy.KillSwitch = true
		}
		if usage, err := o.governance.GetRuntimeUsage(ctx, in.OrgID, currentUsage.Period); err == nil {
			currentUsage = usage
		} else {
			slog.Error("runtime_usage_lookup_failed", "customer_org_id", in.OrgID, "period", currentUsage.Period, "error", err)
		}
	}
	decision := applyRuntimePolicy(policy, currentUsage, route, modelName)
	route = decision.Route
	trace := RunTrace{
		RunID:          uuid.NewString(),
		IdentityChain:  identity,
		Intent:         route.Intent,
		ProductSurface: route.Product,
		AutonomyLevel:  route.Autonomy,
		PromptVersion:  SystemPromptVersion,
		Model:          modelName,
		StartedAt:      time.Now().UTC(),
	}
	if decision.Event != nil {
		trace.GuardrailEvents = append(trace.GuardrailEvents, *decision.Event)
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
	systemPrompt += "\n\nRuntime control plane:\n" + runtimeSummary(trace.IdentityChain, route)
	if assembled.Summary != "" {
		systemPrompt += "\n\nContexto actual:\n" + assembled.Summary
	}

	llmMessages := make([]LLMMessage, 0, len(assembled.History)+1)
	llmMessages = append(llmMessages, assembled.History...)
	llmMessages = append(llmMessages, LLMMessage{Role: "user", Content: in.Message})

	// 3. Loop de tool calling (máximo maxToolRounds rondas)
	for round := 0; round < maxToolRounds; round++ {
		recordChatInputUsage(&trace.Usage, systemPrompt, llmMessages)
		trace.Usage.AddLLMCall()
		resp, err := o.provider.Chat(ctx, ChatRequest{
			SystemPrompt: systemPrompt,
			Messages:     llmMessages,
			Tools:        allowedSchemas,
			MaxTokens:    1024,
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

		// Si no hay tool calls, tenemos la respuesta final
		if len(resp.ToolCalls) == 0 {
			reply := resp.Text
			if reply == "" {
				reply = "No pude generar una respuesta en este momento."
			}
			trace.CompletedAt = time.Now().UTC()
			o.finishTrace(ctx, trace, in, "")
			return RunResult{Reply: reply, Trace: trace}, nil
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
			toolCtx, cancel := context.WithTimeout(toolCtx, 15*time.Second)
			result := o.toolkit.ExecuteTool(toolCtx, tc.Name, tc.Args)
			cancel()
			trace.Usage.AddToolCall(result)
			trace.ToolCalls = append(trace.ToolCalls, ToolTrace{
				Name:           tc.Name,
				ToolCallID:     tc.ID,
				Allowed:        true,
				DecisionReason: "allowed_by_runtime_policy",
				DurationMS:     time.Since(toolStart).Milliseconds(),
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
	o.persistTrace(ctx, trace, in, errMsg)
	if o.governance == nil || in.OrgID == "" {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := o.governance.AddRuntimeUsage(recordCtx, in.OrgID, runtimeUsagePeriod(trace.StartedAt), trace.Usage); err != nil {
		slog.Error("runtime_usage_record_failed", "run_id", trace.RunID, "customer_org_id", in.OrgID, "error", err)
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
