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
			orgs, err := s.iam.ListOrgsForActor(r.Context(), p.Actor, hasScope(p.Scopes, "axis:cross_org", "axis:orgs:admin"))
			writeStoreResult(w, map[string]any{"orgs": orgs}, err)
		case http.MethodPost:
			_ = s.ensureActorUser(r, p)
			input, ok := decodeJSONBody[IAMOrg](w, r)
			if !ok {
				return
			}
			org, err := s.iam.CreateOrg(r.Context(), input, p.Actor)
			writeStoreCreated(w, map[string]any{"org": org}, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPatch {
		input, ok := decodeJSONBody[IAMOrg](w, r)
		if !ok {
			return
		}
		org, err := s.iam.UpdateOrg(r.Context(), parts[1], input)
		writeStoreResult(w, map[string]any{"org": org}, err)
		return
	}
	if len(parts) == 3 && parts[2] == "members" {
		orgID := parts[1]
		switch r.Method {
		case http.MethodGet:
			if !s.canAccessOrg(r, p, orgID) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "selected org is not allowed for this principal")
				return
			}
			members, err := s.iam.ListMembers(r.Context(), orgID)
			writeStoreResult(w, map[string]any{"members": members}, err)
		case http.MethodPost:
			input, ok := decodeJSONBody[IAMMember](w, r)
			if !ok {
				return
			}
			input.OrgID = orgID
			member, err := s.iam.UpsertMember(r.Context(), input)
			writeStoreCreated(w, map[string]any{"member": member}, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 4 && parts[2] == "members" && r.Method == http.MethodPatch {
		input, ok := decodeJSONBody[IAMMember](w, r)
		if !ok {
			return
		}
		member, err := s.iam.UpdateMember(r.Context(), parts[1], parts[3], input)
		writeStoreResult(w, map[string]any{"member": member}, err)
		return
	}
	if len(parts) == 3 && parts[2] == "invitations" && r.Method == http.MethodPost {
		input, ok := decodeJSONBody[IAMInvitation](w, r)
		if !ok {
			return
		}
		input.OrgID = parts[1]
		input.InvitedBy = p.Actor
		input.Provider = firstNonEmpty(input.Provider, "clerk")
		invite, err := s.iam.CreateInvitation(r.Context(), input)
		writeStoreCreated(w, map[string]any{"invitation": invite}, err)
		return
	}
	http.NotFound(w, r)
}

func (s *server) users(w http.ResponseWriter, r *http.Request) {
	parts := routeParts(r.URL.Path)
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			users, err := s.iam.ListUsers(r.Context())
			writeStoreResult(w, map[string]any{"users": users}, err)
		case http.MethodPost:
			input, ok := decodeJSONBody[IAMUser](w, r)
			if !ok {
				return
			}
			user, err := s.iam.CreateUser(r.Context(), input)
			writeStoreCreated(w, map[string]any{"user": user}, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPatch {
		input, ok := decodeJSONBody[IAMUser](w, r)
		if !ok {
			return
		}
		user, err := s.iam.UpdateUser(r.Context(), parts[1], input)
		writeStoreResult(w, map[string]any{"user": user}, err)
		return
	}
	http.NotFound(w, r)
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
	invite, err := s.iam.UpdateInvitationStatus(r.Context(), parts[1], status, p.Actor)
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
			ID:            "axis_" + firstWebhookString(event.Data, "id"),
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

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
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
