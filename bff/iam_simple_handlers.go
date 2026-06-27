package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
)

type IAMTenantView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IAMProductView struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ProductSurface string    `json:"product_surface"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type IAMUserView struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id,omitempty"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	OrgID     string    `json:"org_id,omitempty"`
	TenantID  string    `json:"tenant_id,omitempty"`
	Scope     string    `json:"scope"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *server) iamAPI(w http.ResponseWriter, r *http.Request) {
	parts := iamRouteParts(r.URL.Path)
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	switch parts[0] {
	case "tenants":
		s.iamTenants(w, r, parts[1:])
	case "products":
		s.iamProducts(w, r, parts[1:])
	case "users":
		s.iamUsers(w, r, parts[1:])
	default:
		http.NotFound(w, r)
	}
}

func (s *server) iamTenants(w http.ResponseWriter, r *http.Request, parts []string) {
	p := principalFromContext(r.Context())
	if r.Method == http.MethodGet && isListRequest(parts) {
		if !requireScope(w, p, "axis:orgs:read", "axis:orgs:admin", "axis:cross_org") {
			return
		}
		status := listStatus(parts)
		orgs, err := s.iam.ListOrgsForActor(r.Context(), p.Actor, hasScope(p.Scopes, "axis:cross_org", "axis:orgs:admin"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		items := make([]IAMTenantView, 0, len(orgs))
		for _, org := range orgs {
			if lifecycleBucket(org.Status) != status {
				continue
			}
			items = append(items, tenantView(org))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if len(parts) == 0 && r.Method == http.MethodPost {
		if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin") {
			return
		}
		_ = s.ensureActorUser(r, p)
		input, ok := decodeJSONBody[IAMTenantView](w, r)
		if !ok {
			return
		}
		org, err := s.createIAMOrg(r.Context(), p.Actor, IAMOrg{Name: input.Name, Status: firstNonEmpty(input.Status, "active")})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		// Mismo invariante que el control plane: todo org nace con su tenant 'axis'
		// + el creador como owner, así ningún org queda sin tenant.
		if terr := s.ensureOrgDefaultTenant(r.Context(), org, p.Actor); terr != nil {
			writeStoreError(w, terr)
			return
		}
		s.auditIAM(r, p, org.ID, "org.created", "org", org.ID, map[string]any{"name": org.Name, "status": org.Status})
		writeStoreCreated(w, map[string]any{"item": tenantView(org)}, nil)
		return
	}
	if len(parts) >= 1 {
		tenantID := parts[0]
		if len(parts) == 1 && (r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin") {
				return
			}
			if !s.canAccessOrg(r, p, tenantID) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
				return
			}
			input, ok := decodeJSONBody[IAMTenantView](w, r)
			if !ok {
				return
			}
			org, err := s.updateIAMOrg(r.Context(), tenantID, IAMOrg{Name: input.Name, Status: input.Status})
			if err == nil {
				s.auditIAM(r, p, tenantID, "org.updated", "org", tenantID, map[string]any{"name": input.Name, "status": input.Status})
			}
			writeStoreResult(w, map[string]any{"item": tenantView(org)}, err)
			return
		}
		if len(parts) == 2 {
			s.iamTenantLifecycle(w, r, p, tenantID, parts[1])
			return
		}
	}
	http.NotFound(w, r)
}

func (s *server) iamTenantLifecycle(w http.ResponseWriter, r *http.Request, p authn.Principal, tenantID string, action string) {
	if action == "purge" {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireScope(w, p, "axis:iam:purge") {
			return
		}
		if !s.canAccessOrg(r, p, tenantID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
			return
		}
		err := s.deleteIAMOrg(r.Context(), tenantID)
		if err == nil {
			s.auditIAM(r, p, tenantID, "org.purged", "org", tenantID, nil)
		}
		writeStoreNoContent(w, err)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin") {
		return
	}
	if !s.canAccessOrg(r, p, tenantID) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
		return
	}
	status := statusForIAMAction(action)
	if status == "" {
		http.NotFound(w, r)
		return
	}
	org, err := s.updateIAMOrg(r.Context(), tenantID, IAMOrg{Status: status})
	if err == nil {
		s.auditIAM(r, p, tenantID, "org."+action, "org", tenantID, map[string]any{"status": status})
	}
	writeStoreResult(w, map[string]any{"item": tenantView(org)}, err)
}

