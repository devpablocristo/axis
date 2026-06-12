package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
	authoidc "github.com/devpablocristo/platform/authn/go/oidc"
)

const (
	defaultInternalIssuer = "axis-bff"
	defaultDevOrgID       = "local-dev-org"
	defaultDevUserID      = "local-dev-admin"
)

type config struct {
	Addr              string
	AuthMode          string
	AuthIssuerURL     string
	AuthAudience      string
	DevOrgID          string
	DevUserID         string
	DevScopes         []string
	InternalJWTSecret string
	InternalJWTIssuer string
	CompanionBaseURL  string
	CompanionAudience string
	NexusBaseURL      string
	NexusAudience     string
	AllowedOrigin     string
	DownstreamTimeout time.Duration
	ReadHeaderTimeout time.Duration
}

type server struct {
	cfg      config
	oidcAuth authn.Authenticator
	client   *http.Client
}

func main() {
	cfg := loadConfig()
	srv, err := newServer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	log.Printf("axis bff listening on %s", cfg.Addr)
	log.Fatal(httpSrv.ListenAndServe())
}

func loadConfig() config {
	addr := env("PORT", "8080")
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}
	return config{
		Addr:              addr,
		AuthMode:          strings.ToLower(env("AXIS_BFF_AUTH_MODE", "dev")),
		AuthIssuerURL:     env("AXIS_AUTH_ISSUER_URL", ""),
		AuthAudience:      env("AXIS_AUTH_AUDIENCE", ""),
		DevOrgID:          env("AXIS_DEV_ORG_ID", defaultDevOrgID),
		DevUserID:         env("AXIS_DEV_USER_ID", defaultDevUserID),
		DevScopes:         splitScopes(env("AXIS_DEV_SCOPES", strings.Join(defaultAdminScopes(), " "))),
		InternalJWTSecret: env("AXIS_INTERNAL_JWT_SECRET", "axis-dev-internal-jwt-secret-change-me"),
		InternalJWTIssuer: env("AXIS_INTERNAL_JWT_ISSUER", defaultInternalIssuer),
		CompanionBaseURL:  env("COMPANION_BASE_URL", "http://localhost:18085"),
		CompanionAudience: env("AXIS_COMPANION_AUDIENCE", "companion"),
		NexusBaseURL:      env("NEXUS_BASE_URL", "http://localhost:18084"),
		NexusAudience:     env("AXIS_NEXUS_AUDIENCE", "nexus"),
		AllowedOrigin:     env("AXIS_ALLOWED_ORIGIN", ""),
		DownstreamTimeout: durationEnv("AXIS_DOWNSTREAM_TIMEOUT", 10*time.Second),
		ReadHeaderTimeout: durationEnv("AXIS_READ_HEADER_TIMEOUT", 5*time.Second),
	}
}

func newServer(cfg config) (*server, error) {
	if cfg.InternalJWTSecret == "" {
		return nil, errors.New("AXIS_INTERNAL_JWT_SECRET is required")
	}
	s := &server{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.DownstreamTimeout,
		},
	}
	if cfg.AuthMode == "oidc" {
		issuer := strings.TrimRight(strings.TrimSpace(cfg.AuthIssuerURL), "/")
		if issuer == "" {
			return nil, errors.New("AXIS_AUTH_ISSUER_URL is required when AXIS_BFF_AUTH_MODE=oidc")
		}
		expectedAudience := strings.TrimSpace(cfg.AuthAudience)
		s.oidcAuth = &authn.BearerJWTAuthenticator{
			Verify: authoidc.NewDiscoveryClient(issuer),
			Map: func(_ context.Context, claims map[string]any) (authn.Principal, error) {
				if normalizeClaimString(claims["iss"]) != issuer {
					return authn.Principal{}, errors.New("invalid issuer")
				}
				if expectedAudience != "" &&
					!claimContainsAudience(claims["aud"], expectedAudience) &&
					!claimContainsAudience(claims["azp"], expectedAudience) {
					return authn.Principal{}, errors.New("invalid audience")
				}
				sub := firstClaim(claims, "sub")
				if sub == "" {
					return authn.Principal{}, errors.New("missing sub")
				}
				return authn.Principal{
					OrgID:      firstClaim(claims, "org_id", "tenant_id", "orgId"),
					Actor:      firstClaim(claims, "email", "preferred_username", "username", "sub"),
					Role:       firstClaim(claims, "role"),
					Scopes:     claimScopes(claims),
					Claims:     claims,
					AuthMethod: "oidc",
				}, nil
			},
		}
	}
	return s, nil
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", s.ready)
	mux.Handle("GET /api/session", s.withAuth(http.HandlerFunc(s.session)))
	mux.Handle("GET /api/services", s.withAuth(http.HandlerFunc(s.services)))
	mux.Handle("/api/companion/", s.withAuth(s.proxy("companion", "/api/companion", s.cfg.CompanionBaseURL, s.cfg.CompanionAudience)))
	mux.Handle("/api/nexus/", s.withAuth(s.proxy("nexus", "/api/nexus", s.cfg.NexusBaseURL, s.cfg.NexusAudience)))
	return s.securityHeaders(mux)
}

