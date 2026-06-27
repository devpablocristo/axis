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

func (s *server) promptsAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	route, err := promptRouteFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if !route.ok {
		http.NotFound(w, r)
		return
	}
	if route.forbiddenMessage != "" {
		writeError(w, http.StatusForbidden, "FORBIDDEN", route.forbiddenMessage)
		return
	}
	orgID, productSurface, tenantID, scopes, err := s.resolveAppContext(r, p)
	if err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
		return
	}
	if !requireScope(w, authn.Principal{Scopes: scopes}, route.requiredScopes...) {
		return
	}
	var body io.Reader
	if route.forwardBody {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", "invalid request body")
			return
		}
		body = bytes.NewReader(raw)
	}
	s.forwardPromptRequest(w, r, p, orgID, productSurface, tenantID, scopes, route.method, route.path, route.rawQuery, body)
}

type promptRoute struct {
	method           string
	path             string
	rawQuery         string
	requiredScopes   []string
	forwardBody      bool
	forbiddenMessage string
	ok               bool
}

func promptRouteFromRequest(r *http.Request) (promptRoute, error) {
	parts, err := promptRouteParts(r.URL.Path)
	if err != nil {
		return promptRoute{}, err
	}
	readAssist := []string{"companion:assist:read", "companion:runtime:admin", "axis:products:admin"}
	readProfiles := []string{"companion:agent_profiles:read", "companion:agent_profiles:admin", "companion:runtime:admin", "axis:agents:admin"}
	writeProfiles := []string{"companion:agent_profiles:admin", "companion:runtime:admin", "axis:agents:admin"}
	if len(parts) == 1 && parts[0] == "assist-packs" && r.Method == http.MethodGet {
		query := cleanPromptQuery(r.URL.Query(), "lifecycle", "view")
		path := "/v1/assist-packs"
		if lifecycleView(r.URL.Query()) == "archived" {
			path = "/v1/assist-packs/archived"
		}
		return promptRoute{method: http.MethodGet, path: path, rawQuery: query.Encode(), requiredScopes: readAssist, ok: true}, nil
	}
	if len(parts) == 3 && parts[0] == "assist-packs" && parts[2] == "content" && r.Method == http.MethodPut {
		return promptRoute{
			forbiddenMessage: "assist pack prompts must be loaded from the owner product",
			ok:               true,
		}, nil
	}
	if len(parts) == 3 && parts[0] == "assist-packs" && (parts[2] == "archive" || parts[2] == "restore") && r.Method == http.MethodPost {
		return promptRoute{
			forbiddenMessage: "assist pack prompts are managed from the owner product",
			ok:               true,
		}, nil
	}
	if len(parts) == 1 && parts[0] == "agent-profiles" && r.Method == http.MethodGet {
		query := r.URL.Query()
		return promptRoute{method: http.MethodGet, path: "/v1/agent-profiles", rawQuery: query.Encode(), requiredScopes: readProfiles, ok: true}, nil
	}
	if len(parts) == 3 && parts[0] == "agent-profiles" && parts[2] == "system-prompt" && r.Method == http.MethodPut {
		return promptRoute{
			method:         http.MethodPut,
			path:           "/v1/agent-profiles/" + url.PathEscape(parts[1]),
			requiredScopes: writeProfiles,
			forwardBody:    true,
			ok:             true,
		}, nil
	}
	if len(parts) == 3 && parts[0] == "agent-profiles" && (parts[2] == "archive" || parts[2] == "restore") && r.Method == http.MethodPost {
		return promptRoute{
			method:         http.MethodPost,
			path:           "/v1/agent-profiles/" + url.PathEscape(parts[1]) + "/" + parts[2],
			requiredScopes: writeProfiles,
			ok:             true,
		}, nil
	}
	if len(parts) == 3 && parts[0] == "agent-profiles" && parts[2] == "trash" && r.Method == http.MethodPost {
		return promptRoute{
			method:         http.MethodPost,
			path:           "/v1/agent-profiles/" + url.PathEscape(parts[1]) + "/trash",
			requiredScopes: writeProfiles,
			ok:             true,
		}, nil
	}
	if len(parts) == 3 && parts[0] == "agent-profiles" && parts[2] == "purge" && r.Method == http.MethodDelete {
		return promptRoute{
			method:         http.MethodDelete,
			path:           "/v1/agent-profiles/" + url.PathEscape(parts[1]) + "/purge",
			requiredScopes: writeProfiles,
			ok:             true,
		}, nil
	}
	return promptRoute{}, nil
}

func (s *server) forwardPromptRequest(w http.ResponseWriter, r *http.Request, p authn.Principal, orgID string, productSurface string, tenantID string, scopes []string, method string, companionPath string, rawQuery string, body io.Reader) {
	target, err := url.Parse(s.cfg.CompanionBaseURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "COMPANION_URL_INVALID", err.Error())
		return
	}
	target.Path = companionPath
	target.RawQuery = rawQuery
	token, err := s.signDownstreamTokenForContext(p, orgID, productSurface, tenantID, scopes, s.cfg.CompanionAudience)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_SIGNING_FAILED", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), method, target.String(), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "REQUEST_BUILD_FAILED", err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Product-Surface", productSurface)
	req.Header.Set("X-Tenant-ID", tenantID)
	req.Header.Set("X-Request-ID", requestID(r))
	req.Header.Set("X-Axis-Forwarded-By", "axis-bff")
	if r.Header.Get("Content-Type") != "" {
		req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	}
	if productSurface := strings.TrimSpace(r.Header.Get("X-Product-Surface")); productSurface != "" {
		req.Header.Set("X-Product-Surface", productSurface)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "DOWNSTREAM_UNAVAILABLE", err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", firstNonEmpty(resp.Header.Get("Content-Type"), "application/json"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func promptRouteParts(path string) ([]string, error) {
	path = strings.TrimPrefix(path, "/api/prompts")
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
			return nil, fmt.Errorf("empty prompt path segment")
		}
		parts = append(parts, decoded)
	}
	return parts, nil
}

func cleanPromptQuery(query url.Values, keys ...string) url.Values {
	clean := url.Values{}
	for key, values := range query {
		skip := false
		for _, remove := range keys {
			if key == remove {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, value := range values {
			clean.Add(key, value)
		}
	}
	return clean
}

func lifecycleView(query url.Values) string {
	if value := strings.TrimSpace(query.Get("lifecycle")); value != "" {
		return strings.ToLower(value)
	}
	return strings.ToLower(strings.TrimSpace(query.Get("view")))
}
