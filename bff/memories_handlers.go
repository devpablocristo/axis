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

func (s *server) memoriesAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts, err := memoryRouteParts(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid memory path")
		return
	}
	companionMethod, companionPath, requiredScopes, ok := memoryRoute(r.Method, parts)
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
	req, err := http.NewRequestWithContext(r.Context(), companionMethod, target.String(), body)
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

func memoryRoute(method string, parts []string) (string, string, []string, bool) {
	readScopes := []string{"companion:memory:read", "companion:memory:admin", "companion:runtime:admin"}
	writeScopes := []string{"companion:memory:write", "companion:memory:admin", "companion:runtime:admin"}
	if len(parts) == 0 {
		switch method {
		case http.MethodGet:
			return http.MethodGet, "/v1/memories", readScopes, true
		case http.MethodPost:
			return http.MethodPost, "/v1/memories", writeScopes, true
		}
	}
	if len(parts) == 1 {
		memoryPath := "/v1/memories/" + url.PathEscape(parts[0])
		switch method {
		case http.MethodGet:
			return http.MethodGet, memoryPath, readScopes, true
		case http.MethodPatch:
			return http.MethodPatch, memoryPath, writeScopes, true
		}
	}
	if len(parts) == 2 {
		memoryPath := "/v1/memories/" + url.PathEscape(parts[0])
		switch {
		case method == http.MethodPost && parts[1] == "status":
			return http.MethodPost, memoryPath + "/status", writeScopes, true
		case method == http.MethodGet && parts[1] == "entries":
			return http.MethodGet, memoryPath + "/entries", readScopes, true
		case method == http.MethodPost && parts[1] == "entries":
			return http.MethodPost, memoryPath + "/entries", writeScopes, true
		}
	}
	return "", "", nil, false
}

func memoryRouteParts(path string) ([]string, error) {
	path = strings.TrimPrefix(path, "/api/memories")
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
			return nil, fmt.Errorf("empty memory path segment")
		}
		parts = append(parts, decoded)
	}
	return parts, nil
}
