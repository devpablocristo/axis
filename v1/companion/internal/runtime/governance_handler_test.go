package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devpablocristo/companion/internal/identityctx"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
)

type handlerRuntimeControls struct {
	policy  TenantRuntimePolicy
	missing bool
	audits  []RuntimePolicyAuditEntry
}

func (f *handlerRuntimeControls) GetRuntimePolicy(_ context.Context, orgID string) (TenantRuntimePolicy, error) {
	if f.missing {
		return TenantRuntimePolicy{}, ErrRuntimePolicyNotFound
	}
	if f.policy.OrgID == "" {
		return defaultRuntimePolicy(orgID), nil
	}
	return f.policy, nil
}

func (f *handlerRuntimeControls) UpsertRuntimePolicy(_ context.Context, policy TenantRuntimePolicy) (TenantRuntimePolicy, error) {
	policy = normalizeRuntimePolicy(policy)
	if policy.SettingsVersion == 0 {
		policy.SettingsVersion = 1
	} else {
		policy.SettingsVersion++
	}
	policy.UpdatedAt = time.Now().UTC()
	f.policy = policy
	f.missing = false
	changedBy, reason := runtimePolicyAuditMetadata(policy)
	f.audits = append(f.audits, RuntimePolicyAuditEntry{
		OrgID:           policy.OrgID,
		SettingsVersion: policy.SettingsVersion,
		ChangedBy:       changedBy,
		Reason:          reason,
		Policy:          policy,
		CreatedAt:       time.Now().UTC(),
	})
	return policy, nil
}

func (f *handlerRuntimeControls) ListRuntimePolicyAudit(context.Context, string, int) ([]RuntimePolicyAuditEntry, error) {
	return f.audits, nil
}

func (f *handlerRuntimeControls) GetRuntimeUsage(_ context.Context, orgID, period string) (TenantRuntimeUsage, error) {
	return TenantRuntimeUsage{OrgID: orgID, Period: period}, nil
}

func (f *handlerRuntimeControls) AddRuntimeUsage(context.Context, string, string, RunUsage) error {
	return nil
}

func TestRuntimeControlsHandlerGetMCPPolicyDefaultsWhenMissing(t *testing.T) {
	t.Parallel()

	repo := &handlerRuntimeControls{missing: true}
	mux := http.NewServeMux()
	NewRuntimeControlsHandler(repo).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/runtime/mcp-policy", nil)
	req = withRuntimePrincipal(req, []string{scopeCompanionRuntimeAdmin})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var out mcpRuntimePolicyView
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.OrgID != "org-a" || !out.Enabled || out.KillSwitch {
		t.Fatalf("unexpected default mcp policy view: %+v", out)
	}
}

