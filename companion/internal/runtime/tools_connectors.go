package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/nexusclient"

	connectorsdomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// ConnectorTypeView expone un connector type registrado (Kind + capability
// templates declaradas en código). Es solo lectura.
type ConnectorTypeView interface {
	ID() string
	Kind() string
	Capabilities() []connectorsdomain.Capability
}

// ConnectorExecutor ejecuta una spec contra el connector resuelto en DB.
// Mismo contrato que `connectors.Usecases.Execute / ListConnectors`.
type ConnectorExecutor interface {
	Execute(ctx context.Context, spec connectorsdomain.ExecutionSpec) (connectorsdomain.ExecutionResult, error)
	ListConnectors(ctx context.Context) ([]connectorsdomain.Connector, error)
	BuildActionBinding(ctx context.Context, spec connectorsdomain.ExecutionSpec) (map[string]any, string, error)
}

// NexusSubmitter envía un request a Nexus. Mismo contrato que
// `tasks.NexusGateway.SubmitRequest`.
type NexusSubmitter interface {
	SubmitRequest(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error)
}

type RuntimePolicyReader interface {
	GetRuntimePolicy(ctx context.Context, orgID string) (TenantRuntimePolicy, error)
}

// CapabilityBridgeDeps agrupa lo que la bridge necesita para exponer
// connector capabilities como runtime tools. Connectors es la lista de
// connector types disponibles al boot (típicamente armada desde
// connectors/registry.Registry.List() via un loop trivial).
type CapabilityBridgeDeps struct {
	Connectors       []ConnectorTypeView
	Executor         ConnectorExecutor
	Submitter        NexusSubmitter
	Controls         RuntimePolicyReader
	ManifestRegistry *capabilities.Registry
}

// RegisterConnectorCapabilities itera cada connector type registrado y expone
// SUS capabilities como runtime tools para el LLM.
//
//   - Reads (NeedsNexusApproval == false): se ejecutan directo contra ConnectorExecutor.
//   - Writes controlled: se proponen primero a Nexus; si Nexus las marca como
//     allowed/approved se ejecutan inmediatamente; si quedan pending o son
//     denied se devuelve el estado al LLM para que informe al usuario.
//
// Naming: la capability "pymes.customers.search" se expone como tool
// "pymes_customers_search" para mantener nombres simples y portables.
// El campo `org_id` se inyecta automáticamente desde identity y se esconde del
// schema visible al LLM.
func RegisterConnectorCapabilities(tk *ToolKit, deps CapabilityBridgeDeps) {
	if tk == nil || deps.Executor == nil {
		return
	}
	for _, conn := range deps.Connectors {
		kind := conn.Kind()
		for _, capability := range conn.Capabilities() {
			capability := capability // capture per iteration
			manifest, err := capabilities.FromConnectorCapability(conn.ID(), kind, capability)
			if err != nil {
				slog.Error("skip invalid capability manifest", "connector", conn.ID(), "operation", capability.Operation, "error", err)
				continue
			}
			if deps.ManifestRegistry != nil {
				active, ok := deps.ManifestRegistry.Lookup(manifest.CapabilityID, manifest.Version)
				if !ok {
					slog.Warn("skip capability without active manifest", "connector", conn.ID(), "operation", capability.Operation, "capability_id", manifest.CapabilityID, "version", manifest.Version)
					continue
				}
				manifest = active
			}
			capability = manifest.ToConnectorCapability().Normalized(conn.ID(), kind)
			name := operationToToolName(capability.Operation)
			if name == "" {
				continue
			}
			schema := ToolSchema{
				Name:        name,
				Description: describeCapability(kind, capability),
				Parameters:  llmToolParameters(capability.InputSchema),
			}
			policy := toolPolicy{
				RequiresTenant:   true,
				RequiredAnyScope: capability.RequiredScopes,
			}
			tk.add(schema, policy, capabilityToolHandler(kind, capability, deps))
			tk.setMetadata(name, toolMetadataFromCapability(kind, capability))
		}
	}
}

