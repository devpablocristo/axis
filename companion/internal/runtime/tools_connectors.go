package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/nexusclient"

	connectorsdomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// ConnectorTypeView expone un connector type registrado (Kind + capability
// templates declaradas en código). Es solo lectura.
type ConnectorTypeView interface {
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

// CapabilityBridgeDeps agrupa lo que la bridge necesita para exponer
// connector capabilities como runtime tools. Connectors es la lista de
// connector types disponibles al boot (típicamente armada desde
// connectors/registry.Registry.List() via un loop trivial).
type CapabilityBridgeDeps struct {
	Connectors []ConnectorTypeView
	Executor   ConnectorExecutor
	Submitter  NexusSubmitter
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
			capability = capability.Normalized("", kind)
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
		Product:               capability.Product,
		ConnectorKind:         kind,
		SideEffectClass:       capability.SideEffectClass,
		RiskClass:             capability.RiskClass,
		RequiresNexusApproval: capability.NeedsNexusApproval(),
		EvidenceRequired:      append([]string(nil), capability.EvidenceRequired...),
		RollbackSupported:     capability.Rollback.Supported,
		RollbackCapabilityID:  capability.Rollback.CapabilityID,
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
			ActionType:     nexusclient.ActionTypeAgentCapabilityInvoke,
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
