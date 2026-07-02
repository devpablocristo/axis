package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
		if _, err := s.iam.CreateUser(r.Context(), IAMUser{
			ID:             firstWebhookString(event.Data, "id"),
			ExternalID:     firstWebhookString(event.Data, "id"),
			Provider:       "clerk",
			ProviderUserID: firstWebhookString(event.Data, "id"),
			Email:          email,
			Name:           firstWebhookString(event.Data, "name", "username"),
			Status:         "active",
		}); err != nil {
			// Don't swallow: surface 5xx so Clerk retries the webhook.
			log.Printf("clerk webhook %q: create user failed: %v", event.Type, err)
			writeStoreError(w, err)
			return
		}
	case "organization.created", "organization.updated":
		if _, err := s.iam.CreateOrg(r.Context(), IAMOrg{
			ID:            firstWebhookString(event.Data, "id"),
			ExternalID:    firstWebhookString(event.Data, "id"),
			Provider:      "clerk",
			ProviderOrgID: firstWebhookString(event.Data, "id"),
			Name:          firstWebhookString(event.Data, "name"),
			Slug:          firstWebhookString(event.Data, "slug"),
			Status:        "active",
		}, ""); err != nil {
			log.Printf("clerk webhook %q: create org failed: %v", event.Type, err)
			writeStoreError(w, err)
			return
		}
	case "user.deleted":
		// Clerk (IdP) removed the user → drop the orphaned Axis row.
		if id := firstWebhookString(event.Data, "id"); id != "" {
			if err := s.iam.DeleteUser(r.Context(), id); err != nil {
				log.Printf("clerk webhook %q: delete user failed: %v", event.Type, err)
				writeStoreError(w, err)
				return
			}
		}
	case "organization.deleted":
		if id := firstWebhookString(event.Data, "id"); id != "" {
			if err := s.iam.DeleteOrg(r.Context(), id); err != nil {
				log.Printf("clerk webhook %q: delete org failed: %v", event.Type, err)
				writeStoreError(w, err)
				return
			}
		}
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

type orgAccessError struct {
	cause error
}

func (e *orgAccessError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return "org access lookup failed"
}

func (e *orgAccessError) Unwrap() error {
	return e.cause
}

func writeOrgAccessError(w http.ResponseWriter, err error, forbiddenMessage string) {
	var accessErr *orgAccessError
	if errors.As(err, &accessErr) {
		writeLoggedError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "request failed", err)
		return
	}
	writeError(w, http.StatusForbidden, "FORBIDDEN", forbiddenMessage)
}

func (s *server) canAccessOrg(r *http.Request, p authn.Principal, orgID string) (bool, error) {
	if hasScope(p.Scopes, "axis:cross_org", "axis:orgs:admin") {
		return true, nil
	}
	ok, err := s.iam.ActorCanAccessOrg(r.Context(), p.Actor, orgID)
	if err != nil {
		return false, &orgAccessError{cause: fmt.Errorf("org access lookup failed: %w", err)}
	}
	return ok, nil
}

func (s *server) requireOrgAccess(w http.ResponseWriter, r *http.Request, p authn.Principal, orgID string, forbiddenMessage string) bool {
	ok, err := s.canAccessOrg(r, p, orgID)
	if err != nil {
		writeOrgAccessError(w, err, forbiddenMessage)
		return false
	}
	if !ok {
		writeError(w, http.StatusForbidden, "FORBIDDEN", forbiddenMessage)
		return false
	}
	return true
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
	if errors.Is(err, errValidation) {
		writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if errors.Is(err, errIdentityProviderNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, "IDENTITY_PROVIDER_NOT_CONFIGURED", "identity provider is not configured")
		return
	}
	// Preserve an upstream (Clerk) 4xx instead of masking it as a generic 500.
	switch code := clerkStatus(err); {
	case code == http.StatusConflict:
		writeError(w, http.StatusConflict, "CONFLICT", err.Error())
		return
	case code == http.StatusUnprocessableEntity || code == http.StatusBadRequest:
		writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	case code >= 400 && code < 500:
		writeError(w, code, "UPSTREAM", err.Error())
		return
	}
	// Genuinely unexpected: log the cause (it's hidden from the client otherwise).
	log.Printf("iam: unexpected store error: %v", err)
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "request failed")
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