func toolMetadataFromCapability(kind string, capability connectorsdomain.Capability) ToolMetadata {
	return ToolMetadata{
		Operation:             capability.Operation,
		CapabilityID:          capability.ID,
		CapabilityVersion:     capability.Version,
		Product:               capability.Product,
		ConnectorKind:         kind,
		ActionType:            capability.ActionType,
		SideEffectClass:       capability.SideEffectClass,
		RiskClass:             capability.RiskClass,
		NexusActionType:       capability.NexusActionType,
		RequiresNexusApproval: capability.NeedsNexusApproval(),
		EvidenceRequired:      append([]string(nil), capability.EvidenceRequired...),
		RollbackSupported:     capability.Rollback.Supported,
		RollbackCapabilityID:  capability.Rollback.CapabilityID,
		CompensationStrategy:  capability.CompensationStrategy,
		CostClass:             capability.CostClass,
		RateLimitClass:        capability.RateLimitClass,
		Timeout:               capability.Timeout,
		Preconditions:         append([]string(nil), capability.Preconditions...),
		Postconditions:        append([]string(nil), capability.Postconditions...),
		ObservabilityTags:     append([]string(nil), capability.ObservabilityTags...),
	}
}

func operationToToolName(operation string) string {
	op := strings.TrimSpace(operation)
	if op == "" {
		return ""
	}
	// Reemplazamos dots por underscore para preservar el namespacing por connector.
	return strings.ReplaceAll(op, ".", "_")
}

func describeCapability(kind string, c connectorsdomain.Capability) string {
	switch {
	case c.ReadOnly:
		return fmt.Sprintf("Read-only operation %q on the %q connector.", c.Operation, kind)
	case c.RequiresNexusApproval:
		return fmt.Sprintf("Write operation %q on the %q connector (requires nexus approval before execution).", c.Operation, kind)
	default:
		return fmt.Sprintf("Operation %q on the %q connector.", c.Operation, kind)
	}
}

// llmToolParameters limpia el InputSchema de la capability para exponerlo al
// LLM. El bridge inyecta org_id desde identity, por lo que se quita del schema
// visible (si el LLM lo manda igual, lo sobrescribimos).
func llmToolParameters(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	out := make(map[string]any, len(schema))
	for k, v := range schema {
		out[k] = v
	}
	if req, ok := out["required"]; ok {
		out["required"] = filterOutOrgID(req)
	}
	if props, ok := out["properties"].(map[string]any); ok {
		cleaned := make(map[string]any, len(props))
		for k, v := range props {
			if k == "org_id" {
				continue
			}
			cleaned[k] = v
		}
		out["properties"] = cleaned
	}
	return out
}