func (s *server) iamProducts(w http.ResponseWriter, r *http.Request, parts []string) {
	p := principalFromContext(r.Context())
	if r.Method == http.MethodGet && isListRequest(parts) {
		if !requireScope(w, p, "axis:products:read", "axis:products:admin", "axis:cross_org") {
			return
		}
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		if tenantID == "__none__" {
			writeJSON(w, http.StatusOK, map[string]any{"items": []IAMProductView{}})
			return
		}
		if tenantID != "" && !s.canAccessOrg(r, p, tenantID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected tenant is not allowed for this principal")
			return
		}
		if tenantID == "" && !hasScope(p.Scopes, "axis:cross_org", "axis:products:admin") {
			var err error
			tenantID, err = s.selectedOrg(r, p)
			if err != nil {
				writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
				return
			}
		}
		products, err := s.iam.ListProducts(r.Context(), tenantID, listStatus(parts))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		items := make([]IAMProductView, 0, len(products))
		for _, product := range products {
			items = append(items, productView(product))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if len(parts) == 0 && r.Method == http.MethodPost {
		if !requireScope(w, p, "axis:products:write", "axis:products:admin") {
			return
		}
		input, ok := decodeJSONBody[IAMProductView](w, r)
		if !ok {
			return
		}
		if strings.TrimSpace(input.TenantID) == "" || !s.canAccessOrg(r, p, input.TenantID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected tenant is not allowed for this principal")
			return
		}
		product, err := s.iam.CreateProduct(r.Context(), IAMProduct{TenantID: input.TenantID, ProductSurface: input.ProductSurface, Name: input.Name, Status: firstNonEmpty(input.Status, "active")})
		if err == nil {
			s.auditIAM(r, p, product.TenantID, "product.created", "product", product.ID, map[string]any{"name": product.Name, "product_surface": product.ProductSurface})
		}
		writeStoreCreated(w, map[string]any{"item": productView(product)}, err)
		return
	}
	if len(parts) >= 1 {
		productID := parts[0]
		if len(parts) == 1 && (r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			if !requireScope(w, p, "axis:products:write", "axis:products:admin") {
				return
			}
			product, ok := s.productForAccess(w, r, p, productID)
			if !ok {
				return
			}
			input, ok := decodeJSONBody[IAMProductView](w, r)
			if !ok {
				return
			}
			updated, err := s.iam.UpdateProduct(r.Context(), productID, IAMProduct{Name: input.Name, ProductSurface: input.ProductSurface, Status: input.Status})
			if err == nil {
				s.auditIAM(r, p, product.TenantID, "product.updated", "product", productID, map[string]any{"name": input.Name, "status": input.Status})
			}
			writeStoreResult(w, map[string]any{"item": productView(updated)}, err)
			return
		}
		if len(parts) == 2 {
			s.iamProductLifecycle(w, r, p, productID, parts[1])
			return
		}
	}
	http.NotFound(w, r)
}

func (s *server) iamProductLifecycle(w http.ResponseWriter, r *http.Request, p authn.Principal, productID string, action string) {
	product, ok := s.productForAccess(w, r, p, productID)
	if !ok {
		return
	}
	if action == "purge" {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireScope(w, p, "axis:iam:purge") {
			return
		}
		err := s.iam.DeleteProduct(r.Context(), productID)
		if err == nil {
			s.auditIAM(r, p, product.TenantID, "product.purged", "product", productID, nil)
		}
		writeStoreNoContent(w, err)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireScope(w, p, "axis:products:write", "axis:products:admin") {
		return
	}
	status := statusForIAMAction(action)
	if status == "" {
		http.NotFound(w, r)
		return
	}
	updated, err := s.iam.UpdateProduct(r.Context(), productID, IAMProduct{Status: status})
	if err == nil {
		s.auditIAM(r, p, product.TenantID, "product."+action, "product", productID, map[string]any{"status": status})
	}
	writeStoreResult(w, map[string]any{"item": productView(updated)}, err)
}

func (s *server) iamUsers(w http.ResponseWriter, r *http.Request, parts []string) {
	p := principalFromContext(r.Context())
	if r.Method == http.MethodGet && isListRequest(parts) {
		if !requireScope(w, p, "axis:users:read", "axis:users:admin") {
			return
		}
		orgID := iamUsersOrgFilter(r)
		if orgID == "axis" && !hasScope(p.Scopes, "axis:cross_org") {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
			return
		}
		if orgID == "axis" {
			items, err := s.listGlobalIAMUserViews(r.Context(), p, listStatus(parts))
			if err != nil {
				writeStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": items})
			return
		}
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		if tenantID == "" {
			writeStoreError(w, fmt.Errorf("%w: tenant_id is required", errValidation))
			return
		}
		tenant, terr := s.iam.TenantByID(r.Context(), tenantID)
		if terr != nil {
			writeStoreError(w, terr)
			return
		}
		if orgID == "" {
			orgID = tenant.OrgID
		}
		if tenant.OrgID != orgID {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected tenant is not allowed for this org")
			return
		}
		if !s.canAccessOrg(r, p, tenant.OrgID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
			return
		}
		items, err := s.listTenantUserViews(r.Context(), tenant, listStatus(parts))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if len(parts) == 0 && r.Method == http.MethodPost {
		if !requireScope(w, p, "axis:users:write", "axis:users:admin") {
			return
		}
		input, ok := decodeJSONBody[IAMUserView](w, r)
		if !ok {
			return
		}
		view, err := s.createIAMUserView(r, p, input)
		if err == nil {
			s.auditIAM(r, p, view.OrgID, "user.created", "user", view.UserID, map[string]any{"email": view.Email, "role": view.Role, "scope": view.Scope})
		}
		writeStoreCreated(w, map[string]any{"item": view}, err)
		return
	}
	if len(parts) >= 1 {
		ref := parts[0]
		if len(parts) == 1 && (r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			if !requireScope(w, p, "axis:users:write", "axis:users:admin") {
				return
			}
			input, ok := decodeJSONBody[IAMUserView](w, r)
			if !ok {
				return
			}
			view, err := s.updateIAMUserView(r, p, ref, input)
			if err == nil {
				s.auditIAM(r, p, view.TenantID, "user.updated", "user", view.UserID, map[string]any{"email": view.Email, "role": view.Role, "status": view.Status})
			}
			writeStoreResult(w, map[string]any{"item": view}, err)
			return
		}
		if len(parts) == 2 {
			s.iamUserLifecycle(w, r, p, ref, parts[1])
			return
		}
	}
	http.NotFound(w, r)
}

func (s *server) iamUserLifecycle(w http.ResponseWriter, r *http.Request, p authn.Principal, ref string, action string) {
	ref0, rerr := s.resolveUserRef(r.Context(), ref)
	if rerr != nil {
		writeStoreError(w, rerr)
		return
	}
	if ref0.userID == "" {
		writeStoreError(w, errNotFound)
		return
	}
	if action == "purge" {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireScope(w, p, "axis:iam:purge") {
			return
		}
		if ref0.kind != userRefGlobal && !s.canAccessOrg(r, p, ref0.orgID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected tenant is not allowed for this principal")
			return
		}
		// Purge = delete the user from the IdP (Clerk DELETE /users/{id}). Clerk is
		// the source of truth for identities, so a hard delete removes it there;
		// the org/tenant memberships are cascaded away by the FK in axis-control.
		err := s.deleteIAMUser(r.Context(), "", ref0.userID)
		if err == nil {
			s.auditIAM(r, p, ref0.orgID, "user.purged", "user", ref0.userID, nil)
		}
		writeStoreNoContent(w, err)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireScope(w, p, "axis:users:write", "axis:users:admin") {
		return
	}
	status := statusForIAMAction(action)
	if status == "" {
		http.NotFound(w, r)
		return
	}
	if ref0.kind != userRefGlobal && !s.canAccessOrg(r, p, ref0.orgID) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "selected tenant is not allowed for this principal")
		return
	}
	var view IAMUserView
	var err error
	switch ref0.kind {
	case userRefTenant:
		// Lifecycle (archive/trash/restore) of a tenant user = the status of its
		// per-product access. Preserve the existing role.
		current, mErr := s.iam.TenantMembership(r.Context(), ref0.tenant.ID, ref0.userID)
		if mErr != nil {
			writeStoreError(w, mErr)
			return
		}
		if _, err = s.iam.UpsertTenantMember(r.Context(), IAMTenantMember{TenantID: ref0.tenant.ID, UserID: ref0.userID, Role: current.Role, Status: status}); err == nil {
			view, _ = s.tenantUserView(r.Context(), ref0.tenant, ref0.userID)
		}
	case userRefGlobal:
		var user IAMUser
		user, err = s.updateIAMUser(r.Context(), "", ref0.userID, IAMUser{Status: status})
		view = globalUserView(user)
	}
	if err == nil {
		s.auditIAM(r, p, ref0.orgID, "user."+action, "user", ref0.userID, map[string]any{"status": status})
	}
	writeStoreResult(w, map[string]any{"item": view}, err)
}

func (s *server) createIAMUserView(r *http.Request, p authn.Principal, input IAMUserView) (IAMUserView, error) {
	role := normalizedRole(input.Role)
	if role == "" {
		role = "member"
	}
	// Un user SIEMPRE se crea DENTRO de un tenant (org × product). El tenant
	// objetivo viene del header X-Tenant-ID o del body (tenant_id / org_id): un
	// tenant id real se usa directo; un org id resuelve al tenant 'axis' (workspace
	// de admin) por defecto de ese org (todo org tiene uno — ver
	// ensureOrgDefaultTenant). El caso global/plataforma (org "axis"/vacío) se
	// maneja abajo. El path legacy org-member (axis_org_members como authz) se retiró.
	inputOrgID := strings.TrimSpace(input.OrgID)
	ref := firstNonEmpty(strings.TrimSpace(r.Header.Get("X-Tenant-ID")), strings.TrimSpace(input.TenantID), inputOrgID)
	if strings.EqualFold(inputOrgID, "axis") {
		ref = "axis"
	}
	if ref != "" && !strings.EqualFold(ref, "axis") {
		tenant, ok := s.resolveTargetTenant(r.Context(), ref)
		if !ok {
			return IAMUserView{}, fmt.Errorf("%w: no resuelve ningún tenant para %q (un user se crea dentro de un tenant)", errValidation, ref)
		}
		if !s.canAccessOrg(r, p, tenant.OrgID) {
			return IAMUserView{}, errNotFound
		}
		// Identidad en Clerk + membership en el ORG de Clerk (company). 'owner' es
		// rol global, no rol de org de Clerk → entra a la company como admin.
		companyRole := role
		if companyRole == "owner" {
			companyRole = "admin"
		}
		user, found, ferr := s.findIAMUserByEmail(r.Context(), input.Email)
		if ferr != nil {
			return IAMUserView{}, ferr
		}
		if found {
			if err := s.upsertIAMMember(r.Context(), tenant.OrgID, user.ID, companyRole); err != nil {
				return IAMUserView{}, err
			}
		} else {
			created, cerr := s.createIAMUser(r.Context(), tenant.OrgID, IAMUser{Email: input.Email, Name: input.Email, Role: companyRole, Status: "active"})
			if cerr != nil {
				return IAMUserView{}, cerr
			}
			user = created
		}
		// Acceso per-product (axis_tenant_members) + rol owner de plataforma,
		// aplicados atómicamente. En create nunca degradamos a un owner global.
		op := platformRoleKeep
		if role == "owner" {
			op = platformRoleGrantOwner
		}
		if err := s.iam.SetTenantMembership(r.Context(), tenant.ID, user.ID, role, "active", op); err != nil {
			return IAMUserView{}, err
		}
		return IAMUserView{ID: tenantUserRowID(tenant.ID, user.ID), UserID: user.ID, Email: user.Email, Role: role, OrgID: tenant.OrgID, TenantID: tenant.ID, Scope: "tenant", Status: "active", CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}, nil
	}
	// User global / de plataforma (org "axis" o sin especificar): sin membership de
	// tenant. Requiere scope cross-org / users:admin.
	if !hasScope(p.Scopes, "axis:cross_org", "axis:users:admin") {
		return IAMUserView{}, errNotFound
	}
	user, err := s.createIAMUser(r.Context(), "", IAMUser{Email: input.Email, Name: input.Email, Role: role, AxisRole: role, Status: "active"})
	if err != nil {
		return IAMUserView{}, err
	}
	return globalUserView(user), nil
}

// resolveTargetTenant mapea un ref (un tenant id, o un org id) al tenant donde el
// user debe entrar: un tenant id real se devuelve tal cual; un org id resuelve al
// tenant 'axis' (workspace por defecto) de ese org.
func (s *server) resolveTargetTenant(ctx context.Context, ref string) (IAMTenant, bool) {
	if t, err := s.iam.TenantByID(ctx, ref); err == nil {
		return t, true
	}
	return s.orgDefaultTenant(ctx, ref)
}

// orgDefaultTenant devuelve el tenant 'axis' (workspace por defecto) del org.
func (s *server) orgDefaultTenant(ctx context.Context, orgID string) (IAMTenant, bool) {
	tenants, err := s.iam.ListTenants(ctx, orgID)
	if err != nil {
		return IAMTenant{}, false
	}
	for _, t := range tenants {
		if t.ProductSurface == defaultOrgProductSurface {
			return t, true
		}
	}
	return IAMTenant{}, false
}

func (s *server) updateIAMUserView(r *http.Request, p authn.Principal, ref string, input IAMUserView) (IAMUserView, error) {
	ref0, rerr := s.resolveUserRef(r.Context(), ref)
	if rerr != nil {
		return IAMUserView{}, rerr
	}
	role := normalizedRole(input.Role)
	if ref0.kind == userRefGlobal {
		user, err := s.updateIAMUser(r.Context(), "", ref0.userID, IAMUser{Email: input.Email, Name: input.Email, Role: role, AxisRole: role, Status: input.Status})
		if err != nil {
			return IAMUserView{}, err
		}
		return globalUserView(user), nil
	}
	if !s.canAccessOrg(r, p, ref0.orgID) {
		return IAMUserView{}, errNotFound
	}
	if ref0.kind == userRefTenant {
		// Tenant user: role is the per-product role (axis_tenant_members), email
		// is identity-level (Clerk, no org membership touch). 'owner' is global.
		current, mErr := s.iam.TenantMembership(r.Context(), ref0.tenant.ID, ref0.userID)
		if mErr != nil {
			return IAMUserView{}, mErr
		}
		newRole := firstNonEmpty(role, current.Role)
		status := firstNonEmpty(input.Status, current.Status)
		if strings.TrimSpace(input.Email) != "" {
			if _, err := s.updateIAMUser(r.Context(), "", ref0.userID, IAMUser{Email: input.Email, Name: input.Email}); err != nil {
				return IAMUserView{}, err
			}
		}
		// Tenant role + global owner transition applied atomically: on edit a
		// non-owner role REVOKES the global owner platform role (demotion), an
		// owner role GRANTS it — and the tenant-member row commits in the same tx.
		op := platformRoleRevokeOwner
		if newRole == "owner" {
			op = platformRoleGrantOwner
		}
		if err := s.iam.SetTenantMembership(r.Context(), ref0.tenant.ID, ref0.userID, newRole, status, op); err != nil {
			return IAMUserView{}, err
		}
		if view, ok := s.tenantUserView(r.Context(), ref0.tenant, ref0.userID); ok {
			return view, nil
		}
		return IAMUserView{}, errNotFound
	}
	return IAMUserView{}, errNotFound
}

// findIAMUserByEmail looks up an existing identity by email (case-insensitive).
// Used by tenant-scoped creation so adding a user to a tenant reuses an existing
// identity instead of failing on the unique email constraint.
func (s *server) findIAMUserByEmail(ctx context.Context, email string) (IAMUser, bool, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return IAMUser{}, false, nil
	}
	users, err := s.iam.ListUsers(ctx)
	if err != nil {
		return IAMUser{}, false, err
	}
	for _, u := range users {
		if strings.ToLower(u.Email) == email {
			return u, true, nil
		}
	}
	return IAMUser{}, false, nil
}

// listTenantUserViews lists the members of a tenant (org x product) as user
// views. This is the tenancy-model listing: users belong to a tenant, resolved
// from axis_tenant_members (joined with axis_users for email/name).
func (s *server) listTenantUserViews(ctx context.Context, tenant IAMTenant, status string) ([]IAMUserView, error) {
	members, err := s.iam.ListTenantMembers(ctx, tenant.ID)
	if err != nil {
		return nil, err
	}
	items := []IAMUserView{}
	for _, m := range members {
		if m.User == nil || lifecycleBucket(m.Status) != status {
			continue
		}
		items = append(items, tenantMemberToView(tenant, m))
	}
	return items, nil
}

// tenantMemberToView renders a tenant membership (org x product access) as the
// user view the console consumes. The row id encodes the tenant + user so the
// lifecycle/update handlers can route it back to tenant-member operations.
func tenantMemberToView(tenant IAMTenant, m IAMTenantMember) IAMUserView {
	email := ""
	if m.User != nil {
		email = m.User.Email
	}
	return IAMUserView{
		ID:        tenantUserRowID(tenant.ID, m.UserID),
		UserID:    m.UserID,
		Email:     email,
		Role:      m.Role,
		OrgID:     tenant.OrgID,
		TenantID:  tenant.ID,
		Scope:     "tenant",
		Status:    m.Status,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

// tenantUserView fetches the current tenant membership of userID and renders it
// (with the user's email via the join). Used to build the response after a
// lifecycle/role mutation.
func (s *server) tenantUserView(ctx context.Context, tenant IAMTenant, userID string) (IAMUserView, bool) {
	members, err := s.iam.ListTenantMembers(ctx, tenant.ID)
	if err != nil {
		return IAMUserView{}, false
	}
	for _, m := range members {
		if m.UserID == userID {
			return tenantMemberToView(tenant, m), true
		}
	}
	return IAMUserView{}, false
}

type userRefKind int

const (
	userRefGlobal userRefKind = iota
	userRefTenant
)

type resolvedUserRef struct {
	kind   userRefKind
	orgID  string // org used for authz + identity ops (tenant.OrgID for tenant refs)
	tenant IAMTenant
	userID string
}

// resolveUserRef decodes a user row id and classifies it. A
// "tenant__<id>__<user>" ref must resolve via TenantByID and is tenant-scoped
// (per-product access, axis_tenant_members); its authz org is tenant.OrgID —
// NOT the tenant id.
func (s *server) resolveUserRef(ctx context.Context, ref string) (resolvedUserRef, error) {
	scope, userID, global := parseUserRef(ref)
	if global {
		return resolvedUserRef{kind: userRefGlobal, userID: userID}, nil
	}
	tenant, err := s.iam.TenantByID(ctx, scope)
	if err == nil {
		return resolvedUserRef{kind: userRefTenant, orgID: tenant.OrgID, tenant: tenant, userID: userID}, nil
	}
	return resolvedUserRef{}, err
}

func (s *server) listGlobalIAMUserViews(ctx context.Context, p authn.Principal, status string) ([]IAMUserView, error) {
	users, err := s.iam.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	items := []IAMUserView{}
	if !hasScope(p.Scopes, "axis:cross_org") {
		return items, nil
	}
	for _, user := range users {
		if normalizedRole(user.AxisRole) == "" || lifecycleBucket(user.Status) != status {
			continue
		}
		items = append(items, globalUserView(user))
	}
	return items, nil
}

func (s *server) productForAccess(w http.ResponseWriter, r *http.Request, p authn.Principal, productID string) (IAMProduct, bool) {
	products, err := s.iam.ListProducts(r.Context(), "", "")
	if err != nil {
		writeStoreError(w, err)
		return IAMProduct{}, false
	}
	for _, product := range products {
		if product.ID != productID {
			continue
		}
		if !s.canAccessOrg(r, p, product.TenantID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected tenant is not allowed for this principal")
			return IAMProduct{}, false
		}
		return product, true
	}
	writeStoreError(w, errNotFound)
	return IAMProduct{}, false
}

func tenantView(org IAMOrg) IAMTenantView {
	return IAMTenantView{ID: org.ID, Name: org.Name, Status: org.Status, CreatedAt: org.CreatedAt, UpdatedAt: org.UpdatedAt}
}

func productView(product IAMProduct) IAMProductView {
	return IAMProductView{ID: product.ID, TenantID: product.TenantID, ProductSurface: product.ProductSurface, Name: product.Name, Status: product.Status, CreatedAt: product.CreatedAt, UpdatedAt: product.UpdatedAt}
}

func globalUserView(user IAMUser) IAMUserView {
	return IAMUserView{ID: globalUserRowID(user.ID), UserID: user.ID, Email: user.Email, Role: user.AxisRole, OrgID: "axis", Scope: "axis", Status: user.Status, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}
}

func iamUsersOrgFilter(r *http.Request) string {
	orgID := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if orgID == "" {
		orgID = strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	}
	if strings.EqualFold(orgID, "axis") {
		return "axis"
	}
	return orgID
}

func iamRouteParts(path string) []string {
	path = strings.TrimPrefix(path, "/api/iam/")
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func isListRequest(parts []string) bool {
	return len(parts) == 0 || (len(parts) == 1 && (parts[0] == "archived" || parts[0] == "trash"))
}

func listStatus(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	return "active"
}

func statusForIAMAction(action string) string {
	switch action {
	case "archive":
		return "archived"
	case "trash":
		return "trash"
	case "restore", "unarchive":
		return "active"
	default:
		return ""
	}
}

func lifecycleBucket(status string) string {
	switch strings.TrimSpace(status) {
	case "archived":
		return "archived"
	case "trash":
		return "trash"
	default:
		return "active"
	}
}

func globalUserRowID(userID string) string {
	return "axis__" + userID
}

func tenantUserRowID(tenantID string, userID string) string {
	return "tenant__" + tenantID + "__" + userID
}

func parseUserRef(ref string) (tenantID string, userID string, global bool) {
	if strings.HasPrefix(ref, "axis__") {
		return "", strings.TrimPrefix(ref, "axis__"), true
	}
	if strings.HasPrefix(ref, "tenant__") {
		rest := strings.TrimPrefix(ref, "tenant__")
		parts := strings.SplitN(rest, "__", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], false
		}
	}
	return "", ref, true
}
