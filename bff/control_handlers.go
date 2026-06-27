package main

import (
	"encoding/json"
	"net/http"
	"strings"

	authn "github.com/devpablocristo/platform/authn/go"
)

// controlAPI is the Control Plane surface: global resources (organizations,
// products, tenants, platform roles) managed by platform admins. It is
// orthogonal to the active tenant — gated by platform roles stored in Axis,
// NOT by X-Tenant-ID nor Clerk metadata. Mounted at /api/control/.
func (s *server) controlAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	platformRoles, _ := s.iam.PlatformRolesForUser(r.Context(), p.Actor)
	if !isPlatformAdmin(platformRoles) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "platform admin required")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/control"), "/")
	switch {
	case path == "organizations" && r.Method == http.MethodGet:
		s.controlListOrgs(w, r)
	case path == "organizations" && r.Method == http.MethodPost:
		s.controlCreateOrg(w, r, p)
	case path == "tenants" && r.Method == http.MethodGet:
		s.controlListTenants(w, r)
	case path == "tenants" && r.Method == http.MethodPost:
		s.controlProvisionTenant(w, r)
	case path == "products" && r.Method == http.MethodGet:
		s.controlListProducts(w, r)
	case path == "platform-roles" && r.Method == http.MethodPost:
		s.controlGrantPlatformRole(w, r)
	case strings.HasPrefix(path, "tenants/") && strings.HasSuffix(path, "/members") && r.Method == http.MethodPost:
		tenantID := strings.TrimSuffix(strings.TrimPrefix(path, "tenants/"), "/members")
		s.controlAddTenantMember(w, r, tenantID)
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "control plane route not found")
	}
}

func (s *server) controlListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.iam.ListOrgsForActor(r.Context(), "", true) // cross=true -> all companies
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": orgs})
}

func (s *server) controlCreateOrg(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION", "name is required")
		return
	}
	org, err := s.iam.CreateOrg(r.Context(), IAMOrg{Name: strings.TrimSpace(body.Name), Slug: slugify(firstNonEmpty(body.Slug, body.Name)), Status: "active"}, p.Actor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (s *server) controlListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := s.iam.ListTenants(r.Context(), strings.TrimSpace(r.URL.Query().Get("org_id")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tenants})
}

// controlProvisionTenant creates a tenant = (org x product). Optionally adds an
// owner member. This is the provisioning primitive of the Control Plane.
func (s *server) controlProvisionTenant(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrgID          string `json:"org_id"`
		ProductSurface string `json:"product_surface"`
		Name           string `json:"name"`
		OwnerUserID    string `json:"owner_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if strings.TrimSpace(body.OrgID) == "" || strings.TrimSpace(body.ProductSurface) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION", "org_id and product_surface are required")
		return
	}
	tenant, err := s.iam.CreateTenant(r.Context(), IAMTenant{
		OrgID:          strings.TrimSpace(body.OrgID),
		ProductSurface: strings.TrimSpace(body.ProductSurface),
		Name:           strings.TrimSpace(body.Name),
		Status:         "active",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	if owner := strings.TrimSpace(body.OwnerUserID); owner != "" {
		if _, err := s.iam.UpsertTenantMember(r.Context(), IAMTenantMember{TenantID: tenant.ID, UserID: owner, Role: "owner", Status: "active"}); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusCreated, tenant)
}

// controlListProducts returns the product catalog (the product_surface registry).
// Static for now; the source of truth for the catalog can move to a table later.
func (s *server) controlListProducts(w http.ResponseWriter, _ *http.Request) {
	type product struct {
		ProductSurface string `json:"product_surface"`
		Name           string `json:"name"`
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": []product{
		{ProductSurface: "axis", Name: "Axis (admin)"},
		{ProductSurface: "medmory", Name: "Medmory"},
		{ProductSurface: "ponti", Name: "Ponti"},
		{ProductSurface: "pymes", Name: "Pymes"},
		{ProductSurface: "companion", Name: "Companion"},
	}})
}

func (s *server) controlGrantPlatformRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.UserID) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION", "user_id is required")
		return
	}
	role := firstNonEmpty(strings.TrimSpace(body.Role), "platform_admin")
	if err := s.iam.SetPlatformRole(r.Context(), strings.TrimSpace(body.UserID), role); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user_id": body.UserID, "role": role})
}

func (s *server) controlAddTenantMember(w http.ResponseWriter, r *http.Request, tenantID string) {
	var body struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.UserID) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION", "user_id is required")
		return
	}
	member, err := s.iam.UpsertTenantMember(r.Context(), IAMTenantMember{
		TenantID: strings.TrimSpace(tenantID),
		UserID:   strings.TrimSpace(body.UserID),
		Role:     firstNonEmpty(strings.TrimSpace(body.Role), "member"),
		Status:   "active",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, member)
}
