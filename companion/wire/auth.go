package wire

import (
	"context"
	"errors"
	"net/http"
	"strings"

	authn "github.com/devpablocristo/platform/authn/go"
	authoidc "github.com/devpablocristo/platform/authn/go/oidc"
	sharedapikey "github.com/devpablocristo/platform/security/go/apikey"
)

type apiKeyMetadata struct {
	Actor            string
	Role             string
	OrgID            string
	Scopes           []string
	ServicePrincipal bool
}

func newAuthMiddleware(apiKeys, issuerURL, audience string, internalJWT internalJWTConfig) (func(http.Handler) http.Handler, error) {
	apiKeyAuth, err := newAPIKeyAuthenticator(apiKeys)
	if err != nil {
		return nil, err
	}
	jwtAuth := newFallbackAuthenticator(
		newInternalJWTAuthenticator(internalJWT),
		newJWTAuthenticator(issuerURL, audience),
	)

	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}

			principal, method, err := authn.TryInbound(
				r.Context(),
				jwtAuth,
				apiKeyAuth,
				r.Header.Get("Authorization"),
				r.Header.Get("X-API-Key"),
			)
			if err != nil || principal == nil {
				writeUnauthorized(w)
				return
			}

			req := withIdentityHeaders(r, principal, method)
			next.ServeHTTP(w, req)
		})
	}, nil
}

func newAPIKeyAuthenticator(raw string) (authn.Authenticator, error) {
	sanitized, metadata := parseAPIKeyConfig(raw)
	base, err := sharedapikey.NewAuthenticator(sanitized)
	if err != nil {
		return nil, err
	}
	return &authn.APIKeyFuncAuthenticator{
		Resolve: func(_ context.Context, rawKey string) (*authn.Principal, error) {
			principal, ok := base.Authenticate(rawKey)
			if !ok {
				return nil, errors.New("authn: invalid api key")
			}
			meta := metadata[principal.Name]
			actor := firstNonEmpty(meta.Actor, principal.Name)
			role := firstNonEmpty(meta.Role, principal.Name)
			scopes := append([]string(nil), meta.Scopes...)
			if len(scopes) == 0 {
				scopes = defaultAPIKeyScopes(principal.Name)
			}
			return &authn.Principal{
				OrgID:      meta.OrgID,
				Actor:      actor,
				Role:       role,
				Scopes:     scopes,
				Claims:     map[string]any{"service_principal": meta.ServicePrincipal},
				AuthMethod: "api_key",
			}, nil
		},
	}, nil
}

func newJWTAuthenticator(issuerURL, audience string) authn.Authenticator {
	expectedIssuer := normalizeIssuer(issuerURL)
	if expectedIssuer == "" {
		return nil
	}

	discovery := authoidc.NewDiscoveryClient(expectedIssuer)
	expectedAudience := strings.TrimSpace(audience)

	return &authn.BearerJWTAuthenticator{
		Verify: discovery,
		Map: func(_ context.Context, claims map[string]any) (authn.Principal, error) {
			if normalizeIssuer(claims["iss"]) != expectedIssuer {
				return authn.Principal{}, errors.New("authn: invalid issuer")
			}
			if expectedAudience != "" &&
				!claimContainsAudience(claims["aud"], expectedAudience) &&
				!claimContainsAudience(claims["azp"], expectedAudience) {
				return authn.Principal{}, errors.New("authn: invalid audience")
			}

			sub := strings.TrimSpace(claimString(claims["sub"]))
			if sub == "" {
				return authn.Principal{}, errors.New("authn: missing sub claim")
			}

			actor := firstNonEmptyClaim(claims, "email", "preferred_username", "username", "sub")
			return authn.Principal{
				OrgID:      firstNonEmptyClaim(claims, "org_id", "tenant_id", "orgId"),
				Actor:      actor,
				Role:       firstNonEmptyClaim(claims, "role"),
				Scopes:     claimScopes(claims),
				Claims:     claims,
				AuthMethod: "jwt",
			}, nil
		},
	}
}

