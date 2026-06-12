package actiontypes

import (
	"net/http"
	"strings"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusActionTypesAdmin = "nexus:policies:admin"
	scopeNexusCrossOrg         = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func effectiveActionTypeOrg(r *http.Request, requested *string) (*string, bool) {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return normalizeOrgPtr(requested), true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	if orgID == "" {
		return nil, false
	}
	if requested != nil && strings.TrimSpace(*requested) != "" && strings.TrimSpace(*requested) != orgID {
		return nil, false
	}
	return &orgID, true
}

func canAccessActionTypeOrg(r *http.Request, orgID *string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	principalOrgID := identityhttp.PrincipalOrgID(r)
	if principalOrgID == "" {
		return orgID == nil || strings.TrimSpace(*orgID) == ""
	}
	if orgID == nil || strings.TrimSpace(*orgID) == "" {
		return true
	}
	return strings.TrimSpace(*orgID) == principalOrgID
}

func canWriteActionTypeOrg(r *http.Request, orgID *string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	principalOrgID := identityhttp.PrincipalOrgID(r)
	return principalOrgID != "" && orgID != nil && strings.TrimSpace(*orgID) == principalOrgID
}

func listActionTypesOrg(r *http.Request) (*string, bool) {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return nil, true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	if orgID == "" {
		return nil, false
	}
	return &orgID, true
}

func normalizeOrgPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
