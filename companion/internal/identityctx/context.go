package identityctx

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
)

const (
	CompanionPrincipal = "companion.employee_ai"
	DefaultSurface     = "companion"

	HeaderProductSurface = "X-Product-Surface"
)

type contextKey struct{}

// IdentityContext is Companion's canonical request identity.
//
// CustomerOrgID is the work context where Companion acts. It maps to the
// existing physical org_id column, but it does not mean the customer administers
// Companion itself.
type IdentityContext struct {
	CustomerOrgID      string   `json:"customer_org_id,omitempty"`
	HumanUserID        string   `json:"human_user_id,omitempty"`
	ActorType          string   `json:"actor_type,omitempty"`
	CompanionPrincipal string   `json:"companion_principal"`
	OnBehalfOf         string   `json:"on_behalf_of,omitempty"`
	ProductSurface     string   `json:"product_surface,omitempty"`
	Scopes             []string `json:"scopes,omitempty"`
	AuthMethod         string   `json:"auth_method,omitempty"`
	ServicePrincipal   bool     `json:"service_principal,omitempty"`
}

func WithPrincipal(r *http.Request, principal *authn.Principal) *http.Request {
	if r == nil || principal == nil {
		return r
	}
	id := fromPrincipal(principal)
	req := r.Clone(context.WithValue(r.Context(), contextKey{}, id))
	req.Header = r.Header.Clone()
	return req
}

func FromRequest(r *http.Request) IdentityContext {
	if r == nil {
		return normalize(IdentityContext{})
	}
	if id, ok := r.Context().Value(contextKey{}).(IdentityContext); ok {
		if surface := strings.TrimSpace(r.Header.Get(HeaderProductSurface)); surface != "" {
			id.ProductSurface = surface
		}
		return normalize(id)
	}
	return normalize(fromHTTPContext(r))
}

func (id IdentityContext) WithProductSurface(surface string) IdentityContext {
	if value := strings.TrimSpace(surface); value != "" {
		id.ProductSurface = value
	}
	return normalize(id)
}

func (id IdentityContext) EffectiveActorID() string {
	for _, value := range []string{id.HumanUserID, id.OnBehalfOf, id.CompanionPrincipal} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return CompanionPrincipal
}

func (id IdentityContext) IsZeroWorkContext() bool {
	return strings.TrimSpace(id.CustomerOrgID) == ""
}

func (id IdentityContext) HasScope(scope string) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return false
	}
	for _, current := range id.Scopes {
		if current == scope {
			return true
		}
	}
	return false
}

func (id IdentityContext) HasAnyScope(scopes ...string) bool {
	for _, scope := range scopes {
		if id.HasScope(scope) {
			return true
		}
	}
	return false
}

func (id IdentityContext) CanActAs(actorID string, operatorScopes ...string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return true
	}
	if id.HasAnyScope(operatorScopes...) {
		return true
	}
	for _, value := range []string{id.EffectiveActorID(), id.HumanUserID, id.OnBehalfOf} {
		if strings.TrimSpace(value) == actorID {
			return true
		}
	}
	return false
}

func HasNoAuthContext(r *http.Request) bool {
	if r != nil {
		if _, ok := r.Context().Value(contextKey{}).(IdentityContext); ok {
			return false
		}
	}
	return identityhttp.HasNoAuthContext(r)
}

func HasScope(r *http.Request, scope string) bool {
	if FromRequest(r).HasScope(scope) {
		return true
	}
	return identityhttp.HasScope(r, scope)
}

func HasAnyScope(r *http.Request, scopes ...string) bool {
	return FromRequest(r).HasAnyScope(scopes...)
}

func PrincipalOrgID(r *http.Request) string {
	return FromRequest(r).CustomerOrgID
}

func PrincipalUserID(r *http.Request) string {
	return FromRequest(r).HumanUserID
}

func ProductSurface(r *http.Request) string {
	return FromRequest(r).ProductSurface
}

func WorkIdentity(r *http.Request) (IdentityContext, bool) {
	id := FromRequest(r)
	if id.CustomerOrgID == "" {
		return id, false
	}
	return id, true
}

func WorkIdentityForOrg(r *http.Request, requestedOrgID, crossOrgScope string) (IdentityContext, bool) {
	id := FromRequest(r)
	orgID, ok := EffectiveOrgID(r, requestedOrgID, crossOrgScope)
	if !ok || strings.TrimSpace(orgID) == "" {
		return id, false
	}
	id.CustomerOrgID = orgID
	return normalize(id), true
}

func CanAccessOrg(r *http.Request, orgID, crossOrgScope string) bool {
	orgID = strings.TrimSpace(orgID)
	if HasNoAuthContext(r) {
		return true
	}
	if crossOrgScope != "" && HasScope(r, crossOrgScope) {
		return true
	}
	return orgID != "" && PrincipalOrgID(r) == orgID
}

func EffectiveOrgID(r *http.Request, requestedOrgID, crossOrgScope string) (string, bool) {
	requestedOrgID = strings.TrimSpace(requestedOrgID)
	id := FromRequest(r)
	if HasNoAuthContext(r) {
		if id.CustomerOrgID != "" {
			if requestedOrgID == "" || requestedOrgID == id.CustomerOrgID {
				return id.CustomerOrgID, true
			}
			return "", false
		}
		return requestedOrgID, true
	}
	if crossOrgScope != "" && HasScope(r, crossOrgScope) {
		if requestedOrgID != "" {
			return requestedOrgID, true
		}
		return id.CustomerOrgID, true
	}
	if id.CustomerOrgID == "" {
		return "", false
	}
	if requestedOrgID != "" && requestedOrgID != id.CustomerOrgID {
		return "", false
	}
	return id.CustomerOrgID, true
}