func withIdentityHeaders(r *http.Request, principal *authn.Principal, method string) *http.Request {
	if principal == nil {
		return r
	}

	req := r.Clone(r.Context())
	req.Header = r.Header.Clone()
	req.Header.Del("X-User-ID")
	req.Header.Del("X-Org-ID")
	req.Header.Del("X-Auth-Role")
	req.Header.Del("X-Auth-Scopes")
	req.Header.Del("X-Auth-Method")
	req.Header.Del("X-Service-Principal")
	if actor := strings.TrimSpace(principal.Actor); actor != "" {
		req.Header.Set("X-User-ID", actor)
	}
	if orgID := strings.TrimSpace(principal.OrgID); orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if role := strings.TrimSpace(principal.Role); role != "" {
		req.Header.Set("X-Auth-Role", role)
	}
	if len(principal.Scopes) > 0 {
		req.Header.Set("X-Auth-Scopes", strings.Join(principal.Scopes, " "))
	}
	if principal.AuthMethod != "" {
		req.Header.Set("X-Auth-Method", principal.AuthMethod)
	} else if method != "" {
		req.Header.Set("X-Auth-Method", method)
	}
	if principalServicePrincipal(principal) {
		req.Header.Set("X-Service-Principal", "true")
	}
	return req
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"valid credentials required"}}`))
}

func normalizeIssuer(value any) string {
	return strings.TrimRight(strings.TrimSpace(claimString(value)), "/")
}

func claimString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func firstNonEmptyClaim(claims map[string]any, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(claimString(claims[name])); value != "" {
			return value
		}
	}
	return ""
}

func claimContainsAudience(value any, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == expected
	case []string:
		for _, item := range v {
			if strings.TrimSpace(item) == expected {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if strings.TrimSpace(claimString(item)) == expected {
				return true
			}
		}
	}
	return false
}

func claimScopes(claims map[string]any) []string {
	raw := claims["scope"]
	if raw == nil {
		raw = claims["scp"]
	}

	switch v := raw.(type) {
	case string:
		parts := strings.Fields(v)
		return append([]string(nil), parts...)
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if scope := strings.TrimSpace(claimString(item)); scope != "" {
				out = append(out, scope)
			}
		}
		return out
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseAPIKeyConfig(raw string) (string, map[string]apiKeyMetadata) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	sanitized := make([]string, 0, len(parts))
	metadata := make(map[string]apiKeyMetadata, len(parts))
	for _, part := range parts {
		piece := strings.TrimSpace(part)
		if piece == "" {
			continue
		}
		name, rhs, ok := strings.Cut(piece, "=")
		if !ok {
			sanitized = append(sanitized, piece)
			continue
		}
		name = strings.TrimSpace(name)
		rhs = strings.TrimSpace(rhs)
		if name == "" || rhs == "" {
			sanitized = append(sanitized, piece)
			continue
		}
		secret, meta := parseAPIKeyValue(rhs)
		if secret == "" {
			secret = rhs
		}
		sanitized = append(sanitized, name+"="+secret)
		if meta.Actor == "" {
			meta.Actor = name
		}
		if meta.Role == "" {
			meta.Role = name
		}
		metadata[name] = meta
	}
	return strings.Join(sanitized, ","), metadata
}

func parseAPIKeyValue(value string) (string, apiKeyMetadata) {
	segments := strings.Split(value, "|")
	secret := strings.TrimSpace(segments[0])
	meta := apiKeyMetadata{}
	for _, segment := range segments[1:] {
		key, raw, ok := strings.Cut(strings.TrimSpace(segment), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "actor", "actor_id", "user", "user_id":
			meta.Actor = strings.TrimSpace(raw)
		case "role":
			meta.Role = strings.TrimSpace(raw)
		case "org", "org_id", "tenant", "tenant_id":
			meta.OrgID = strings.TrimSpace(raw)
		case "scope", "scopes":
			meta.Scopes = parseScopeList(raw)
		case "service", "service_principal":
			meta.ServicePrincipal = parseBool(raw)
		}
	}
	return secret, meta
}

func parseScopeList(raw string) []string {
	raw = strings.NewReplacer(";", " ", "+", " ").Replace(raw)
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		scope := strings.TrimSpace(field)
		if scope == "" {
			continue
		}
		if _, exists := seen[scope]; exists {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func parseBool(raw string) bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "1", "true", "yes", "y", "service":
		return true
	default:
		return false
	}
}

func defaultAPIKeyScopes(name string) []string {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "admin":
		return []string{
			"companion:tasks:read",
			"companion:tasks:write",
			"companion:connectors:execute",
			"companion:connectors:admin",
			"companion:watchers:read",
			"companion:watchers:write",
			"companion:watchers:execute",
			"companion:nexus:read",
			"companion:nexus:admin",
			"companion:nexus-assist:read",
			"companion:nexus-assist:admin",
		}
	default:
		return nil
	}
}

func principalServicePrincipal(principal *authn.Principal) bool {
	if principal == nil || principal.Claims == nil {
		return false
	}
	for _, key := range []string{"service_principal", "service"} {
		switch value := principal.Claims[key].(type) {
		case bool:
			if value {
				return true
			}
		case string:
			if parseBool(value) {
				return true
			}
		}
	}
	return false
}
