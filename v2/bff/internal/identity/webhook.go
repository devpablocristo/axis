package identity

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

const (
	headerSvixID        = "svix-id"
	headerSvixTimestamp = "svix-timestamp"
	headerSvixSignature = "svix-signature"

	maxWebhookBodyBytes = 2 * 1024 * 1024
	maxClockSkew        = 5 * time.Minute
)

var sigV1Regexp = regexp.MustCompile(`v1,([A-Za-z0-9+/=_-]+)`)

type WebhookIdentityPort interface {
	Ensure(ctx context.Context, input identitydomain.EnsureInput) (identitydomain.User, error)
	FindByProviderUserID(ctx context.Context, provider, providerUserID string) (identitydomain.User, error)
	MarkDeletedByProviderUserID(ctx context.Context, provider, providerUserID string) error
}

type WebhookOrganizationAccessPort interface {
	EnsureProviderDefaultProduct(ctx context.Context, input productdomain.EnsureOrgInput, userID string) (productdomain.Product, error)
	OrgByProvider(ctx context.Context, provider, providerOrgID string) (productdomain.Org, error)
	DeactivateUserMemberships(ctx context.Context, userID string) error
	DeactivateOrgUserMemberships(ctx context.Context, orgID, userID string) error
}

type WebhookHandler struct {
	identity WebhookIdentityPort
	products WebhookOrganizationAccessPort
	secret   string
	now      func() time.Time
}

