package runtime

import (
	"context"
	"encoding/json"
	"testing"

	connectorsdomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/google/uuid"
)

func TestEffectiveWorkspace_topLevelWinsOverHandoff(t *testing.T) {
	t.Parallel()

	in := RunInput{
		Workspace: json.RawMessage(`{"customer_id":99,"project_id":7}`),
		Handoff:   json.RawMessage(`{"source":"ponti-web","workspace":{"customer_id":17,"project_id":30}}`),
	}
	workspace := effectiveWorkspace(in)
	if workspace["customer_id"] != float64(99) || workspace["project_id"] != float64(7) {
		t.Fatalf("expected top-level workspace to win, got %+v", workspace)
	}
}

func TestEffectiveWorkspace_fallsBackToHandoff(t *testing.T) {
	t.Parallel()

	in := RunInput{
		Handoff: json.RawMessage(`{"source":"ponti-web","workspace":{"customer_id":17,"campaign_id":2}}`),
	}
	workspace := effectiveWorkspace(in)
	if workspace["customer_id"] != float64(17) || workspace["campaign_id"] != float64(2) {
		t.Fatalf("expected handoff workspace fallback, got %+v", workspace)
	}
}

func TestEffectiveWorkspace_sanitizesEmptyValues(t *testing.T) {
	t.Parallel()

	in := RunInput{
		Workspace: json.RawMessage(`{"customer_id":0,"project_id":null,"field_id":"  ","campaign_id":2}`),
	}
	workspace := effectiveWorkspace(in)
	if len(workspace) != 1 || workspace["campaign_id"] != float64(2) {
		t.Fatalf("expected only campaign_id to survive, got %+v", workspace)
	}

	if got := effectiveWorkspace(RunInput{}); got != nil {
		t.Fatalf("expected nil workspace without inputs, got %+v", got)
	}
}