func fromPrincipal(principal *authn.Principal) IdentityContext {
	claims := principal.Claims
	scopes := principal.Scopes
	if len(scopes) == 0 {
		scopes = claimScopes(claims, "scope", "scp")
	}
	actorID := strings.TrimSpace(principal.Actor)
	if actorID == "" {
		actorID = firstNonEmptyClaim(claims, "actor_id")
	}
	servicePrincipal := claimBool(claims, "service_principal") || claimBool(claims, "service")
	actorType := firstNonEmptyClaim(claims, "actor_type")
	if actorType == "" {
		if servicePrincipal {
			actorType = "service"
		} else if actorID != "" {
			actorType = "human"
		}
	}
	onBehalfOf := firstNonEmptyClaim(claims, "on_behalf_of")
	humanUserID := firstNonEmptyClaim(claims, "human_user_id", "user_id")
	if humanUserID == "" && onBehalfOf != "" {
		humanUserID = onBehalfOf
	}
	if humanUserID == "" && strings.EqualFold(actorType, "human") {
		humanUserID = actorID
	}
	if humanUserID == "" && !servicePrincipal {
		humanUserID = actorID
	}
	return normalize(IdentityContext{
		CustomerOrgID:      principal.OrgID,
		HumanUserID:        humanUserID,
		ActorType:          actorType,
		CompanionPrincipal: firstNonEmptyClaim(claims, "companion_principal"),
		OnBehalfOf:         onBehalfOf,
		ProductSurface:     firstNonEmptyClaim(claims, "product_surface"),
		Scopes:             scopes,
		AuthMethod:         principal.AuthMethod,
		ServicePrincipal:   servicePrincipal,
	})
}

func fromHTTPContext(r *http.Request) IdentityContext {
	ctx := identityhttp.FromRequest(r)
	actorType := strings.TrimSpace(r.Header.Get("X-Actor-Type"))
	if actorType == "" {
		if ctx.ServicePrincipal {
			actorType = "service"
		} else if ctx.Actor != "" {
			actorType = "human"
		}
	}
	onBehalfOf := strings.TrimSpace(r.Header.Get("X-On-Behalf-Of"))
	humanUserID := strings.TrimSpace(r.Header.Get(identityhttp.HeaderUserID))
	if ctx.ServicePrincipal && onBehalfOf == "" && !strings.EqualFold(actorType, "human") {
		humanUserID = ""
	}
	if humanUserID == "" && onBehalfOf != "" {
		humanUserID = onBehalfOf
	}
	if humanUserID == "" && !ctx.ServicePrincipal {
		humanUserID = ctx.Actor
	}
	return normalize(IdentityContext{
		CustomerOrgID:      ctx.OrgID,
		HumanUserID:        humanUserID,
		ActorType:          actorType,
		CompanionPrincipal: strings.TrimSpace(r.Header.Get("X-Companion-Principal")),
		OnBehalfOf:         onBehalfOf,
		ProductSurface:     strings.TrimSpace(r.Header.Get(HeaderProductSurface)),
		Scopes:             ctx.Scopes,
		AuthMethod:         ctx.AuthMethod,
		ServicePrincipal:   ctx.ServicePrincipal,
	})
}

func normalize(id IdentityContext) IdentityContext {
	id.CustomerOrgID = strings.TrimSpace(id.CustomerOrgID)
	id.HumanUserID = strings.TrimSpace(id.HumanUserID)
	id.ActorType = strings.TrimSpace(id.ActorType)
	id.CompanionPrincipal = strings.TrimSpace(id.CompanionPrincipal)
	if id.CompanionPrincipal == "" {
		id.CompanionPrincipal = CompanionPrincipal
	}
	id.OnBehalfOf = strings.TrimSpace(id.OnBehalfOf)
	id.ProductSurface = strings.TrimSpace(id.ProductSurface)
	if id.ProductSurface == "" {
		id.ProductSurface = DefaultSurface
	}
	id.AuthMethod = strings.TrimSpace(id.AuthMethod)
	id.Scopes = cleanScopes(id.Scopes)
	return id
}

func cleanScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func claimScopes(claims map[string]any, names ...string) []string {
	for _, name := range names {
		if claims == nil {
			return nil
		}
		scopes := valueToScopes(claims[name])
		if len(scopes) > 0 {
			return scopes
		}
	}
	return nil
}

func valueToScopes(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return splitScopeString(v)
	case []string:
		return v
	case []any:
		scopes := make([]string, 0, len(v))
		for _, item := range v {
			scopes = append(scopes, splitScopeString(claimToString(item))...)
		}
		return scopes
	default:
		return splitScopeString(claimToString(v))
	}
}

func splitScopeString(raw string) []string {
	raw = strings.NewReplacer(",", " ", ";", " ", "+", " ").Replace(raw)
	return strings.Fields(raw)
}

func firstNonEmptyClaim(claims map[string]any, names ...string) string {
	for _, name := range names {
		if claims == nil {
			return ""
		}
		if value := strings.TrimSpace(claimToString(claims[name])); value != "" {
			return value
		}
	}
	return ""
}

func claimToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func claimBool(claims map[string]any, name string) bool {
	if claims == nil {
		return false
	}
	switch value := claims[name].(type) {
	case bool:
		return value
	case string:
		return parseBool(value)
	default:
		return false
	}
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}
