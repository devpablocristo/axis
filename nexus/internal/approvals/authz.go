package approvals

import (
	"net/http"
	"strings"

	approvaldomain "github.com/devpablocristo/nexus/internal/approvals/usecases/domain"
	"github.com/devpablocristo/nexus/internal/orgctx"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusApprovalsDecide = "nexus:approvals:decide"
	scopeNexusCrossOrg        = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

// requestOrgScope traduce el contexto de auth del request HTTP a parámetros
// que el repo pueda aplicar como WHERE en SQL. Espeja la semántica de
// canAccessApprovalOrg para mantener consistencia entre filtro SQL y
// post-check por item. Principals cross_org pueden acotar su vista a un org
// puntual vía X-Org-ID inbound (preservado en orgctx antes de que el
// middleware de authn rebindee el header); sin cross_org se ignora.
func requestOrgScope(r *http.Request) (*string, bool, bool) {
	if identityhttp.HasNoAuthContext(r) {
		return nil, true, true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	if identityhttp.HasScope(r, scopeNexusCrossOrg) {
		if narrowed := orgctx.Narrowed(r, orgID); narrowed != "" {
			return &narrowed, false, true
		}
		return nil, true, true
	}
	if orgID != "" {
		return &orgID, false, true
	}
	return nil, false, false
}

func canAccessApprovalOrg(r *http.Request, approval approvaldomain.Approval) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	if orgID == "" || approval.OrgID == nil {
		return false
	}
	return strings.TrimSpace(*approval.OrgID) == orgID
}
