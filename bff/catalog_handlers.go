package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	authn "github.com/devpablocristo/platform/authn/go"
)

func (s *server) capabilitiesAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts, err := catalogRouteParts(r.URL.Path, "/api/capabilities", "capability")
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid capability path")
		return
	}
	companionMethod, companionPath, requiredScopes, ok := capabilityRoute(r.Method, parts)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.forwardCatalogRequest(w, r, p, companionMethod, companionPath, requiredScopes)
}

func (s *server) toolsAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts, err := catalogRouteParts(r.URL.Path, "/api/tools", "tool")
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid tool path")
		return
	}
	companionMethod, companionPath, requiredScopes, ok := toolRoute(r.Method, parts)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.forwardCatalogRequest(w, r, p, companionMethod, companionPath, requiredScopes)
}

func (s *server) forwardCatalogRequest(w http.ResponseWriter, r *http.Request, p authn.Principal, method string, companionPath string, requiredScopes []string) {
	orgID, productSurface, tenantID, scopes, err := s.resolveAppContext(r, p)
	if err != nil {
		writeAppContextError(w, err)
		return
	}
	if !requireScope(w, authn.Principal{Scopes: scopes}, requiredScopes...) {
		return
	}
	var body io.Reader
	if r.Body != nil && r.Method == http.MethodPost {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", "invalid request body")
			return
		}
		body = bytes.NewReader(raw)
	}
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

func capabilityRoute(method string, parts []string) (string, string, []string, bool) {
	readScopes := []string{"companion:capabilities:read", "companion:capabilities:admin", "companion:runtime:admin"}
	writeScopes := []string{"companion:capabilities:admin", "companion:runtime:admin"}
	if len(parts) == 0 && method == http.MethodGet {
		return http.MethodGet, "/v1/capabilities", readScopes, true
	}
	if len(parts) == 1 {
		switch {
		case method == http.MethodGet:
			return http.MethodGet, "/v1/capabilities/" + url.PathEscape(parts[0]), readScopes, true
		case method == http.MethodPost && parts[0] == "import-source":
			return http.MethodPost, "/v1/capabilities/import-source", writeScopes, true
		}
	}
	if len(parts) == 2 && method == http.MethodPost && parts[1] == "status" {
		return http.MethodPost, "/v1/capabilities/" + url.PathEscape(parts[0]) + "/status", writeScopes, true
	}
	return "", "", nil, false
}

func toolRoute(method string, parts []string) (string, string, []string, bool) {
	readScopes := []string{"companion:capabilities:read", "companion:capabilities:admin", "companion:runtime:admin"}
	writeScopes := []string{"companion:capabilities:admin", "companion:runtime:admin"}
	if len(parts) == 0 && method == http.MethodGet {
		return http.MethodGet, "/v1/tools", readScopes, true
	}
	if len(parts) == 1 && method == http.MethodGet {
		return http.MethodGet, "/v1/tools/" + url.PathEscape(parts[0]), readScopes, true
	}
	if len(parts) == 2 && method == http.MethodPost && parts[1] == "status" {
		return http.MethodPost, "/v1/tools/" + url.PathEscape(parts[0]) + "/status", writeScopes, true
	}
	return "", "", nil, false
}

func catalogRouteParts(path string, prefix string, resource string) ([]string, error) {
	path = strings.TrimPrefix(path, prefix)
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