func filterOutOrgID(value any) any {
	switch items := value.(type) {
	case []string:
		out := make([]string, 0, len(items))
		for _, it := range items {
			if it != "org_id" {
				out = append(out, it)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(items))
		for _, it := range items {
			if s, ok := it.(string); ok && s == "org_id" {
				continue
			}
			out = append(out, it)
		}
		return out
	}
	return value
}

// capabilityToolHandler arma el handler que el LLM va a invocar.
func capabilityToolHandler(kind string, capability connectorsdomain.Capability, deps CapabilityBridgeDeps) ToolHandler {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		id := IdentityFromContext(ctx)
		if strings.TrimSpace(id.OrgID) == "" {
			return `{"error":"customer org context required"}`, nil
		}
		toolName := operationToToolName(capability.Operation)
		if event := validateCapabilityControlPlane(ctx, deps.Controls, id.OrgID, toolName, kind, capability); event != nil {
			slog.Warn("capability_blocked_by_control_plane", "operation", capability.Operation, "kind", kind, "reason", event.Reason)
			return jsonOrError(map[string]any{
				"error":  "capability blocked by customer org control plane",
				"target": event.Target,
				"reason": event.Reason,
			}), nil
		}

		connID, err := resolveConnectorID(ctx, deps.Executor, id.OrgID, kind)
		if err != nil {
			return jsonOrError(map[string]any{
				"error":   "connector not configured for this customer org",
				"kind":    kind,
				"details": err.Error(),
			}), nil
		}

		payload, err := mergeOrgIDIntoArgs(args, id.OrgID)
		if err != nil {
			return `{"error":"invalid tool arguments"}`, nil
		}
		if event := ValidateEgressPayload(payload); event != nil {
			slog.Warn("capability_blocked_by_egress_policy", "operation", capability.Operation, "kind", kind, "reason", event.Reason)
			return jsonOrError(map[string]any{
				"error":  "capability blocked by egress policy",
				"target": event.Target,
				"reason": event.Reason,
			}), nil
		}

		spec := connectorsdomain.ExecutionSpec{
			ConnectorID:        connID,
			OrgID:              id.OrgID,
			ActorID:            firstNonEmpty(id.UserID, id.OnBehalfOf, id.CompanionPrincipal, CompanionPrincipal),
			ActorType:          firstNonEmpty(id.ActorType, "agent"),
			CompanionPrincipal: firstNonEmpty(id.CompanionPrincipal, CompanionPrincipal),
			OnBehalfOf:         id.OnBehalfOf,
			ServicePrincipal:   id.ServicePrincipal,
			ProductSurface:     productSurfaceFromIdentity(id),
			AuthScopes:         append([]string(nil), id.AuthScopes...),
			Operation:          capability.Operation,
			Payload:            payload,
			IdempotencyKey:     firstNonEmpty(id.IdempotencyKey, fmt.Sprintf("chat-%s-%s", capability.Operation, uuid.NewString())),
		}
		if taskID, err := uuid.Parse(strings.TrimSpace(id.TaskID)); err == nil && taskID != uuid.Nil {
			spec.TaskID = &taskID
		}
		if invocationID := strings.TrimSpace(id.PlanStepID); invocationID != "" {
			spec.ToolInvocationID = invocationID
		}

		if !capability.NeedsNexusApproval() {
			res, err := deps.Executor.Execute(ctx, spec)
			if err != nil {
				slog.Error("capability execute failed", "operation", capability.Operation, "kind", kind, "error", err)
				return `{"error":"execution failed"}`, nil
			}
			if isPlanStepInvocation(id) {
				return connectorPlanStepResult(res), nil
			}
			if len(res.ResultJSON) == 0 {
				return `{"result": null}`, nil
			}
			return string(res.ResultJSON), nil
		}

		// Controlled write: propose primero, luego ejecutar si Nexus aprueba.
		if deps.Submitter == nil {
			return `{"error":"nexus not configured"}`, nil
		}
		binding, bindingHash, err := deps.Executor.BuildActionBinding(ctx, spec)
		if err != nil {
			slog.Error("capability action binding failed", "operation", capability.Operation, "error", err)
			return `{"error":"action binding failed"}`, nil
		}
		submitBody := nexusclient.SubmitRequestBody{
			RequesterType:  "agent",
			RequesterID:    firstNonEmpty(id.CompanionPrincipal, CompanionPrincipal),
			RequesterName:  "Companion Employee AI",
			ActionType:     firstNonEmpty(capability.NexusActionType, nexusclient.ActionTypeAgentCapabilityInvoke),
			TargetSystem:   kind,
			TargetResource: connID.String(),
			ActionBinding:  binding,
			Params: map[string]any{
				"org_id":              spec.OrgID,
				"operation":           capability.Operation,
				"payload":             json.RawMessage(payload),
				"action_binding":      binding,
				"action_binding_hash": bindingHash,
				"actor_id":            spec.ActorID,
				"actor_type":          spec.ActorType,
				"companion_principal": spec.CompanionPrincipal,
				"on_behalf_of":        spec.OnBehalfOf,
			},
			Reason: fmt.Sprintf("LLM-driven invocation of %s", capability.Operation),
		}
		submitOut, err := deps.Submitter.SubmitRequest(ctx, spec.IdempotencyKey, submitBody)
		if err != nil {
			slog.Error("nexus submit failed", "operation", capability.Operation, "error", err)
			return `{"error":"nexus submit failed"}`, nil
		}
		status := strings.ToLower(strings.TrimSpace(submitOut.Status))
		switch status {
		case "allowed", "approved", "executed":
			reqID, perr := uuid.Parse(submitOut.RequestID)
			if perr != nil {
				return `{"error":"invalid request_id from nexus"}`, nil
			}
			spec.NexusRequestID = &reqID
			res, err := deps.Executor.Execute(ctx, spec)
			if err != nil {
				slog.Error("controlled capability execute failed", "operation", capability.Operation, "error", err)
				return jsonOrError(map[string]any{
					"status":       "execution_failed",
					"request_id":   submitOut.RequestID,
					"nexus_status": status,
				}), nil
			}
			if isPlanStepInvocation(id) {
				return connectorPlanStepResult(res, "request_id", submitOut.RequestID, "nexus_status", status), nil
			}
			return jsonOrError(map[string]any{
				"status":       "executed",
				"request_id":   submitOut.RequestID,
				"nexus_status": status,
				"result":       json.RawMessage(res.ResultJSON),
			}), nil
		case "pending_approval", "pending":
			return jsonOrError(map[string]any{
				"status":     "pending_approval",
				"request_id": submitOut.RequestID,
				"message":    "Acción enviada a aprobación. El usuario debe aprobarla antes de ejecutarse.",
			}), nil
		case "denied", "rejected":
			return jsonOrError(map[string]any{
				"status":     "denied",
				"request_id": submitOut.RequestID,
			}), nil
		default:
			return jsonOrError(map[string]any{
				"status":     status,
				"request_id": submitOut.RequestID,
			}), nil
		}
	}
}

