package wire

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
)

type internalJWTConfig struct {
	Secret   string
	Issuer   string
	Audience string
}

type fallbackAuthenticator struct {
	authenticators []authn.Authenticator
}

func (a fallbackAuthenticator) Authenticate(ctx context.Context, cred authn.Credential) (*authn.Principal, error) {
	var lastErr error
	for _, authenticator := range a.authenticators {
		if authenticator == nil {
			continue
		}
		principal, err := authenticator.Authenticate(ctx, cred)
		if err == nil && principal != nil {
			return principal, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("authn: no jwt authenticator configured")
}

func newFallbackAuthenticator(authenticators ...authn.Authenticator) authn.Authenticator {
	active := make([]authn.Authenticator, 0, len(authenticators))
	for _, authenticator := range authenticators {
		if authenticator != nil {
			active = append(active, authenticator)
		}
	}
	if len(active) == 0 {
		return nil
	}
	if len(active) == 1 {
		return active[0]
	}
	return fallbackAuthenticator{authenticators: active}
}

func newInternalJWTAuthenticator(cfg internalJWTConfig) authn.Authenticator {
	cfg.Secret = strings.TrimSpace(cfg.Secret)
	if cfg.Secret == "" {
		return nil
	}
	expectedIssuer := strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	expectedAudience := strings.TrimSpace(cfg.Audience)
	return &authn.BearerJWTAuthenticator{
		Verify: hmacJWTVerifier{secret: cfg.Secret},
		Map: func(_ context.Context, claims map[string]any) (authn.Principal, error) {
			if expectedIssuer != "" && normalizeIssuer(claims["iss"]) != expectedIssuer {
				return authn.Principal{}, errors.New("authn: invalid internal jwt issuer")
			}
			if expectedAudience != "" &&
				!claimContainsAudience(claims["aud"], expectedAudience) &&
				!claimContainsAudience(claims["azp"], expectedAudience) {
				return authn.Principal{}, errors.New("authn: invalid internal jwt audience")
			}
			sub := firstNonEmptyClaim(claims, "sub", "actor_id")
			if sub == "" {
				return authn.Principal{}, errors.New("authn: missing internal jwt subject")
			}
			return authn.Principal{
				OrgID:      firstNonEmptyClaim(claims, "org_id", "tenant_id", "orgId"),
				Actor:      firstNonEmptyClaim(claims, "actor_id", "email", "preferred_username", "username", "sub"),
				Role:       firstNonEmptyClaim(claims, "role"),
				Scopes:     claimScopes(claims),
				Claims:     claims,
				AuthMethod: "internal_jwt",
			}, nil
		},
	}
}

type hmacJWTVerifier struct {
	secret string
}

func (v hmacJWTVerifier) VerifyToken(_ context.Context, rawToken string) (map[string]any, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("authn: invalid jwt format")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("authn: decode jwt header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("authn: parse jwt header: %w", err)
	}
	if header.Alg != "HS256" {
		return nil, errors.New("authn: unsupported internal jwt alg")
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(v.secret))
	_, _ = mac.Write([]byte(unsigned))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errors.New("authn: invalid internal jwt signature")
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("authn: decode jwt claims: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("authn: parse jwt claims: %w", err)
	}
	now := time.Now().Unix()
	if exp, ok := numericClaim(claims["exp"]); ok && exp < now {
		return nil, errors.New("authn: internal jwt expired")
	}
	if nbf, ok := numericClaim(claims["nbf"]); ok && nbf > now {
		return nil, errors.New("authn: internal jwt not active")
	}
	return claims, nil
}

func numericClaim(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}
