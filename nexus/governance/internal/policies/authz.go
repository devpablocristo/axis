package policies

import (
	"net/http"
	"strings"

	policydomain "github.com/devpablocristo/nexus/governance/internal/policies/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusPoliciesAdmin = "nexus:policies:admin"
	scopeNexusCrossOrg      = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) *string {
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID == "" {
		return nil
	}
	return &orgID
}

func canAccessPolicyOrg(r *http.Request, policy policydomain.Policy) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if policy.OrgID == nil {
		return true
	}
	return orgID != "" && strings.TrimSpace(*policy.OrgID) == orgID
}

func canWritePolicyOrg(r *http.Request, policy policydomain.Policy) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	return orgID != "" && policy.OrgID != nil && strings.TrimSpace(*policy.OrgID) == orgID
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
