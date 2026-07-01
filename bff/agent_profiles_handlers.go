package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
)

func (s *server) virployeeProfilesAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts, err := virployeeProfileRouteParts(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid virployee profile path")
		return
	}
	companionMethod, companionPath, requiredScopes, ok := virployeeProfileRoute(r.Method, parts)
	if !ok {
		http.NotFound(w, r)
		return
	}
	orgID, productSurface, tenantID, scopes, err := s.resolveAppContext(r, p)
	if err != nil {
		writeAppContextError(w, err)
		return
	}
	if !requireScope(w, authn.Principal{Scopes: scopes}, requiredScopes...) {
		return
	}
	var body io.Reader
	if r.Body != nil && (r.Method == http.MethodPatch || r.Method == http.MethodPost) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", "invalid request body")
			return
		}
		body = bytes.NewReader(raw)
	}
	s.forwardVirployeeProfileRequest(w, r, p, orgID, productSurface, tenantID, scopes, companionMethod, companionPath, body)
}

func (s *server) forwardVirployeeProfileRequest(w http.ResponseWriter, r *http.Request, p authn.Principal, orgID string, productSurface string, tenantID string, scopes []string, method string, companionPath string, body io.Reader) {
	target, err := url.Parse(s.cfg.CompanionBaseURL)
	if err != nil {
		writeLoggedError(w, http.StatusInternalServerError, "COMPANION_URL_INVALID", "companion URL is invalid", err)
		return
	}
	target.Path = companionPath
	target.RawQuery = r.URL.RawQuery
	token, err := s.signDownstreamTokenForContext(p, orgID, productSurface, tenantID, scopes, s.cfg.CompanionAudience)
	if err != nil {
		writeLoggedError(w, http.StatusInternalServerError, "TOKEN_SIGNING_FAILED", "token signing failed", err)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), method, target.String(), body)
	if err != nil {
		writeLoggedError(w, http.StatusInternalServerError, "REQUEST_BUILD_FAILED", "request build failed", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Product-Surface", productSurface)
	req.Header.Set("X-Tenant-ID", tenantID)
	if r.Header.Get("Content-Type") != "" {
		req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	}
	req.Header.Set("X-Request-ID", requestID(r))
	req.Header.Set("X-Axis-Forwarded-By", "axis-bff")
	resp, err := s.client.Do(req)
	if err != nil {
		writeLoggedError(w, http.StatusBadGateway, "DOWNSTREAM_UNAVAILABLE", "downstream request failed", err)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", firstNonEmpty(resp.Header.Get("Content-Type"), "application/json"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func virployeeProfileRoute(method string, parts []string) (string, string, []string, bool) {
	readScopes := []string{"companion:virployee_profiles:read", "companion:virployee_profiles:admin", "companion:runtime:admin"}
	writeScopes := []string{"companion:virployee_profiles:admin", "companion:runtime:admin"}
	if len(parts) == 0 {
		switch method {
		case http.MethodGet:
			return http.MethodGet, "/v1/virployee-profiles", readScopes, true
		case http.MethodPost:
			return http.MethodPost, "/v1/virployee-profiles", writeScopes, true
		}
	}
	if len(parts) == 1 {
		profilePath := "/v1/virployee-profiles/" + url.PathEscape(parts[0])
		switch method {
		case http.MethodGet:
			return http.MethodGet, profilePath, readScopes, true
		case http.MethodPatch:
			return http.MethodPatch, profilePath, writeScopes, true
		}
	}
	if len(parts) == 2 {
		profilePath := "/v1/virployee-profiles/" + url.PathEscape(parts[0])
		switch {
		case method == http.MethodGet && parts[1] == "versions":
			return http.MethodGet, profilePath + "/versions", readScopes, true
		case method == http.MethodPost && (parts[1] == "status" || parts[1] == "archive" || parts[1] == "restore" || parts[1] == "trash"):
			return http.MethodPost, profilePath + "/" + parts[1], writeScopes, true
		case method == http.MethodDelete && parts[1] == "purge":
			return http.MethodDelete, profilePath + "/purge", writeScopes, true
		}
	}
	return "", "", nil, false
}

func virployeeProfileRouteParts(path string) ([]string, error) {
	path = strings.TrimPrefix(path, "/api/virployee-profiles")
	return profileRouteParts(path, "virployee profile")
}

func profileRouteParts(path string, resource string) ([]string, error) {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil, nil
	}
	rawParts := strings.Split(path, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(decoded) == "" {
			return nil, fmt.Errorf("empty %s path segment", resource)
		}
		parts = append(parts, decoded)
	}
	return parts, nil
}

func (s *server) signDownstreamToken(p authn.Principal, orgID string, audience string) (string, error) {
	return s.signDownstreamTokenForContext(p, orgID, "axis-console", "", p.Scopes, audience)
}

func (s *server) signDownstreamTokenForContext(p authn.Principal, orgID string, productSurface string, tenantID string, scopes []string, audience string) (string, error) {
	now := time.Now().UTC()
	return signInternalJWT(s.cfg.InternalJWTSecret, internalJWTClaims{
		Issuer:         s.cfg.InternalJWTIssuer,
		Audience:       audience,
		Subject:        p.Actor,
		ActorID:        p.Actor,
		ActorType:      "human",
		OrgID:          orgID,
		TenantID:       tenantID,
		ProductSurface: productSurface,
		Scopes:         scopes,
		Service:        "axis-bff",
		OnBehalfOf:     p.Actor,
		ExpiresAt:      now.Add(5 * time.Minute),
		IssuedAt:       now,
	})
}
