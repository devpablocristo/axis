package actiontypes

import (
	"net/http"
	"strings"

	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusActionTypesAdmin = "nexus:policies:admin"
	scopeNexusCrossOrg         = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func effectiveActionTypeOrg(r *http.Request, requested *string) (*string, bool) {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopeNexusCrossOrg) {
		return normalizeOrgPtr(requested), true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID == "" {
		return nil, false
	}
	if requested != nil && strings.TrimSpace(*requested) != "" && strings.TrimSpace(*requested) != orgID {
		return nil, false
	}
	return &orgID, true
}

func canAccessActionTypeOrg(r *http.Request, orgID *string) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopeNexusCrossOrg) {
		return true
	}
	principalOrgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if principalOrgID == "" {
		return orgID == nil || strings.TrimSpace(*orgID) == ""
	}
	if orgID == nil || strings.TrimSpace(*orgID) == "" {
		return true
	}
	return strings.TrimSpace(*orgID) == principalOrgID
}

func canWriteActionTypeOrg(r *http.Request, orgID *string) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopeNexusCrossOrg) {
		return true
	}
	principalOrgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	return principalOrgID != "" && orgID != nil && strings.TrimSpace(*orgID) == principalOrgID
}

func listActionTypesOrg(r *http.Request) (*string, bool) {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopeNexusCrossOrg) {
		return nil, true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
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

func requestHasNoAuthContext(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("X-Auth-Method")) == "" &&
		strings.TrimSpace(r.Header.Get("X-Auth-Scopes")) == ""
}

func requestHasScope(r *http.Request, scopes ...string) bool {
	have := parseHeaderScopes(r.Header.Get("X-Auth-Scopes"))
	for _, scope := range scopes {
		if _, ok := have[scope]; ok {
			return true
		}
	}
	return false
}

func parseHeaderScopes(raw string) map[string]struct{} {
	raw = strings.NewReplacer(",", " ", ";", " ", "+", " ").Replace(raw)
	fields := strings.Fields(raw)
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if scope := strings.TrimSpace(field); scope != "" {
			out[scope] = struct{}{}
		}
	}
	return out
}
