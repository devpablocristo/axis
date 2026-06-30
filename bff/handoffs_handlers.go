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

func (s *server) handoffsAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts, err := handoffRouteParts(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid handoff path")
		return
	}
	companionMethod, companionPath, requiredScopes, ok := handoffRoute(r.Method, parts)
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
	if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPatch) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", "invalid request body")
			return
		}
		body = bytes.NewReader(raw)
	}
	s.forwardCompanionRequest(w, r, p, orgID, productSurface, tenantID, scopes, companionMethod, companionPath, body)
}

func (s *server) auditEventsAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	orgID, productSurface, tenantID, scopes, err := s.resolveAppContext(r, p)
	if err != nil {
		writeAppContextError(w, err)
		return
	}
	readScopes := []string{"axis:virtual_employees:read", "axis:virtual_employees:admin", "axis:agents:read", "axis:agents:admin", "companion:audit:read", "companion:runtime:admin"}
	if !requireScope(w, authn.Principal{Scopes: scopes}, readScopes...) {
		return
	}
	s.forwardCompanionRequest(w, r, p, orgID, productSurface, tenantID, scopes, http.MethodGet, "/v1/audit-events", nil)
}

func (s *server) forwardCompanionRequest(w http.ResponseWriter, r *http.Request, p authn.Principal, orgID string, productSurface string, tenantID string, scopes []string, method string, companionPath string, body io.Reader) {
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
	req.Header.Set("Content-Type", firstNonEmpty(r.Header.Get("Content-Type"), "application/json"))
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

func handoffRoute(method string, parts []string) (string, string, []string, bool) {
	readScopes := []string{"axis:virtual_employees:read", "axis:virtual_employees:admin", "axis:agents:read", "axis:agents:admin"}
	writeScopes := []string{"axis:virtual_employees:write", "axis:virtual_employees:admin", "axis:agents:write", "axis:agents:admin"}
	if len(parts) == 0 {
		switch method {
		case http.MethodGet:
			return http.MethodGet, "/v1/handoffs", readScopes, true
		case http.MethodPost:
			return http.MethodPost, "/v1/handoffs", writeScopes, true
		}
	}
	if len(parts) == 1 {
		path := "/v1/handoffs/" + url.PathEscape(parts[0])
		switch method {
		case http.MethodGet:
			return http.MethodGet, path, readScopes, true
		case http.MethodPatch:
			return http.MethodPatch, path, writeScopes, true
		}
	}
	return "", "", nil, false
}

func handoffRouteParts(path string) ([]string, error) {
	path = strings.TrimPrefix(path, "/api/handoffs")
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
			return nil, fmt.Errorf("empty handoff path segment")
		}
		parts = append(parts, decoded)
	}
	return parts, nil
}