func validateCapabilityControlPlane(ctx context.Context, reader RuntimePolicyReader, orgID, toolName, connectorKind string, capability connectorsdomain.Capability) *GuardrailEvent {
	if reader == nil {
		return nil
	}
	policy, err := reader.GetRuntimePolicy(ctx, orgID)
	if err != nil {
		if errors.Is(err, ErrRuntimePolicyNotFound) {
			return &GuardrailEvent{Type: "org_control_plane", Target: "policy", Reason: "customer org runtime policy is required before executing capabilities"}
		}
		return &GuardrailEvent{Type: "org_control_plane", Target: "policy", Reason: "customer org runtime policy lookup failed"}
	}
	policy = normalizeRuntimePolicy(policy)
	if !policy.Enabled || policy.KillSwitch {
		return &GuardrailEvent{Type: "org_control_plane", Target: "runtime", Reason: "runtime is disabled for this customer org"}
	}
	settings := policy.ControlPlane
	if settings.ConnectorKillSwitches[connectorKind] {
		return &GuardrailEvent{Type: "org_control_plane", Target: "connector:" + connectorKind, Reason: "connector kill switch is active"}
	}
	if stringListAllows(settings.DeniedConnectors, connectorKind) {
		return &GuardrailEvent{Type: "org_control_plane", Target: "connector:" + connectorKind, Reason: "connector is denied for this customer org"}
	}
	if len(settings.AllowedConnectors) > 0 && !stringListAllows(settings.AllowedConnectors, connectorKind) {
		return &GuardrailEvent{Type: "org_control_plane", Target: "connector:" + connectorKind, Reason: "connector is not allowed for this customer org"}
	}
	if settings.ToolKillSwitches[toolName] {
		return &GuardrailEvent{Type: "org_control_plane", Target: "tool:" + toolName, Reason: "tool kill switch is active"}
	}
	if stringListAllows(settings.DeniedTools, toolName) {
		return &GuardrailEvent{Type: "org_control_plane", Target: "tool:" + toolName, Reason: "tool is denied for this customer org"}
	}
	if len(settings.AllowedTools) > 0 && !stringListAllows(settings.AllowedTools, toolName) {
		return &GuardrailEvent{Type: "org_control_plane", Target: "tool:" + toolName, Reason: "tool is not allowed for this customer org"}
	}
	if controlPlaneMatchesAny(settings.DeniedCapabilities, capability.ID, capability.Operation, toolName) {
		return &GuardrailEvent{Type: "org_control_plane", Target: "capability:" + capability.ID, Reason: "capability is denied for this customer org"}
	}
	if len(settings.AllowedCapabilities) > 0 && !controlPlaneMatchesAny(settings.AllowedCapabilities, capability.ID, capability.Operation, toolName) {
		return &GuardrailEvent{Type: "org_control_plane", Target: "capability:" + capability.ID, Reason: "capability is not allowed for this customer org"}
	}
	if riskRankControlPlane(capability.RiskClass) > riskRankControlPlane(settings.MaxRiskClass) {
		return &GuardrailEvent{Type: "org_control_plane", Target: "risk:" + capability.RiskClass, Reason: "capability risk exceeds customer org limit"}
	}
	if approvalThresholdRequiresNexus(settings.ApprovalThresholds, capability.RiskClass) && !capability.NeedsNexusApproval() {
		return &GuardrailEvent{Type: "org_control_plane", Target: "approval_threshold", Reason: "capability risk requires Nexus approval but capability is not configured for controlled execution"}
	}
	return nil
}