func TestMergeWorkspaceIntoArgs(t *testing.T) {
	t.Parallel()

	workspace := map[string]any{"project_id": float64(30)}

	merged, err := mergeWorkspaceIntoArgs(json.RawMessage(`{"limit":25}`), workspace)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(merged, &decoded); err != nil {
		t.Fatal(err)
	}
	ws, ok := decoded["workspace"].(map[string]any)
	if !ok || ws["project_id"] != float64(30) {
		t.Fatalf("expected workspace merged into args, got %s", string(merged))
	}

	// Args con workspace propio: el LLM gana, no se pisa.
	original := json.RawMessage(`{"workspace":{"project_id":1}}`)
	merged, err = mergeWorkspaceIntoArgs(original, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if string(merged) != string(original) {
		t.Fatalf("expected args untouched when workspace already present, got %s", string(merged))
	}

	// Sin workspace efectivo: no-op.
	merged, err = mergeWorkspaceIntoArgs(original, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(merged) != string(original) {
		t.Fatalf("expected args untouched without run workspace, got %s", string(merged))
	}
}

func TestOrchestrator_PontiPrefetchUsesTopLevelWorkspace(t *testing.T) {
	t.Parallel()

	var capturedArgs json.RawMessage
	toolkit := &ToolKit{
		Schemas: []ToolSchema{
			{Name: "ponti_reports_summary_results_summary", Description: "Report summary", Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"workspace": map[string]any{"type": "object"}},
				"required":   []string{"workspace"},
			}},
		},
		Handlers: map[string]ToolHandler{
			"ponti_reports_summary_results_summary": func(_ context.Context, args json.RawMessage) (string, error) {
				capturedArgs = append(json.RawMessage(nil), args...)
				return `{"source":"ponti.reports.summary_results.summary","summary":{"result":"ok"},"items":[]}`, nil
			},
		},
	}
	provider := &fakeLLMProvider{responses: []ChatResponse{
		{Text: "No tengo acceso a esos informes."},
		{Text: "Listo."},
	}}
	orch := NewOrchestrator(provider, toolkit, ContextPorts{})

	_, err := orch.Run(context.Background(), RunInput{
		UserID:         "user-1",
		OrgID:          "org-1",
		Message:        "Resumí los informes económicos de la campaña.",
		RouteHint:      "reports",
		ProductSurface: "ponti",
		Workspace:      json.RawMessage(`{"customer_id":99,"project_id":7}`),
		Handoff:        json.RawMessage(`{"source":"ponti-web","route_hint":"reports","workspace":{"customer_id":17,"project_id":30}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal(capturedArgs, &args); err != nil {
		t.Fatal(err)
	}
	workspace, _ := args["workspace"].(map[string]any)
	if workspace["customer_id"] != float64(99) || workspace["project_id"] != float64(7) {
		t.Fatalf("expected top-level workspace in forced tool args, got %s", string(capturedArgs))
	}
}

// fakeWorkspaceExecutor captura la spec ejecutada por capabilityToolHandler.
type fakeWorkspaceExecutor struct {
	connectorID uuid.UUID
	orgID       string
	kind        string
	lastSpec    connectorsdomain.ExecutionSpec
}

func (f *fakeWorkspaceExecutor) Execute(_ context.Context, spec connectorsdomain.ExecutionSpec) (connectorsdomain.ExecutionResult, error) {
	f.lastSpec = spec
	return connectorsdomain.ExecutionResult{ResultJSON: json.RawMessage(`{"ok":true}`)}, nil
}

func (f *fakeWorkspaceExecutor) ListConnectors(_ context.Context) ([]connectorsdomain.Connector, error) {
	return []connectorsdomain.Connector{{ID: f.connectorID, OrgID: f.orgID, Kind: f.kind, Enabled: true}}, nil
}

func (f *fakeWorkspaceExecutor) BuildActionBinding(_ context.Context, _ connectorsdomain.ExecutionSpec) (map[string]any, string, error) {
	return map[string]any{}, "", nil
}

func TestCapabilityToolHandler_mergesWorkspaceOnlyForProductCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		publishedFrom string
		wantWorkspace bool
	}{
		{name: "capability product-published recibe workspace", publishedFrom: connectorsdomain.CapabilityPublishedFromProduct, wantWorkspace: true},
		{name: "capability no product-published no recibe workspace", publishedFrom: "platform", wantWorkspace: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			executor := &fakeWorkspaceExecutor{connectorID: uuid.New(), orgID: "org-1", kind: "ponti"}
			capability := connectorsdomain.Capability{
				Operation:     "ponti.workorders.list",
				PublishedFrom: tc.publishedFrom,
				ReadOnly:      true,
				Mode:          connectorsdomain.CapabilityModeRead,
			}
			handler := capabilityToolHandler("ponti", capability, CapabilityBridgeDeps{Executor: executor})

			ctx := WithIdentity(context.Background(), "user-1", "org-1")
			ctx = WithWorkspace(ctx, map[string]any{"project_id": float64(30)})
			if _, err := handler(ctx, json.RawMessage(`{"limit":25}`)); err != nil {
				t.Fatal(err)
			}

			var payload map[string]any
			if err := json.Unmarshal(executor.lastSpec.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			_, hasWorkspace := payload["workspace"]
			if hasWorkspace != tc.wantWorkspace {
				t.Fatalf("workspace presence = %v, want %v (payload %s)", hasWorkspace, tc.wantWorkspace, string(executor.lastSpec.Payload))
			}
		})
	}
}

func TestCapabilityToolHandler_keepsLLMWorkspaceInArgs(t *testing.T) {
	t.Parallel()

	executor := &fakeWorkspaceExecutor{connectorID: uuid.New(), orgID: "org-1", kind: "ponti"}
	capability := connectorsdomain.Capability{
		Operation:     "ponti.workorders.list",
		PublishedFrom: connectorsdomain.CapabilityPublishedFromProduct,
		ReadOnly:      true,
		Mode:          connectorsdomain.CapabilityModeRead,
	}
	handler := capabilityToolHandler("ponti", capability, CapabilityBridgeDeps{Executor: executor})

	ctx := WithIdentity(context.Background(), "user-1", "org-1")
	ctx = WithWorkspace(ctx, map[string]any{"project_id": float64(30)})
	if _, err := handler(ctx, json.RawMessage(`{"workspace":{"project_id":1}}`)); err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(executor.lastSpec.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	workspace, _ := payload["workspace"].(map[string]any)
	if workspace["project_id"] != float64(1) {
		t.Fatalf("expected LLM workspace preserved, got %+v", workspace)
	}
}