func NewWebhookHandler(identity WebhookIdentityPort, products WebhookOrganizationAccessPort, secret string) *WebhookHandler {
	return &WebhookHandler{
		identity: identity,
		products: products,
		secret:   strings.TrimSpace(secret),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (h *WebhookHandler) Routes(router gin.IRouter) {
	router.POST("/webhooks/clerk", h.Handle)
}

func (h *WebhookHandler) Handle(c *gin.Context) {
	if h.secret == "" {
		ginmw.WriteError(c, http.StatusServiceUnavailable, "webhook_not_configured", "clerk webhook secret is not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodyBytes))
	if err != nil {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_body", "invalid webhook body")
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if err := verifySvix(
		h.secret,
		c.GetHeader(headerSvixID),
		c.GetHeader(headerSvixTimestamp),
		c.GetHeader(headerSvixSignature),
		body,
		h.now,
	); err != nil {
		ginmw.WriteError(c, http.StatusUnauthorized, "invalid_signature", "invalid webhook signature")
		return
	}
	var event clerkEventEnvelope
	if err := json.Unmarshal(body, &event); err != nil {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_payload", "invalid webhook payload")
		return
	}
	if err := h.dispatch(c.Request.Context(), event); err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"ok": true})
}

func (h *WebhookHandler) dispatch(ctx context.Context, event clerkEventEnvelope) error {
	switch strings.TrimSpace(event.Type) {
	case "user.created", "user.updated":
		return h.onUserUpsert(ctx, event.Data)
	case "user.deleted":
		return h.onUserDeleted(ctx, event.Data)
	case "organization.created", "organization.updated":
		return h.onOrganizationUpsert(ctx, event.Data)
	case "organizationMembership.deleted", "organization_membership.deleted":
		return h.onOrganizationMembershipDeleted(ctx, event.Data)
	default:
		return nil
	}
}

func (h *WebhookHandler) onUserUpsert(ctx context.Context, raw json.RawMessage) error {
	var data clerkUserData
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	email := data.primaryEmail()
	if email == "" {
		return domainerr.Validation("webhook user has no email")
	}
	now := h.now()
	_, err := h.identity.Ensure(ctx, identitydomain.EnsureInput{
		Provider:       identitydomain.ProviderClerk,
		ProviderUserID: data.ID,
		Email:          email,
		Status:         identitydomain.StatusActive,
		SyncedAt:       &now,
	})
	return err
}

func (h *WebhookHandler) onUserDeleted(ctx context.Context, raw json.RawMessage) error {
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	data.ID = strings.TrimSpace(data.ID)
	if data.ID == "" {
		return nil
	}
	user, err := h.identity.FindByProviderUserID(ctx, identitydomain.ProviderClerk, data.ID)
	if err != nil && !domainerr.IsNotFound(err) {
		return err
	}
	if err := h.identity.MarkDeletedByProviderUserID(ctx, identitydomain.ProviderClerk, data.ID); err != nil {
		return err
	}
	if user.ID == "" {
		return nil
	}
	return h.products.DeactivateUserMemberships(ctx, user.ID)
}

func (h *WebhookHandler) onOrganizationUpsert(ctx context.Context, raw json.RawMessage) error {
	var data clerkOrganizationData
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	providerOrgID := strings.TrimSpace(data.ID)
	if providerOrgID == "" {
		return nil
	}
	now := h.now()
	_, err := h.products.EnsureProviderDefaultProduct(ctx, productdomain.EnsureOrgInput{
		Provider:      identitydomain.ProviderClerk,
		ProviderOrgID: providerOrgID,
		Name:          firstNonEmpty(data.Name, data.Slug, providerOrgID),
		Slug:          data.Slug,
		Status:        productdomain.StatusActive,
		SyncedAt:      &now,
	}, "")
	return err
}

func (h *WebhookHandler) onOrganizationMembershipDeleted(ctx context.Context, raw json.RawMessage) error {
	var data clerkMembershipData
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	providerOrgID := strings.TrimSpace(data.Organization.ID)
	providerUserID := firstNonEmpty(data.PublicUserData.UserID, data.User.ID)
	if providerOrgID == "" || providerUserID == "" {
		return nil
	}
	org, err := h.products.OrgByProvider(ctx, identitydomain.ProviderClerk, providerOrgID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return nil
		}
		return err
	}
	user, err := h.identity.FindByProviderUserID(ctx, identitydomain.ProviderClerk, providerUserID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return nil
		}
		return err
	}
	return h.products.DeactivateOrgUserMemberships(ctx, org.ID, user.ID)
}

type clerkEventEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type clerkEmailAddress struct {
	ID           string `json:"id"`
	EmailAddress string `json:"email_address"`
}

type clerkUserData struct {
	ID                    string              `json:"id"`
	PrimaryEmailAddressID string              `json:"primary_email_address_id"`
	EmailAddresses        []clerkEmailAddress `json:"email_addresses"`
}

func (u clerkUserData) primaryEmail() string {
	if u.PrimaryEmailAddressID != "" {
		for _, item := range u.EmailAddresses {
			if strings.TrimSpace(item.ID) == strings.TrimSpace(u.PrimaryEmailAddressID) {
				return strings.TrimSpace(strings.ToLower(item.EmailAddress))
			}
		}
	}
	for _, item := range u.EmailAddresses {
		if email := strings.TrimSpace(strings.ToLower(item.EmailAddress)); email != "" {
			return email
		}
	}
	return ""
}

type clerkOrganizationData struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type clerkMembershipData struct {
	Organization struct {
		ID string `json:"id"`
	} `json:"organization"`
	PublicUserData struct {
		UserID string `json:"user_id"`
	} `json:"public_user_data"`
	User clerkUserData `json:"user"`
}

func verifySvix(secret, id, ts, sigHeader string, payload []byte, now func() time.Time) error {
	secret = strings.TrimSpace(secret)
	id = strings.TrimSpace(id)
	ts = strings.TrimSpace(ts)
	sigHeader = strings.TrimSpace(sigHeader)
	if secret == "" || id == "" || ts == "" || sigHeader == "" {
		return errors.New("missing svix headers")
	}
	timestamp, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errors.New("invalid svix timestamp")
	}
	delta := now().UTC().Sub(time.Unix(timestamp, 0).UTC())
	if delta < 0 {
		delta = -delta
	}
	if delta > maxClockSkew {
		return errors.New("svix timestamp expired")
	}
	secretBytes, err := decodeSvixSecret(secret)
	if err != nil {
		return err
	}
	message := id + "." + ts + "." + string(payload)
	mac := hmac.New(sha256.New, secretBytes)
	_, _ = mac.Write([]byte(message))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	for _, candidate := range extractV1Signatures(sigHeader) {
		if hmac.Equal([]byte(candidate), []byte(expected)) {
			return nil
		}
	}
	return errors.New("signature mismatch")
}

func decodeSvixSecret(secret string) ([]byte, error) {
	secret = strings.TrimPrefix(strings.TrimSpace(secret), "whsec_")
	if secret == "" {
		return nil, errors.New("invalid svix secret")
	}
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(secret)
		if err == nil && len(decoded) > 0 {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid svix secret encoding")
}

func extractV1Signatures(raw string) []string {
	matches := sigV1Regexp.FindAllStringSubmatch(raw, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			out = append(out, strings.TrimSpace(match[1]))
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
