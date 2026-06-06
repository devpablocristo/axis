package products

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	ProductStatusActive   = "active"
	ProductStatusDisabled = "disabled"

	AuthModeNone        = "none"
	AuthModeAPIKeyRef   = "api_key_ref"
	AuthModeOAuth2      = "oauth2"
	AuthModeInternalJWT = "internal_jwt"
	AuthModeCustom      = "custom"
)

var (
	ErrProductNotFound       = errors.New("product not found")
	ErrInstallationNotFound  = errors.New("product installation not found")
	ErrInstallationDisabled  = errors.New("product installation disabled")
	ErrInstallationRequired  = errors.New("active product installation required")
	ErrProductDisabled       = errors.New("product disabled")
	ErrValidation            = errors.New("product registry validation failed")
	productSurfaceExpression = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type Product struct {
	ProductSurface string         `json:"product_surface"`
	DisplayName    string         `json:"display_name"`
	Status         string         `json:"status"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedBy      string         `json:"created_by,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
}

type Installation struct {
	ID               string         `json:"id,omitempty"`
	OrgID            string         `json:"org_id"`
	ProductSurface   string         `json:"product_surface"`
	ExternalTenantID string         `json:"external_tenant_id,omitempty"`
	BaseURL          string         `json:"base_url,omitempty"`
	AuthMode         string         `json:"auth_mode"`
	SecretRef        string         `json:"secret_ref,omitempty"`
	Enabled          bool           `json:"enabled"`
	Config           map[string]any `json:"config,omitempty"`
	CreatedBy        string         `json:"created_by,omitempty"`
	CreatedAt        time.Time      `json:"created_at,omitempty"`
	UpdatedAt        time.Time      `json:"updated_at,omitempty"`
}

func normalizeProduct(product Product) Product {
	product.ProductSurface = normalizeProductSurface(product.ProductSurface)
	product.DisplayName = strings.TrimSpace(product.DisplayName)
	if product.DisplayName == "" {
		product.DisplayName = product.ProductSurface
	}
	product.Status = strings.TrimSpace(strings.ToLower(product.Status))
	if product.Status == "" {
		product.Status = ProductStatusActive
	}
	product.CreatedBy = strings.TrimSpace(product.CreatedBy)
	if product.Metadata == nil {
		product.Metadata = map[string]any{}
	}
	return product
}

func normalizeInstallation(installation Installation) Installation {
	installation.ID = strings.TrimSpace(installation.ID)
	installation.OrgID = strings.TrimSpace(installation.OrgID)
	installation.ProductSurface = normalizeProductSurface(installation.ProductSurface)
	installation.ExternalTenantID = strings.TrimSpace(installation.ExternalTenantID)
	installation.BaseURL = strings.TrimRight(strings.TrimSpace(installation.BaseURL), "/")
	installation.AuthMode = strings.TrimSpace(strings.ToLower(installation.AuthMode))
	if installation.AuthMode == "" {
		installation.AuthMode = AuthModeNone
	}
	installation.SecretRef = strings.TrimSpace(installation.SecretRef)
	installation.CreatedBy = strings.TrimSpace(installation.CreatedBy)
	if installation.Config == nil {
		installation.Config = map[string]any{}
	}
	return installation
}

func normalizeProductSurface(surface string) string {
	return strings.TrimSpace(strings.ToLower(surface))
}

func validateProduct(product Product) error {
	if !validProductSurface(product.ProductSurface) {
		return fmt.Errorf("%w: product_surface must match %s", ErrValidation, productSurfaceExpression.String())
	}
	switch product.Status {
	case ProductStatusActive, ProductStatusDisabled:
	default:
		return fmt.Errorf("%w: invalid product status", ErrValidation)
	}
	if containsPlainSecret(product.Metadata) {
		return fmt.Errorf("%w: product metadata must reference secrets indirectly", ErrValidation)
	}
	return nil
}

func validateInstallation(installation Installation) error {
	if strings.TrimSpace(installation.OrgID) == "" {
		return fmt.Errorf("%w: org_id is required", ErrValidation)
	}
	if !validProductSurface(installation.ProductSurface) {
		return fmt.Errorf("%w: product_surface must match %s", ErrValidation, productSurfaceExpression.String())
	}
	switch installation.AuthMode {
	case AuthModeNone, AuthModeAPIKeyRef, AuthModeOAuth2, AuthModeInternalJWT, AuthModeCustom:
	default:
		return fmt.Errorf("%w: invalid auth_mode", ErrValidation)
	}
	if installation.Enabled && installation.BaseURL == "" {
		return fmt.Errorf("%w: base_url is required for enabled installations", ErrValidation)
	}
	if installation.BaseURL != "" {
		parsed, err := url.Parse(installation.BaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%w: base_url must be an http(s) URL", ErrValidation)
		}
	}
	if authModeNeedsSecretRef(installation.AuthMode) && installation.SecretRef == "" {
		return fmt.Errorf("%w: secret_ref is required for auth_mode %s", ErrValidation, installation.AuthMode)
	}
	if containsPlainSecret(installation.Config) {
		return fmt.Errorf("%w: installation config must not contain inline secrets", ErrValidation)
	}
	return nil
}

func validProductSurface(surface string) bool {
	return productSurfaceExpression.MatchString(strings.TrimSpace(surface))
}

func authModeNeedsSecretRef(authMode string) bool {
	switch authMode {
	case AuthModeAPIKeyRef, AuthModeOAuth2, AuthModeCustom:
		return true
	default:
		return false
	}
}

func containsPlainSecret(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if isSensitiveConfigKey(key) {
				return true
			}
			if containsPlainSecret(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsPlainSecret(item) {
				return true
			}
		}
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return false
		}
		return containsPlainSecret(decoded)
	}
	return false
}

func isSensitiveConfigKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, token := range []string{"password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "private_key", "client_secret"} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}
