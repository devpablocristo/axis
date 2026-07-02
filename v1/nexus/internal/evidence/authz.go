package evidence

import (
	"net/http"
	"strings"

	evidencedomain "github.com/devpablocristo/nexus/internal/evidence/usecases/domain"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusRequestsRead = "nexus:requests:read"
	scopeNexusCrossOrg     = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func canAccessEvidenceOrg(r *http.Request, pack evidencedomain.EvidencePack) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := identityhttp.PrincipalOrgID(r)
	// Antes esto sacaba el org del bag user-controlled `Params["org_id"]`,
	// permitiendo bypass cross-org si el caller original no incluía la
	// clave. Ahora usamos pack.Request.OrgID que viene de la columna
	// requests.org_id (autoritativa).
	packOrg := strings.TrimSpace(pack.Request.OrgID)
	if orgID == "" {
		return false
	}
	return packOrg == orgID
}
