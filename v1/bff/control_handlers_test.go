package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type atomicTenantProvisionStore struct {
	IAMStore
	calledCreateWithOwner bool
	calledCreateTenant    bool
	calledUpsertMember    bool
	gotOwner              string
	gotTenant             IAMTenant
}

func (s *atomicTenantProvisionStore) CreateTenantWithOwner(_ context.Context, tenant IAMTenant, ownerUserID string) (IAMTenant, error) {
	s.calledCreateWithOwner = true
	s.gotOwner = ownerUserID
	tenant.ID = "tenant_control"
	s.gotTenant = tenant
	return tenant, nil
}

func (s *atomicTenantProvisionStore) CreateTenant(ctx context.Context, tenant IAMTenant) (IAMTenant, error) {
	s.calledCreateTenant = true
	return s.IAMStore.CreateTenant(ctx, tenant)
}

func (s *atomicTenantProvisionStore) UpsertTenantMember(ctx context.Context, member IAMTenantMember) (IAMTenantMember, error) {
	s.calledUpsertMember = true
	return s.IAMStore.UpsertTenantMember(ctx, member)
}

func TestControlProvisionTenantUsesAtomicStorePrimitive(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	store := &atomicTenantProvisionStore{IAMStore: srv.iam}
	srv.iam = store
	req := httptest.NewRequest(http.MethodPost, "/api/control/tenants", strings.NewReader(`{"org_id":"co-a","product_surface":"medmory","name":"Medmory","owner_user_id":"user-owner"}`))
	rec := httptest.NewRecorder()

	srv.controlProvisionTenant(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("provision tenant: want 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	if !store.calledCreateWithOwner {
		t.Fatalf("controlProvisionTenant must use CreateTenantWithOwner")
	}
	if store.calledCreateTenant || store.calledUpsertMember {
		t.Fatalf("controlProvisionTenant must not split tenant/member writes: create=%v upsert=%v", store.calledCreateTenant, store.calledUpsertMember)
	}
	if store.gotOwner != "user-owner" {
		t.Fatalf("owner user mismatch: %q", store.gotOwner)
	}
	if store.gotTenant.OrgID != "co-a" || store.gotTenant.ProductSurface != "medmory" {
		t.Fatalf("tenant input mismatch: %#v", store.gotTenant)
	}
	var tenant IAMTenant
	if err := json.Unmarshal(rec.Body.Bytes(), &tenant); err != nil {
		t.Fatal(err)
	}
	if tenant.ID != "tenant_control" {
		t.Fatalf("response should be the created tenant, got %#v", tenant)
	}
}

func TestControlAPISurfacesPlatformRoleStoreError(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	srv.iam = platformRolesErrStore{IAMStore: srv.iam}
	req := httptest.NewRequest(http.MethodGet, "/api/control/organizations", nil)
	rec := httptest.NewRecorder()

	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "platform roles store down") {
		t.Fatalf("response leaked store error: %s", rec.Body.String())
	}
}