func TestRuntimeControlsHandlerPutMCPPolicySavesDeniedToolsAndAuditMetadata(t *testing.T) {
	t.Parallel()

	repo := &handlerRuntimeControls{missing: true}
	mux := http.NewServeMux()
	NewRuntimeControlsHandler(repo).Register(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/runtime/mcp-policy", strings.NewReader(`{
		"denied_tools":["axis.products.list"],
		"metadata":{"changed_by":"ops-admin","change_reason":"maintenance"}
	}`))
	req = withRuntimePrincipal(req, []string{scopeCompanionRuntimeAdmin})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	if len(repo.policy.ControlPlane.DeniedTools) != 1 || repo.policy.ControlPlane.DeniedTools[0] != "axis.products.list" {
		t.Fatalf("expected denied tool saved, got %+v", repo.policy.ControlPlane.DeniedTools)
	}
	if repo.policy.Metadata["changed_by"] != "ops-admin" || repo.policy.Metadata["change_reason"] != "maintenance" {
		t.Fatalf("expected audit metadata saved, got %+v", repo.policy.Metadata)
	}
	if len(repo.audits) != 1 || repo.audits[0].ChangedBy != "ops-admin" || repo.audits[0].Reason != "maintenance" {
		t.Fatalf("expected audit entry metadata, got %+v", repo.audits)
	}
}

func TestRuntimeControlsHandlerPutMCPPolicyPreservesNonMCPFields(t *testing.T) {
	t.Parallel()

	repo := &handlerRuntimeControls{policy: TenantRuntimePolicy{
		OrgID:                  "org-a",
		Enabled:                true,
		MaxAutonomy:            AutonomyA1,
		AllowedModels:          []string{"gemini-1"},
		MonthlyTokenBudget:     1000,
		MonthlyToolCallBudget:  100,
			AllowedProductSurfaces: []string{"companion"},
			ControlPlane: OrgControlPlaneSettings{
				Memory: OrgMemoryPolicy{
					RetentionDays:     30,
					RequireProvenance: true,
			},
			ProductPolicies: map[string]ProductRuntimePolicy{
				"ponti": {MonthlyCostBudgetCents: 123, MaxAutonomy: AutonomyA1},
			},
		},
		Metadata: map[string]any{"existing": "kept"},
	}}
	mux := http.NewServeMux()
	NewRuntimeControlsHandler(repo).Register(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/runtime/mcp-policy", strings.NewReader(`{"allowed_tools":["axis.products.*"]}`))
	req = withRuntimePrincipal(req, []string{scopeCompanionRuntimeAdmin})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	policy := repo.policy
	if policy.MaxAutonomy != AutonomyA1 || len(policy.AllowedModels) != 1 || policy.AllowedModels[0] != "gemini-1" {
		t.Fatalf("expected model/autonomy preserved, got %+v", policy)
	}
	if policy.MonthlyTokenBudget != 1000 || policy.MonthlyToolCallBudget != 100 {
		t.Fatalf("expected budgets preserved, got %+v", policy)
	}
		if policy.ControlPlane.Memory.RetentionDays != 30 || !policy.ControlPlane.Memory.RequireProvenance {
		t.Fatalf("expected memory policy preserved, got %+v", policy.ControlPlane.Memory)
	}
	if policy.Metadata["existing"] != "kept" {
		t.Fatalf("expected metadata preserved, got %+v", policy.Metadata)
	}
}

func TestRuntimeControlsHandlerPutMCPProductDeniedPreservesProductPolicyBudgets(t *testing.T) {
	t.Parallel()

	repo := &handlerRuntimeControls{policy: TenantRuntimePolicy{
		OrgID:   "org-a",
		Enabled: true,
		ControlPlane: OrgControlPlaneSettings{
			ProductPolicies: map[string]ProductRuntimePolicy{
				"ponti": {MonthlyCostBudgetCents: 5000, MonthlyToolCallBudget: 42, MaxAutonomy: AutonomyA1},
			},
		},
	}}
	mux := http.NewServeMux()
	NewRuntimeControlsHandler(repo).Register(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/runtime/mcp-policy", strings.NewReader(`{"product_policies":{"ponti":{"denied":true}}}`))
	req = withRuntimePrincipal(req, []string{scopeCompanionRuntimeAdmin})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	productPolicy := repo.policy.ControlPlane.ProductPolicies["ponti"]
	if !productPolicy.Denied || productPolicy.MonthlyCostBudgetCents != 5000 || productPolicy.MonthlyToolCallBudget != 42 || productPolicy.MaxAutonomy != AutonomyA1 {
		t.Fatalf("expected denied plus preserved product settings, got %+v", productPolicy)
	}
}

func TestRuntimeControlsHandlerPutMCPPolicyRejectsInvalidToolPattern(t *testing.T) {
	t.Parallel()

	repo := &handlerRuntimeControls{missing: true}
	mux := http.NewServeMux()
	NewRuntimeControlsHandler(repo).Register(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/runtime/mcp-policy", strings.NewReader(`{"allowed_tools":["axis.*.bad"]}`))
	req = withRuntimePrincipal(req, []string{scopeCompanionRuntimeAdmin})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRuntimeControlsHandlerMCPPolicyRequiresRuntimeAdminScope(t *testing.T) {
	t.Parallel()

	repo := &handlerRuntimeControls{missing: true}
	mux := http.NewServeMux()
	NewRuntimeControlsHandler(repo).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/runtime/mcp-policy", nil)
	req = withRuntimePrincipal(req, []string{"companion:tasks:read"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", res.Code, res.Body.String())
	}
}

func withRuntimePrincipal(req *http.Request, scopes []string) *http.Request {
	principal := &authn.Principal{OrgID: "org-a", Actor: "user-a", Scopes: scopes, AuthMethod: "internal_jwt"}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}