func (s *server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *server) authenticate(r *http.Request) (authn.Principal, error) {
	if s.cfg.AuthMode == "dev" || s.cfg.AuthMode == "preview" || s.cfg.AuthMode == "stg" {
		return authn.Principal{
			OrgID:      firstNonEmpty(r.Header.Get("X-Dev-Org-ID"), s.cfg.DevOrgID),
			Actor:      firstNonEmpty(r.Header.Get("X-Dev-User-ID"), s.cfg.DevUserID),
			Role:       firstNonEmpty(r.Header.Get("X-Dev-Role"), "axis-admin"),
			Scopes:     firstNonEmptyScopes(splitScopes(r.Header.Get("X-Dev-Scopes")), s.cfg.DevScopes),
			AuthMethod: s.cfg.AuthMode,
		}, nil
	}
	if s.oidcAuth == nil {
		return authn.Principal{}, errors.New("oidc authenticator is not configured")
	}
	token, ok := authn.BearerToken(r.Header.Get("Authorization"))
	if !ok {
		return authn.Principal{}, errors.New("bearer token required")
	}
	p, err := s.oidcAuth.Authenticate(r.Context(), authn.BearerCredential{Token: token})
	if err != nil {
		return authn.Principal{}, err
	}
	if p == nil {
		return authn.Principal{}, errors.New("principal not resolved")
	}
	return *p, nil
}

type principalContextKey struct{}

func principalFromContext(ctx context.Context) authn.Principal {
	p, _ := ctx.Value(principalContextKey{}).(authn.Principal)
	return p
}

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	orgID, err := s.selectedOrg(r, p)
	if err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"actor_id":    p.Actor,
		"org_id":      orgID,
		"role":        p.Role,
		"scopes":      p.Scopes,
		"auth_method": p.AuthMethod,
	})
}

func (s *server) services(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": []map[string]string{
			{"name": "companion", "base_url": "/api/companion"},
			{"name": "nexus", "base_url": "/api/nexus"},
		},
	})
}

func (s *server) proxy(name, prefix, target, audience string) http.Handler {
	targetURL, err := url.Parse(target)
	if err != nil {
		log.Fatalf("invalid %s target %q: %v", name, target, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeError(w, http.StatusBadGateway, "DOWNSTREAM_UNAVAILABLE", err.Error())
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := principalFromContext(r.Context())
		orgID, err := s.selectedOrg(r, p)
		if err != nil {
			writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		now := time.Now().UTC()
		token, err := signInternalJWT(s.cfg.InternalJWTSecret, internalJWTClaims{
			Issuer:         s.cfg.InternalJWTIssuer,
			Audience:       audience,
			Subject:        p.Actor,
			ActorID:        p.Actor,
			ActorType:      "human",
			OrgID:          orgID,
			ProductSurface: "axis-console",
			Scopes:         p.Scopes,
			Service:        "axis-bff",
			OnBehalfOf:     p.Actor,
			ExpiresAt:      now.Add(5 * time.Minute),
			IssuedAt:       now,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TOKEN_SIGNING_FAILED", err.Error())
			return
		}

		r2 := r.Clone(r.Context())
		r2.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		if r2.URL.Path == "" {
			r2.URL.Path = "/"
		}
		r2.URL.RawPath = ""
		r2.Header = r.Header.Clone()
		r2.Header.Del("Cookie")
		r2.Header.Del("X-API-Key")
		// Nunca reenviar delegación de identidad del browser: el canal
		// legítimo es el claim on_behalf_of del internal JWT del BFF. Un
		// X-On-Behalf-Of inbound permitiría a un humano de consola forjar
		// decided_by aguas abajo (nexus approvals).
		r2.Header.Del("X-On-Behalf-Of")
		r2.Header.Set("Authorization", "Bearer "+token)
		r2.Header.Set("X-Request-ID", requestID(r))
		r2.Header.Set("X-Axis-Forwarded-By", "axis-bff")
		proxy.ServeHTTP(w, r2)
	})
}

func (s *server) selectedOrg(r *http.Request, p authn.Principal) (string, error) {
	requested := strings.TrimSpace(r.Header.Get("X-Axis-Org-ID"))
	principalOrg := strings.TrimSpace(p.OrgID)
	if requested == "" {
		requested = principalOrg
	}
	if requested == "" {
		return "", errors.New("org_id is required")
	}
	if principalOrg != "" && requested == principalOrg {
		return requested, nil
	}
	if hasScope(p.Scopes, "axis:cross_org", "nexus:cross_org", "companion:cross_org") {
		return requested, nil
	}
	return "", errors.New("selected org is not allowed for this principal")
}

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.DownstreamTimeout)
	defer cancel()
	companion := s.ping(ctx, s.cfg.CompanionBaseURL+"/readyz")
	nexus := s.ping(ctx, s.cfg.NexusBaseURL+"/readyz")
	status := http.StatusOK
	if companion != "ok" || nexus != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]string{"companion": companion, "nexus": nexus})
}

