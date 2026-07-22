package capabilities

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestCapabilityManifestConformAndActivateRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	repo := &fakeCapabilityRepo{rows: map[uuid.UUID]domain.Capability{id: {
		ID: id, TenantID: "tenant-1", CapabilityKey: "diagnosis.reports.create", Name: "Create diagnosis",
		RequiredAutonomy: virployeedomain.AutonomyA3, RiskClass: "high", SideEffectClass: "write",
		RequiresNexusApproval: true, EvidenceRequired: true, PromotionState: domain.PromotionDraft,
	}}}
	ucs, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	ucs.SetQuotaPolicyChecker(&fakeQuotaPolicyChecker{allowed: true})
	router := gin.New()
	NewHandler(ucs).Routes(router.Group("/v1"))

	manifest := map[string]any{
		"version": "1.0.0", "product_surface": "medmory",
		"input_schema": map[string]any{"type": "object"}, "output_schema": map[string]any{"type": "object"},
		"required_scopes": []string{"diagnosis:write"},
		"idempotency":     map[string]any{"mode": "required", "key_fields": []string{"subject_id"}},
		"rollback_mode":   "manual", "timeout_ms": 30000,
		"retry":          map[string]any{"max_attempts": 3, "backoff_ms": 1000},
		"postconditions": []string{"evidence linked"}, "quota_areas": []string{"inbound", "executors"},
		"secret_refs":          []string{"secretmanager://projects/project/secrets/executor/versions/latest"},
		"attestation_required": true, "cost_class": "medium",
	}

	updated := capabilityRequest(t, router, http.MethodPut, "/v1/capabilities/"+id.String()+"/manifest", manifest, http.StatusOK)
	if updated.PromotionState != domain.PromotionDraft || updated.ManifestHash == "" {
		t.Fatalf("unexpected manifest response: %+v", updated)
	}
	conformed := capabilityRequest(t, router, http.MethodPost, "/v1/capabilities/"+id.String()+"/conform", map[string]any{}, http.StatusOK)
	if conformed.PromotionState != domain.PromotionConformant || !conformed.ConformanceReport.Conformant {
		t.Fatalf("unexpected conformance response: %+v", conformed)
	}
	active := capabilityRequest(t, router, http.MethodPost, "/v1/capabilities/"+id.String()+"/activate", map[string]any{}, http.StatusOK)
	if active.PromotionState != domain.PromotionActive || active.ActivatedAt == nil {
		t.Fatalf("unexpected activation response: %+v", active)
	}
}

func capabilityRequest(t *testing.T, router http.Handler, method, path string, body any, wantStatus int) domain.Capability {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status=%d body=%s", method, path, rec.Code, rec.Body.String())
	}
	var response struct {
		PromotionState    domain.PromotionState    `json:"promotion_state"`
		ManifestHash      string                   `json:"manifest_hash"`
		ConformanceReport domain.ConformanceReport `json:"conformance_report"`
		ActivatedAt       *time.Time               `json:"activated_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return domain.Capability{
		PromotionState: response.PromotionState, ManifestHash: response.ManifestHash,
		ConformanceReport: response.ConformanceReport, ActivatedAt: response.ActivatedAt,
	}
}
