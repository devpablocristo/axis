package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
)

const (
	clerkWebhookMaxBodySize     = 1 << 20
	clerkWebhookMaxClockDrift   = 5 * time.Minute
	clerkWebhookSecretPrefix    = "whsec_"
	clerkWebhookSignatureScheme = "v1"
	clerkWebhookHeaderID        = "svix-id"
	clerkWebhookHeaderTimestamp = "svix-timestamp"
	clerkWebhookHeaderSignature = "svix-signature"
)

var (
	errClerkWebhookMissingHeader     = errors.New("missing svix headers")
	errClerkWebhookBadTimestamp      = errors.New("invalid svix timestamp")
	errClerkWebhookStaleTimestamp    = errors.New("svix timestamp drift exceeds tolerance")
	errClerkWebhookSignatureMismatch = errors.New("svix signature mismatch")
)

func (s *server) orgs(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts := routeParts(r.URL.Path)
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			if !requireScope(w, p, "axis:orgs:read", "axis:orgs:admin", "axis:cross_org") {
				return
			}
			orgs, err := s.iam.ListOrgsForActor(r.Context(), p.Actor, hasScope(p.Scopes, "axis:cross_org", "axis:orgs:admin"))
			writeStoreResult(w, map[string]any{"orgs": orgs}, err)
		case http.MethodPost:
			if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin") {
				return
			}
			_ = s.ensureActorUser(r, p)
			input, ok := decodeJSONBody[IAMOrg](w, r)
			if !ok {
				return
			}
			org, err := s.createIAMOrg(r.Context(), p.Actor, input)
			if err == nil {
				s.auditIAM(r, p, org.ID, "org.created", "org", org.ID, map[string]any{"name": org.Name, "slug": org.Slug, "status": org.Status})
			}
			writeStoreCreated(w, map[string]any{"org": org}, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodPatch:
			if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin") {
				return
			}
			input, ok := decodeJSONBody[IAMOrg](w, r)
			if !ok {
				return
			}
			org, err := s.updateIAMOrg(r.Context(), parts[1], input)
			if err == nil {
				s.auditIAM(r, p, parts[1], "org.updated", "org", parts[1], map[string]any{"name": input.Name, "slug": input.Slug, "status": input.Status})
			}
			writeStoreResult(w, map[string]any{"org": org}, err)
		case http.MethodDelete:
			if !requireScope(w, p, "axis:orgs:admin") {
				return
			}
			err := s.deleteIAMOrg(r.Context(), parts[1])
			if err == nil {
				s.auditIAM(r, p, parts[1], "org.purged", "org", parts[1], nil)
			}
			writeStoreNoContent(w, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 3 && parts[2] == "members" {
		orgID := parts[1]
		switch r.Method {
		case http.MethodGet:
			if !requireScope(w, p, "axis:orgs:read", "axis:orgs:admin") {
				return
			}
			if !s.canAccessOrg(r, p, orgID) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
				return
			}
			members, err := s.iam.ListMembers(r.Context(), orgID)
			writeStoreResult(w, map[string]any{"members": members}, err)
		case http.MethodPost:
			if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin", "axis:users:admin") {
				return
			}
			input, ok := decodeJSONBody[IAMMember](w, r)
			if !ok {
				return
			}
			input.OrgID = orgID
			member, err := s.upsertIAMMember(r.Context(), input)
			if err == nil {
				s.auditIAM(r, p, orgID, "member.upserted", "member", input.UserID, map[string]any{"role": input.Role, "status": input.Status})
			}
			writeStoreCreated(w, map[string]any{"member": member}, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 4 && parts[2] == "members" {
		switch r.Method {
		case http.MethodPatch:
			if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin", "axis:users:admin") {
				return
			}
			input, ok := decodeJSONBody[IAMMember](w, r)
			if !ok {
				return
			}
			member, err := s.updateIAMMember(r.Context(), parts[1], parts[3], input)
			if err == nil {
				s.auditIAM(r, p, parts[1], "member.updated", "member", parts[3], map[string]any{"role": input.Role, "status": input.Status})
			}
			writeStoreResult(w, map[string]any{"member": member}, err)
		case http.MethodDelete:
			if !requireScope(w, p, "axis:orgs:admin", "axis:users:admin") {
				return
			}
			err := s.deleteIAMMember(r.Context(), parts[1], parts[3])
			if err == nil {
				s.auditIAM(r, p, parts[1], "member.purged", "member", parts[3], nil)
			}
			writeStoreNoContent(w, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 3 && parts[2] == "invitations" {
		switch r.Method {
		case http.MethodGet:
			if !requireScope(w, p, "axis:orgs:read", "axis:orgs:admin") {
				return
			}
			if !s.canAccessOrg(r, p, parts[1]) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
				return
			}
			invitations, err := s.iam.ListInvitations(r.Context(), parts[1])
			writeStoreResult(w, map[string]any{"invitations": invitations}, err)
		case http.MethodPost:
			if !requireScope(w, p, "axis:orgs:write", "axis:orgs:admin", "axis:users:admin") {
				return
			}
			input, ok := decodeJSONBody[IAMInvitation](w, r)
			if !ok {
				return
			}
			input.OrgID = parts[1]
			input.InvitedBy = p.Actor
			input.Provider = firstNonEmpty(input.Provider, "identity")
			invite, err := s.createIAMInvitation(r.Context(), input)
			if err == nil {
				s.auditIAM(r, p, parts[1], "invitation.created", "invitation", invite.ID, map[string]any{"email": invite.Email, "role": invite.Role, "status": invite.Status})
			}
			writeStoreCreated(w, map[string]any{"invitation": invite}, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	http.NotFound(w, r)
}

func (s *server) users(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts := routeParts(r.URL.Path)
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			if !requireScope(w, p, "axis:users:read", "axis:users:admin") {
				return
			}
			users, err := s.listVisibleUsers(r, p)
			writeStoreResult(w, map[string]any{"users": users}, err)
		case http.MethodPost:
			if !requireScope(w, p, "axis:users:write", "axis:users:admin") {
				return
			}
			orgID, err := s.selectedOrg(r, p)
			if err != nil {
				writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
				return
			}
			input, ok := decodeJSONBody[IAMUser](w, r)
			if !ok {
				return
			}
			user, err := s.createIAMUser(r.Context(), orgID, input)
			if err == nil {
				s.auditIAM(r, p, "", "user.created", "user", user.ID, map[string]any{"email": user.Email, "status": user.Status})
			}
			writeStoreCreated(w, map[string]any{"user": user}, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodPatch:
			if !requireScope(w, p, "axis:users:write", "axis:users:admin") {
				return
			}
			orgID, err := s.selectedOrg(r, p)
			if err != nil {
				writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
				return
			}
			input, ok := decodeJSONBody[IAMUser](w, r)
			if !ok {
				return
			}
			user, err := s.updateIAMUser(r.Context(), orgID, parts[1], input)
			if err == nil {
				s.auditIAM(r, p, "", "user.updated", "user", parts[1], map[string]any{"email": input.Email, "name": input.Name, "status": input.Status})
			}
			writeStoreResult(w, map[string]any{"user": user}, err)
		case http.MethodDelete:
			if !requireScope(w, p, "axis:users:admin") {
				return
			}
			orgID, selectedErr := s.selectedOrg(r, p)
			if selectedErr != nil {
				writeError(w, http.StatusForbidden, "FORBIDDEN", selectedErr.Error())
				return
			}
			err := s.deleteIAMUser(r.Context(), orgID, parts[1])
			if err == nil {
				s.auditIAM(r, p, "", "user.purged", "user", parts[1], nil)
			}
			writeStoreNoContent(w, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	http.NotFound(w, r)
}

func (s *server) iamAudit(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireScope(w, p, "axis:orgs:read", "axis:orgs:admin", "axis:users:admin") {
		return
	}
	orgID := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if orgID != "" && !s.canAccessOrg(r, p, orgID) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
		return
	}
	events, err := s.iam.ListAuditEvents(r.Context(), orgID)
	writeStoreResult(w, map[string]any{"events": events}, err)
}

func (s *server) orgInvitations(w http.ResponseWriter, r *http.Request) {
	p := principalFromContext(r.Context())
	parts := routeParts(r.URL.Path)
	if len(parts) != 3 || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	status := ""
	switch parts[2] {
	case "accept":
		status = "accepted"
	case "revoke":
		status = "revoked"
	case "resend":
		status = "pending"
	default:
		http.NotFound(w, r)
		return
	}
	invite, err := s.updateIAMInvitationStatus(r.Context(), parts[1], status, p.Actor)
	if err == nil {
		s.auditIAM(r, p, invite.OrgID, "invitation."+status, "invitation", invite.ID, map[string]any{"email": invite.Email, "status": invite.Status})
	}
	writeStoreResult(w, map[string]any{"invitation": invite}, err)
}

func (s *server) clerkWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, clerkWebhookMaxBodySize+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "read body failed")
		return
	}
	if len(body) > clerkWebhookMaxBodySize {
		writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "payload too large")
		return
	}
	secret := strings.TrimSpace(s.cfg.ClerkWebhookSecret)
	if secret != "" {
		if err := verifyClerkWebhookSignature(
			secret,
			strings.TrimSpace(r.Header.Get(clerkWebhookHeaderID)),
			strings.TrimSpace(r.Header.Get(clerkWebhookHeaderTimestamp)),
			strings.TrimSpace(r.Header.Get(clerkWebhookHeaderSignature)),
			body,
			time.Now(),
		); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "signature verification failed")
			return
		}
	} else if s.cfg.AuthMode == "clerk" {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOK_NOT_CONFIGURED", "clerk webhook secret is not configured")
		return
	}

	var event struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if s.identity != nil {
		if err := s.identity.HandleWebhook(r.Context(), event.Type, event.Data); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
		return
	}
	switch strings.TrimSpace(event.Type) {
	case "user.created", "user.updated":
		email := firstWebhookString(event.Data, "email", "email_address")
		if email == "" {
			email = firstClerkEmail(event.Data)
		}
		_, _ = s.iam.CreateUser(r.Context(), IAMUser{
			ID:             firstWebhookString(event.Data, "id"),
			ExternalID:     firstWebhookString(event.Data, "id"),
			Provider:       "clerk",
			ProviderUserID: firstWebhookString(event.Data, "id"),
			Email:          email,
			Name:           firstWebhookString(event.Data, "name", "username"),
			Status:         "active",
		})
	case "organization.created", "organization.updated":
		_, _ = s.iam.CreateOrg(r.Context(), IAMOrg{
			ID:            firstWebhookString(event.Data, "id"),
			ExternalID:    firstWebhookString(event.Data, "id"),
			Provider:      "clerk",
			ProviderOrgID: firstWebhookString(event.Data, "id"),
			Name:          firstWebhookString(event.Data, "name"),
			Slug:          firstWebhookString(event.Data, "slug"),
			Status:        "active",
		}, "")
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

func verifyClerkWebhookSignature(secret, msgID, timestamp, signatureHeader string, body []byte, now time.Time) error {
	if msgID == "" || timestamp == "" || signatureHeader == "" {
		return errClerkWebhookMissingHeader
	}
	tsSec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errClerkWebhookBadTimestamp
	}
	ts := time.Unix(tsSec, 0)
	if drift := now.Sub(ts); drift > clerkWebhookMaxClockDrift || drift < -clerkWebhookMaxClockDrift {
		return errClerkWebhookStaleTimestamp
	}
	rawSecret := strings.TrimPrefix(strings.TrimSpace(secret), clerkWebhookSecretPrefix)
	keyBytes, err := base64.StdEncoding.DecodeString(rawSecret)
	if err != nil {
		return fmt.Errorf("decode webhook secret: %w", err)
	}
	mac := hmac.New(sha256.New, keyBytes)
	if _, err := fmt.Fprintf(mac, "%s.%s.", msgID, timestamp); err != nil {
		return err
	}
	if _, err := mac.Write(body); err != nil {
		return err
	}
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	for _, candidate := range strings.Fields(signatureHeader) {
		parts := strings.SplitN(candidate, ",", 2)
		if len(parts) == 2 && parts[0] == clerkWebhookSignatureScheme && hmac.Equal([]byte(parts[1]), []byte(expected)) {
			return nil
		}
	}
	return errClerkWebhookSignatureMismatch
}

func (s *server) canAccessOrg(r *http.Request, p authn.Principal, orgID string) bool {
	if hasScope(p.Scopes, "axis:cross_org", "axis:orgs:admin") {
		return true
	}
	ok, err := s.iam.ActorCanAccessOrg(r.Context(), p.Actor, orgID)
	return err == nil && ok
}

func requireScope(w http.ResponseWriter, p authn.Principal, scopes ...string) bool {
	if hasScope(p.Scopes, scopes...) {
		return true
	}
	writeError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func (s *server) auditIAM(r *http.Request, p authn.Principal, orgID string, action string, target string, targetID string, payload map[string]any) {
	if action == "" || target == "" {
		return
	}
	event := IAMAuditEvent{
		OrgID:    orgID,
		Actor:    p.Actor,
		Action:   action,
		Target:   target,
		TargetID: targetID,
		Payload:  compactPayload(payload),
	}
	_ = s.iam.AppendAuditEvent(r.Context(), event)
}

func compactPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				out[key] = typed
			}
		default:
			if value != nil {
				out[key] = value
			}
		}
	}
	return out
}

func (s *server) ensureActorUser(r *http.Request, p authn.Principal) error {
	if strings.TrimSpace(p.Actor) == "" {
		return nil
	}
	_, err := s.iam.CreateUser(r.Context(), IAMUser{
		ID:             p.Actor,
		ExternalID:     p.Actor,
		Provider:       p.AuthMethod,
		ProviderUserID: p.Actor,
		Email:          p.Actor,
		Name:           p.Actor,
		Status:         "active",
	})
	return err
}

func writeStoreResult(w http.ResponseWriter, payload any, err error) {
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeStoreCreated(w http.ResponseWriter, payload any, err error) {
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, payload)
}

func writeStoreNoContent(w http.ResponseWriter, err error) {
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	if errors.Is(err, errIdentityProviderNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, "IDENTITY_PROVIDER_NOT_CONFIGURED", "identity provider is not configured")
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "request failed")
}

func routeParts(path string) []string {
	path = strings.TrimPrefix(path, "/api/")
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstWebhookString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := normalizeClaimString(data[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstClerkEmail(data map[string]any) string {
	raw, ok := data["email_addresses"].([]any)
	if !ok {
		return ""
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if email := normalizeClaimString(entry["email_address"]); email != "" {
			return email
		}
	}
	return ""
}
