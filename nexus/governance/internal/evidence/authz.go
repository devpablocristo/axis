package evidence

import (
	"net/http"
	"strings"

	evidencedomain "github.com/devpablocristo/nexus/governance/internal/evidence/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusRequestsRead = "nexus:requests:read"
	scopeNexusCrossOrg     = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func canAccessEvidenceOrg(r *http.Request, pack evidencedomain.EvidencePack) bool {
	if requestHasNoAuthContext(r) || requestHasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
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
