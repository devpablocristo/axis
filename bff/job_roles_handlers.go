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

func (s *server) jobRolesAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts, err := jobRoleRouteParts(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid job role path")
		return
	}
	companionMethod, companionPath, requiredScopes, ok := jobRoleRoute(r.Method, parts)
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
	if r.Body != nil && (r.Method == http.MethodPut || r.Method == http.MethodPost) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", "invalid request body")
			return
		}
		body = bytes.NewReader(raw)
	}
	s.forwardJobRoleRequest(w, r, p, orgID, productSurface, tenantID, scopes, companionMethod, companionPath, body)
}

func (s *server) forwardJobRoleRequest(w http.ResponseWriter, r *http.Request, p authn.Principal, orgID string, productSurface string, tenantID string, scopes []string, method string, companionPath string, body io.Reader) {
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

func jobRoleRoute(method string, parts []string) (string, string, []string, bool) {
	readScopes := []string{"companion:agents:read", "companion:agents:admin", "companion:runtime:admin"}
	writeScopes := []string{"companion:agents:admin", "companion:runtime:admin"}
	if len(parts) == 0 && method == http.MethodGet {
		return http.MethodGet, "/v1/job-roles", readScopes, true
	}
	if len(parts) == 1 {
		rolePath := "/v1/job-roles/" + url.PathEscape(parts[0])
		switch method {
		case http.MethodGet:
			return http.MethodGet, rolePath, readScopes, true
		case http.MethodPut:
			return http.MethodPut, rolePath, writeScopes, true
		}
	}
	if len(parts) == 2 {
		rolePath := "/v1/job-roles/" + url.PathEscape(parts[0])
		switch {
		case method == http.MethodGet && parts[1] == "versions":
			return http.MethodGet, rolePath + "/versions", readScopes, true
		case method == http.MethodPost && (parts[1] == "archive" || parts[1] == "restore"):
			return http.MethodPost, rolePath + "/" + parts[1], writeScopes, true
		}
	}
	return "", "", nil, false
}

func jobRoleRouteParts(path string) ([]string, error) {
	path = strings.TrimPrefix(path, "/api/job-roles")
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
			return nil, fmt.Errorf("empty job role path segment")
		}
		parts = append(parts, decoded)
	}
	return parts, nil
}