func (s *server) ping(ctx context.Context, rawURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "invalid_url"
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "unavailable"
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("status_%d", resp.StatusCode)
	}
	return "ok"
}

func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if s.cfg.AllowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", s.cfg.AllowedOrigin)
			w.Header().Set("Access-Control-Allow-Headers", "authorization, content-type, x-axis-org-id")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type internalJWTClaims struct {
	Issuer         string
	Audience       string
	Subject        string
	ActorID        string
	ActorType      string
	OrgID          string
	ProductSurface string
	Scopes         []string
	Service        string
	OnBehalfOf     string
	ExpiresAt      time.Time
	IssuedAt       time.Time
}

func signInternalJWT(secret string, c internalJWTClaims) (string, error) {
	headerJSON, err := json.Marshal(map[string]any{"typ": "JWT", "alg": "HS256"})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(map[string]any{
		"iss":               c.Issuer,
		"aud":               c.Audience,
		"sub":               c.Subject,
		"actor_id":          c.ActorID,
		"actor_type":        c.ActorType,
		"org_id":            c.OrgID,
		"product_surface":   c.ProductSurface,
		"scope":             strings.Join(c.Scopes, " "),
		"scp":               c.Scopes,
		"service":           c.Service,
		"service_principal": true,
		"on_behalf_of":      c.OnBehalfOf,
		"iat":               c.IssuedAt.Unix(),
		"exp":               c.ExpiresAt.Unix(),
	})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	return fallback
}

func requestID(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Request-ID")); value != "" {
		return value
	}
	return fmt.Sprintf("axis-%d", time.Now().UnixNano())
}

func defaultAdminScopes() []string {
	return []string{
		"axis:cross_org",
		"companion:cross_org",
		"companion:runtime:admin",
		"companion:tasks:read",
		"companion:tasks:write",
		"companion:connectors:execute",
		"companion:connectors:admin",
		"companion:memory:read",
		"companion:memory:write",
		"companion:memory:admin",
		"companion:capabilities:read",
		"companion:capabilities:admin",
		"companion:agents:read",
		"companion:agents:admin",
		"companion:observability:read",
		"companion:costs:read",
		"companion:evals:admin",
		"companion:watchers:read",
		"companion:watchers:write",
		"companion:watchers:execute",
		"companion:nexus:read",
		"companion:nexus:admin",
		"companion:nexus-assist:read",
		"companion:nexus-assist:admin",
		"nexus:requests:read",
		"nexus:requests:write",
		"nexus:requests:result",
		"nexus:approvals:decide",
		"nexus:policies:admin",
		"nexus:rbac:admin",
		"nexus:evidence:write",
		"nexus:dashboard:read",
		"nexus:learning:propose",
		"nexus:cross_org",
	}
}

func splitScopes(raw string) []string {
	raw = strings.NewReplacer(",", " ", ";", " ", "+", " ").Replace(raw)
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func firstNonEmptyScopes(values, fallback []string) []string {
	if len(values) > 0 {
		return values
	}
	return append([]string(nil), fallback...)
}

func hasScope(scopes []string, values ...string) bool {
	for _, scope := range scopes {
		for _, value := range values {
			if strings.TrimSpace(scope) == value {
				return true
			}
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstClaim(claims map[string]any, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(normalizeClaimString(claims[name])); value != "" {
			return value
		}
	}
	return ""
}

func normalizeClaimString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimRight(strings.TrimSpace(v), "/")
	default:
		return ""
	}
}

func claimContainsAudience(value any, expected string) bool {
	expected = strings.TrimSpace(expected)
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
			if strings.TrimSpace(normalizeClaimString(item)) == expected {
				return true
			}
		}
	}
	return false
}

func claimScopes(claims map[string]any) []string {
	if scopes := splitScopes(normalizeClaimString(claims["scope"])); len(scopes) > 0 {
		return scopes
	}
	switch v := claims["scp"].(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if scope := normalizeClaimString(item); scope != "" {
				out = append(out, scope)
			}
		}
		return out
	default:
		return nil
	}
}
