package policies

import (
	"net/http"
	"strings"

	policydomain "github.com/devpablocristo/nexus/internal/policies/usecases/domain"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusPoliciesAdmin = "nexus:policies:admin"
	scopeNexusCrossOrg      = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func principalOrgID(r *http.Request) *string {
	orgID := identityhttp.PrincipalOrgID(r)
	if orgID == "" {
		return nil
	}
	return &orgID
}

func canAccessPolicyOrg(r *http.Request, policy policydomain.Policy) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	if policy.OrgID == nil {
		return true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	return orgID != "" && strings.TrimSpace(*policy.OrgID) == orgID
}

func canWritePolicyOrg(r *http.Request, policy policydomain.Policy) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	return orgID != "" && policy.OrgID != nil && strings.TrimSpace(*policy.OrgID) == orgID
}
