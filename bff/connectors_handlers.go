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

func (s *server) connectorsAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts, err := connectorRouteParts(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", "invalid connector path")
		return
	}
	companionMethod, companionPath, requiredScopes, ok := connectorRoute(r.Method, parts)
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

func connectorRoute(method string, parts []string) (string, string, []string, bool) {
	readScopes := []string{"companion:connectors:admin", "companion:connectors:execute", "companion:runtime:admin"}
	writeScopes := []string{"companion:connectors:admin", "companion:runtime:admin"}
	if len(parts) == 0 {
		switch method {
		case http.MethodGet:
			return http.MethodGet, "/v1/connectors", readScopes, true
		case http.MethodPost:
			return http.MethodPost, "/v1/connectors", writeScopes, true
		}
	}
	if len(parts) == 1 {
		switch {
		case method == http.MethodGet && parts[0] == "types":
			return http.MethodGet, "/v1/connectors/types", readScopes, true
		case method == http.MethodGet:
			return http.MethodGet, "/v1/connectors/" + url.PathEscape(parts[0]), readScopes, true
		case method == http.MethodPatch:
			return http.MethodPatch, "/v1/connectors/" + url.PathEscape(parts[0]), writeScopes, true
		case method == http.MethodDelete:
			return http.MethodDelete, "/v1/connectors/" + url.PathEscape(parts[0]), writeScopes, true
		}
	}
	if len(parts) == 2 {
		path := "/v1/connectors/" + url.PathEscape(parts[0])
		switch {
		case method == http.MethodGet && parts[1] == "executions":
			return http.MethodGet, path + "/executions", readScopes, true
		case method == http.MethodPost && (parts[1] == "archive" || parts[1] == "trash" || parts[1] == "restore" || parts[1] == "test" || parts[1] == "refresh"):
			return http.MethodPost, path + "/" + parts[1], writeScopes, true
		}
	}
	return "", "", nil, false
}

func connectorRouteParts(path string) ([]string, error) {
	path = strings.TrimPrefix(path, "/api/connectors")
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
			return nil, fmt.Errorf("empty connector path segment")
		}
		parts = append(parts, decoded)
	}
	return parts, nil
}
