package main

import (
	"context"
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
		if err == nil {
			s.auditIAM(r, p, org.ID, "tenant.created", "tenant", org.ID, map[string]any{"name": org.Name, "status": org.Status})
		}
		writeStoreCreated(w, map[string]any{"item": tenantView(org)}, err)
		return
	}
	if len(parts) >= 1 {
		tenantID := parts[0]
		if len(parts) == 1 && (r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin") {
				return
			}
			input, ok := decodeJSONBody[IAMTenantView](w, r)
			if !ok {
				return
			}
			org, err := s.updateIAMOrg(r.Context(), tenantID, IAMOrg{Name: input.Name, Status: input.Status})
			if err == nil {
				s.auditIAM(r, p, tenantID, "tenant.updated", "tenant", tenantID, map[string]any{"name": input.Name, "status": input.Status})
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
		err := s.deleteIAMOrg(r.Context(), tenantID)
		if err == nil {
			s.auditIAM(r, p, tenantID, "tenant.purged", "tenant", tenantID, nil)
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
	status := statusForIAMAction(action)
	if status == "" {
		http.NotFound(w, r)
		return
	}
	org, err := s.updateIAMOrg(r.Context(), tenantID, IAMOrg{Status: status})
	if err == nil {
		s.auditIAM(r, p, tenantID, "tenant."+action, "tenant", tenantID, map[string]any{"status": status})
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
		if orgID != "" && orgID != "axis" && !s.canAccessOrg(r, p, orgID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
			return
		}
		items, err := s.listIAMUserViews(r.Context(), p, listStatus(parts), orgID)
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
	tenantID, userID, global := parseUserRef(ref)
	if userID == "" {
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
		if !global && !s.canAccessOrg(r, p, tenantID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected tenant is not allowed for this principal")
			return
		}
		var err error
		if global || s.identity != nil {
			err = s.deleteIAMUser(r.Context(), tenantID, userID)
		} else {
			err = s.deleteIAMMember(r.Context(), tenantID, userID)
		}
		if err == nil {
			s.auditIAM(r, p, tenantID, "user.purged", "user", userID, nil)
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
	var view IAMUserView
	var err error
	if global {
		var user IAMUser
		user, err = s.updateIAMUser(r.Context(), "", userID, IAMUser{Status: status})
		view = globalUserView(user)
	} else {
		if !s.canAccessOrg(r, p, tenantID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "selected tenant is not allowed for this principal")
			return
		}
		var member IAMMember
		member, err = s.updateIAMMember(r.Context(), tenantID, userID, IAMMember{Status: status})
		view, _ = s.memberUserView(r.Context(), member)
	}
	if err == nil {
		s.auditIAM(r, p, tenantID, "user."+action, "user", userID, map[string]any{"status": status})
	}
	writeStoreResult(w, map[string]any{"item": view}, err)
}

func (s *server) createIAMUserView(r *http.Request, p authn.Principal, input IAMUserView) (IAMUserView, error) {
	role := normalizedRole(input.Role)
	if role == "" {
		role = "member"
	}
	orgID := iamUserInputOrgID(input)
	if orgID == "" || orgID == "axis" {
		if !hasScope(p.Scopes, "axis:cross_org", "axis:users:admin") {
			return IAMUserView{}, errNotFound
		}
		user, err := s.createIAMUser(r.Context(), "", IAMUser{Email: input.Email, Name: input.Email, Role: role, AxisRole: role, Status: "active"})
		if err != nil {
			return IAMUserView{}, err
		}
		return globalUserView(user), nil
	}
	if !s.canAccessOrg(r, p, orgID) {
		return IAMUserView{}, errNotFound
	}
	user, err := s.createIAMUser(r.Context(), orgID, IAMUser{Email: input.Email, Name: input.Email, Role: role, Status: "active"})
	if err != nil {
		return IAMUserView{}, err
	}
	return IAMUserView{ID: tenantUserRowID(orgID, user.ID), UserID: user.ID, Email: user.Email, Role: role, OrgID: orgID, TenantID: orgID, Scope: "tenant", Status: "active", CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}, nil
}

func (s *server) updateIAMUserView(r *http.Request, p authn.Principal, ref string, input IAMUserView) (IAMUserView, error) {
	tenantID, userID, global := parseUserRef(ref)
	role := normalizedRole(input.Role)
	if global {
		user, err := s.updateIAMUser(r.Context(), "", userID, IAMUser{Email: input.Email, Name: input.Email, Role: role, AxisRole: role, Status: input.Status})
		if err != nil {
			return IAMUserView{}, err
		}
		return globalUserView(user), nil
	}
	if !s.canAccessOrg(r, p, tenantID) {
		return IAMUserView{}, errNotFound
	}
	user, err := s.updateIAMUser(r.Context(), tenantID, userID, IAMUser{Email: input.Email, Name: input.Email, Role: role, Status: input.Status})
	if err != nil {
		return IAMUserView{}, err
	}
	return IAMUserView{ID: tenantUserRowID(tenantID, user.ID), UserID: user.ID, Email: user.Email, Role: firstNonEmpty(role, input.Role), OrgID: tenantID, TenantID: tenantID, Scope: "tenant", Status: firstNonEmpty(input.Status, user.Status), CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}, nil
}

func (s *server) listIAMUserViews(ctx context.Context, p authn.Principal, status string, orgFilter string) ([]IAMUserView, error) {
	users, err := s.iam.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	items := []IAMUserView{}
	if (orgFilter == "" || orgFilter == "axis") && hasScope(p.Scopes, "axis:cross_org") {
		for _, user := range users {
			if normalizedRole(user.AxisRole) == "" || lifecycleBucket(user.Status) != status {
				continue
			}
			items = append(items, globalUserView(user))
		}
	}
	if orgFilter == "axis" {
		return items, nil
	}
	if orgFilter != "" {
		if orgFilter != "axis" {
			if err := s.syncIAMOrgMembers(ctx, orgFilter); err != nil {
				return nil, err
			}
		}
		members, err := s.iam.ListMembers(ctx, orgFilter)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if lifecycleBucket(member.Status) != status || member.User == nil {
				continue
			}
			view, ok := s.memberUserView(ctx, member)
			if ok {
				items = append(items, view)
			}
		}
		return items, nil
	}
	orgs, err := s.iam.ListOrgsForActor(ctx, p.Actor, hasScope(p.Scopes, "axis:cross_org", "axis:users:admin"))
	if err != nil {
		return nil, err
	}
	for _, org := range orgs {
		members, err := s.iam.ListMembers(ctx, org.ID)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if lifecycleBucket(member.Status) != status || member.User == nil {
				continue
			}
			if normalizedRole(member.User.AxisRole) != "" {
				continue
			}
			view, ok := s.memberUserView(ctx, member)
			if ok {
				items = append(items, view)
			}
		}
	}
	return items, nil
}

func (s *server) syncIAMOrgMembers(ctx context.Context, orgID string) error {
	if s.identity == nil {
		return nil
	}
	return s.identity.SyncOrgMembers(ctx, orgID)
}

func (s *server) memberUserView(_ context.Context, member IAMMember) (IAMUserView, bool) {
	if member.User == nil {
		return IAMUserView{}, false
	}
	return IAMUserView{
		ID:        tenantUserRowID(member.OrgID, member.User.ID),
		UserID:    member.User.ID,
		Email:     member.User.Email,
		Role:      member.Role,
		OrgID:     member.OrgID,
		TenantID:  member.OrgID,
		Scope:     "tenant",
		Status:    member.Status,
		CreatedAt: member.CreatedAt,
		UpdatedAt: member.UpdatedAt,
	}, true
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

func iamUserInputOrgID(input IAMUserView) string {
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" {
		orgID = strings.TrimSpace(input.TenantID)
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
