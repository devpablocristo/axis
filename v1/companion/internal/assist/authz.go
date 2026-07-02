package assist

import (
	"net/http"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeCompanionAssistRead  = "companion:assist:read"
	scopeCompanionAssistWrite = "companion:assist:write"
	scopeCompanionCrossOrg    = "companion:cross_org"
)

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func canWriteOwner(r *http.Request, ownerSystem string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasScope(r, scopeCompanionCrossOrg) {
		return true
	}
	actor := identityctx.FromRequest(r).EffectiveActorID()
	if strings.EqualFold(actor, "admin") {
		return true
	}
	return actor != "" && strings.EqualFold(actor, strings.TrimSpace(ownerSystem))
}

func canAccessOrg(r *http.Request, orgID string) bool {
	return identityctx.CanAccessOrg(r, orgID, scopeCompanionCrossOrg)
}
