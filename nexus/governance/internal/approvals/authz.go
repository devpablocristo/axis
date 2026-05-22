package approvals

import (
	"net/http"
	"strings"

	approvaldomain "github.com/devpablocristo/nexus/governance/internal/approvals/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusApprovalsDecide = "nexus:approvals:decide"
	scopeNexusCrossOrg        = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

// requestOrgScope traduce el contexto de auth del request HTTP a parámetros
// que el repo pueda aplicar como WHERE en SQL. Espeja la semántica de
// canAccessApprovalOrg para mantener consistencia entre filtro SQL y
// post-check por item.
func requestOrgScope(r *http.Request) (*string, bool, bool) {
	if requestHasNoAuthContext(r) {
		return nil, true, true
	}
	if requestHasScope(r, scopeNexusCrossOrg) {
		if orgID := strings.TrimSpace(r.Header.Get("X-Org-ID")); orgID != "" {
			return &orgID, false, true
		}
		return nil, true, true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID != "" {
		return &orgID, false, true
	}
	return nil, false, false
}

func canAccessApprovalOrg(r *http.Request, approval approvaldomain.Approval) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID == "" {
		return false
	}
	if approval.OrgID == nil {
		return false
	}
	return strings.TrimSpace(*approval.OrgID) == orgID
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
