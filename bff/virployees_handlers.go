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

func (s *server) virployeesDomainAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts, err := virployeeRouteParts(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid virployee path")
		return
	}
	companionMethod, companionPath, requiredScopes, bodyOverride, ok := virployeeRoute(r.Method, parts)
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
	if bodyOverride != "" {
		body = strings.NewReader(bodyOverride)
	} else if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPatch) {
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

func virployeeRoute(method string, parts []string) (string, string, []string, string, bool) {
	readScopes := []string{"axis:virployees:read", "axis:virployees:admin", "companion:virployees:read", "companion:virployees:admin"}
	writeScopes := []string{"axis:virployees:write", "axis:virployees:admin", "companion:virployees:write", "companion:virployees:admin"}
	if len(parts) == 0 {
		switch method {
		case http.MethodGet:
			return http.MethodGet, "/v1/virployees", readScopes, "", true
		case http.MethodPost:
			return http.MethodPost, "/v1/virployees", writeScopes, "", true
		}
	}
	if len(parts) == 1 {
		virployeePath := "/v1/virployees/" + url.PathEscape(parts[0])
		switch method {
		case http.MethodGet:
			return http.MethodGet, virployeePath, readScopes, "", true
		case http.MethodPatch:
			return http.MethodPatch, virployeePath, writeScopes, "", true
		}
	}
	if len(parts) == 2 {
		virployeePath := "/v1/virployees/" + url.PathEscape(parts[0])
		switch {
		case method == http.MethodPost && parts[1] == "status":
			return http.MethodPost, virployeePath + "/status", writeScopes, "", true
		case method == http.MethodPost && parts[1] == "archive":
			return http.MethodPost, virployeePath + "/status", writeScopes, `{"status":"archived"}`, true
		case method == http.MethodPost && parts[1] == "restore":
			return http.MethodPost, virployeePath + "/status", writeScopes, `{"status":"active"}`, true
		case method == http.MethodPost && parts[1] == "trash":
			return http.MethodPost, virployeePath + "/status", writeScopes, `{"status":"trashed"}`, true
		}
	}
	return "", "", nil, "", false
}

func virployeeRouteParts(path string) ([]string, error) {
	path = strings.TrimPrefix(path, "/api/virployees")
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
			return nil, fmt.Errorf("empty virployee path segment")
		}
		parts = append(parts, decoded)
	}
	return parts, nil
}
