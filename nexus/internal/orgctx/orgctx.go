// Package orgctx preserva el org solicitado por el caller (header X-Org-ID)
// ANTES de que el middleware de authn reescriba los identity headers con los
// datos del principal (identityhttp.WithPrincipal borra y rebindea X-Org-ID).
//
// Uso: el middleware de wire captura el header inbound en el context; los
// helpers de org-scope lo consultan SOLO cuando el principal tiene
// nexus:cross_org, para acotar su vista a un org puntual en vez de allowAll.
// No es un canal de escalación: principals sin cross_org lo ignoran y siguen
// bound a su propio org.
package orgctx

import (
	"context"
	"net/http"
	"strings"
)

type contextKey struct{}

// WithRequested guarda el org solicitado por el caller en el context.
// Valores vacíos no se guardan.
func WithRequested(ctx context.Context, orgID string) context.Context {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, orgID)
}

// Requested retorna el org solicitado por el caller, o "" si no hubo.
func Requested(ctx context.Context) string {
	value, _ := ctx.Value(contextKey{}).(string)
	return value
}

// RequestedFromRequest es el helper para handlers HTTP.
func RequestedFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return Requested(r.Context())
}

// Narrowed resuelve el org al que un principal cross_org debe acotar su
// vista: prioriza el org solicitado por el caller (preservado en el context)
// y cae al org del principal. "" significa sin acotar (vista cross-org
// completa). Los call sites SOLO deben invocarlo en el branch cross_org:
// para principals sin cross_org el org solicitado se ignora.
func Narrowed(r *http.Request, principalOrg string) string {
	if requested := RequestedFromRequest(r); requested != "" {
		return requested
	}
	return strings.TrimSpace(principalOrg)
}