func controlPlaneMatchesAny(patterns []string, values ...string) bool {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if stringListAllows(patterns, value) {
			return true
		}
	}
	return false
}

func riskRankControlPlane(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none":
		return 0
	case "low":
		return 1
	case "medium":
		return 2
	case "", "high":
		return 3
	case "critical":
		return 4
	default:
		return 4
	}
}

func approvalThresholdRequiresNexus(thresholds map[string]string, risk string) bool {
	rank := riskRankControlPlane(risk)
	for thresholdRisk, mode := range thresholds {
		mode = strings.ToLower(strings.TrimSpace(mode))
		if mode != "require_approval" && mode != "nexus_required" {
			continue
		}
		if rank >= riskRankControlPlane(thresholdRisk) {
			return true
		}
	}
	return false
}

func isPlanStepInvocation(id Identity) bool {
	return strings.TrimSpace(id.PlanStepID) != ""
}

func connectorPlanStepResult(res connectorsdomain.ExecutionResult, extra ...any) string {
	status := res.Status
	if status == "" {
		status = connectorsdomain.ExecSuccess
	}
	payload := map[string]any{
		"status":          status,
		"connector_id":    res.ConnectorID.String(),
		"operation":       res.Operation,
		"external_ref":    res.ExternalRef,
		"result":          json.RawMessage(jsonOrDefaultRaw(res.ResultJSON)),
		"evidence":        json.RawMessage(jsonOrDefaultRaw(res.EvidenceJSON)),
		"duration_ms":     res.DurationMS,
		"idempotency_key": res.IdempotencyKey,
	}
	if res.ID != uuid.Nil {
		payload["execution_id"] = res.ID.String()
	}
	if res.TaskID != nil {
		payload["task_id"] = res.TaskID.String()
	}
	if res.NexusRequestID != nil {
		payload["nexus_request_id"] = res.NexusRequestID.String()
	}
	if res.ErrorMessage != "" {
		payload["error"] = res.ErrorMessage
	}
	for i := 0; i+1 < len(extra); i += 2 {
		key, ok := extra[i].(string)
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		payload[key] = extra[i+1]
	}
	return jsonOrError(payload)
}

func jsonOrDefaultRaw(raw json.RawMessage) string {
	if strings.TrimSpace(string(raw)) == "" {
		return `{}`
	}
	return string(raw)
}

func resolveConnectorID(ctx context.Context, exec ConnectorExecutor, orgID, kind string) (uuid.UUID, error) {
	list, err := exec.ListConnectors(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	for _, c := range list {
		if !c.Enabled {
			continue
		}
		if strings.EqualFold(c.Kind, kind) && strings.EqualFold(c.OrgID, orgID) {
			return c.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("no enabled %s connector for org %s", kind, orgID)
}

func mergeOrgIDIntoArgs(args json.RawMessage, orgID string) (json.RawMessage, error) {
	var m map[string]any
	if len(args) == 0 {
		m = map[string]any{}
	} else if err := json.Unmarshal(args, &m); err != nil {
		return nil, err
	}
	m["org_id"] = orgID
	return json.Marshal(m)
}

func jsonOrError(payload map[string]any) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return `{"error":"marshal_failed"}`
	}
	return string(b)
}

// productSurfaceFromIdentity (en tools.go) y firstNonEmpty (en repository.go)
// se reutilizan desde este archivo.
