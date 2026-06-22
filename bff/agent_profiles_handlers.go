package main

import (
	"io"
	"net/http"
	"net/url"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
)

func (s *server) agentProfilesAPI(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	if !requireScope(w, p, "companion:agent_profiles:read", "companion:agent_profiles:admin", "companion:runtime:admin") {
		return
	}
	orgID, err := s.selectedOrg(r, p)
	if err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
		return
	}
	target, err := url.Parse(s.cfg.CompanionBaseURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "COMPANION_URL_INVALID", err.Error())
		return
	}
	target.Path = "/v1/agent-profiles"
	target.RawQuery = r.URL.RawQuery
	token, err := s.signDownstreamToken(p, orgID, s.cfg.CompanionAudience)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_SIGNING_FAILED", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "REQUEST_BUILD_FAILED", err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Request-ID", requestID(r))
	req.Header.Set("X-Axis-Forwarded-By", "axis-bff")
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

func (s *server) signDownstreamToken(p authn.Principal, orgID string, audience string) (string, error) {
	now := time.Now().UTC()
	return signInternalJWT(s.cfg.InternalJWTSecret, internalJWTClaims{
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
}
