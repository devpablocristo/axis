package runtime

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

func classifyPontiRouteIntent(productSurface, message, routeHint string, handoff json.RawMessage) string {
	if !strings.EqualFold(strings.TrimSpace(productSurface), "ponti") {
		return ""
	}
	screenHint := pontiNormalize(strings.TrimSpace(routeHint + " " + pontiHandoffRouteHint(handoff)))
	if screenHint == "" {
		return ""
	}
	text := pontiNormalize(screenHint + " " + message)
	switch {
	case pontiHasAny(text, "reports", "informe", "informes", "economico", "resultado", "resultados", "campana", "contribucion"):
		return "ponti.reports"
	case pontiHasAny(text, "stock", "existencia", "existencias"):
		return "ponti.stock"
	case pontiHasAny(text, "labors", "labor", "labores", "workorders", "work orders", "orden", "ordenes", "ot"):
		return "ponti.workorders"
	case pontiHasAny(text, "lots", "lote", "lotes"):
		return "ponti.lots"
	case pontiHasAny(text, "supplies", "insumo", "insumos"):
		return "ponti.supplies"
	case pontiHasAny(text, "insight", "insights"):
		return "ponti.insights"
	case pontiHasAny(text, "dashboard", "tablero", "resumen operativo", "general", "campaigns", "campanas"):
		return "ponti.dashboard"
	default:
		return "ponti.operational"
	}
}

func pontiRuntimeGuidance(productSurface, routeHint, message string, allowedSchemas []ToolSchema) string {
	if !strings.EqualFold(strings.TrimSpace(productSurface), "ponti") {
		return ""
	}
	available := pontiAvailableToolSet(allowedSchemas)
	if len(available) == 0 {
		return ""
	}
	suggested := pontiSuggestedReadTools(routeHint, message, available)
	var lines []string
	lines = append(lines,
		"- Si el usuario pide datos operativos de Ponti, usá primero una tool Ponti permitida y respondé con evidencia; no digas que no tenés acceso sin intentar consultar.",
		"- Pasá el objeto workspace del contexto de pantalla al argumento workspace de la tool.",
		"- Si la tool devuelve datos vacíos, explicá que Ponti no devolvió registros para ese workspace/filtro.",
	)
	if len(suggested) > 0 {
		lines = append(lines, "- Tools sugeridas para este contexto: "+strings.Join(suggested, ", ")+".")
	}
	return strings.Join(lines, "\n")
}

func (o *Orchestrator) pontiForcedReadToolCall(in RunInput, allowedSchemas []ToolSchema, route AgentRoute, round int, executedToolCalls int) (LLMToolCall, bool) {
	if round != 0 || executedToolCalls != 0 {
		return LLMToolCall{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(route.Product), "ponti") {
		return LLMToolCall{}, false
	}
	available := pontiAvailableToolSet(allowedSchemas)
	if len(available) == 0 {
		return LLMToolCall{}, false
	}
	toolName := ""
	for _, candidate := range pontiSuggestedReadTools(in.RouteHint, in.Message, available) {
		if _, ok := available[candidate]; ok {
			toolName = candidate
			break
		}
	}
	if toolName == "" {
		return LLMToolCall{}, false
	}
	workspace := effectiveWorkspace(in)
	if !pontiToolCanRunWithoutWorkspace(toolName) && len(workspace) == 0 {
		return LLMToolCall{}, false
	}
	args := map[string]any{}
	if len(workspace) > 0 {
		args["workspace"] = workspace
	}
	switch toolName {
	case "ponti_workorders_list", "ponti_lots_summary", "ponti_supplies_summary":
		args["limit"] = 25
	case "ponti_insights_list":
		args["limit"] = 25
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return LLMToolCall{}, false
	}
	return LLMToolCall{
		ID:   "runtime-prefetch-" + uuid.NewString(),
		Name: toolName,
		Args: raw,
	}, true
}

func pontiSuggestedReadTools(routeHint, message string, available map[string]struct{}) []string {
	text := pontiNormalize(routeHint + " " + message)
	var candidates []string
	switch {
	case pontiHasAny(text, "reports", "informe", "informes", "economico", "resultado", "resultados", "campana", "contribucion"):
		candidates = []string{"ponti_reports_summary_results_summary", "ponti_reports_investor_contribution_summary", "ponti_reports_field_crop_summary"}
	case pontiHasAny(text, "stock", "existencia", "existencias"):
		candidates = []string{"ponti_stock_summary", "ponti_supplies_summary"}
	case pontiHasAny(text, "labors", "labor", "labores", "workorders", "work orders", "orden", "ordenes", "ot"):
		candidates = []string{"ponti_workorders_metrics", "ponti_workorders_list"}
	case pontiHasAny(text, "lots", "lote", "lotes"):
		candidates = []string{"ponti_lots_summary"}
	case pontiHasAny(text, "supplies", "insumo", "insumos"):
		candidates = []string{"ponti_supplies_summary"}
	case pontiHasAny(text, "insight", "insights"):
		candidates = []string{"ponti_insights_summary", "ponti_insights_list", "ponti_dashboard_summary"}
	case pontiHasAny(text, "dashboard", "tablero", "resumen operativo", "general", "campaigns", "campanas"):
		candidates = []string{"ponti_dashboard_summary", "ponti_insights_summary"}
	}
	if len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := available[candidate]; ok {
			out = append(out, candidate)
		}
	}
	return out
}

func pontiAvailableToolSet(schemas []ToolSchema) map[string]struct{} {
	out := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		name := strings.TrimSpace(schema.Name)
		if strings.HasPrefix(name, "ponti_") {
			out[name] = struct{}{}
		}
	}
	return out
}

func pontiToolCanRunWithoutWorkspace(toolName string) bool {
	switch toolName {
	case "ponti_insights_summary", "ponti_insights_list", "ponti_insights_explain":
		return true
	default:
		return false
	}
}

func pontiHandoffRouteHint(raw json.RawMessage) string {
	var root map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return ""
	}
	for _, key := range []string{"route_hint", "routeHint", "module", "context", "screen"} {
		if value, ok := root[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func pontiNormalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"á", "a",
		"é", "e",
		"í", "i",
		"ó", "o",
		"ú", "u",
		"ü", "u",
		"ñ", "n",
	)
	return replacer.Replace(value)
}

func pontiHasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, pontiNormalize(needle)) {
			return true
		}
	}
	return false
}
