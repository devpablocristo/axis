package attestation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const Version = "axis.executor-attestation.v1"

type Payload struct {
	Version           string         `json:"version"`
	ExecutorVersion   string         `json:"executor_version"`
	OrgID             string         `json:"org_id"`
	GovernanceCheckID string         `json:"governance_check_id"`
	BindingHash       string         `json:"binding_hash"`
	IdempotencyKey    string         `json:"idempotency_key"`
	Status            string         `json:"status"`
	DurationMS        int64          `json:"duration_ms"`
	ResultHash        string         `json:"result_hash"`
	Result            map[string]any `json:"-"`
}

type Verifier struct{ key []byte }

func NewVerifier(key []byte) (*Verifier, error) {
	if len(key) < 32 {
		return nil, errors.New("executor attestation key must contain at least 32 bytes")
	}
	return &Verifier{key: append([]byte(nil), key...)}, nil
}

func DeriveDevelopmentKey(token string) []byte {
	sum := sha256.Sum256([]byte("axis-v2/dev-executor-attestation\x00" + strings.TrimSpace(token)))
	return append([]byte(nil), sum[:]...)
}

func (v *Verifier) Verify(payload Payload, signature string) error {
	if v == nil || len(v.key) == 0 {
		return errors.New("executor attestation verifier is not configured")
	}
	if payload.Version != Version || strings.TrimSpace(payload.ExecutorVersion) == "" {
		return errors.New("unsupported executor attestation")
	}
	provided, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(provided) != sha256.Size {
		return errors.New("invalid executor attestation")
	}
	raw, err := canonicalBytes(payload)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, v.key)
	_, _ = mac.Write(raw)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("executor attestation mismatch")
	}
	return nil
}

func ResultHash(result map[string]any) (string, error) {
	if result == nil {
		result = map[string]any{}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalBytes(payload Payload) ([]byte, error) {
	hash := payload.ResultHash
	if payload.Result != nil || hash == "" {
		computed, err := ResultHash(payload.Result)
		if err != nil {
			return nil, err
		}
		if hash != "" && hash != computed {
			return nil, errors.New("executor result hash mismatch")
		}
		hash = computed
	}
	payload.ResultHash = hash
	return json.Marshal(payload)
}
