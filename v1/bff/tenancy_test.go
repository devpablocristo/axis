package main

import (
	"context"
	"regexp"
	"testing"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestCreateTenantGeneratesUUID(t *testing.T) {
	store := newMemoryIAMStore()
	if _, err := store.CreateOrg(context.Background(), IAMOrg{ID: "org-a", Name: "Org A", Status: "active"}, ""); err != nil {
		t.Fatal(err)
	}
	tenant, err := store.CreateTenant(context.Background(), IAMTenant{OrgID: "org-a", ProductSurface: "axis", Name: "Org A / axis"})
	if err != nil {
		t.Fatal(err)
	}
	if !uuidV4Pattern.MatchString(tenant.ID) {
		t.Fatalf("expected generated tenant id to be UUID v4, got %q", tenant.ID)
	}
}

func TestCreateTenantIgnoresExplicitID(t *testing.T) {
	store := newMemoryIAMStore()
	if _, err := store.CreateOrg(context.Background(), IAMOrg{ID: "org-a", Name: "Org A", Status: "active"}, ""); err != nil {
		t.Fatal(err)
	}
	tenant, err := store.CreateTenant(context.Background(), IAMTenant{ID: "explicit-tenant", OrgID: "org-a", ProductSurface: "axis", Name: "Org A / axis"})
	if err != nil {
		t.Fatal(err)
	}
	if !uuidV4Pattern.MatchString(tenant.ID) || tenant.ID == "explicit-tenant" {
		t.Fatalf("expected explicit tenant id to be replaced by generated UUID, got %q", tenant.ID)
	}
}
