package findings

import (
	"net/http"
	"strings"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeNexusFindingsRead  = "nexus:findings:read"
	scopeNexusFindingsWrite = "nexus:findings:write"
	scopeNexusCrossOrg      = "nexus:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func canWriteOwner(r *http.Request, ownerSystem string) bool {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		return true
	}
	actor := identityhttp.FromRequest(r).Actor
	return actor != "" && strings.EqualFold(actor, strings.TrimSpace(ownerSystem))
}
