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
	TenantID          string         `json:"tenant_id"`
	GovernanceCheckID string         `json:"governance_check_id"`
	BindingHash       string         `json:"binding_hash"`
	IdempotencyKey    string         `json:"idempotency_key"`
	Status            string         `json:"status"`
	DurationMS        int64          `json:"duration_ms"`
	ResultHash        string         `json:"result_hash"`
	Result            map[string]any `json:"-"`
}

type Signed struct {
	Version         string
	ExecutorVersion string
	Signature       string
}

type Signer struct {
	key             []byte
	executorVersion string
}

func NewSigner(key []byte, executorVersion string) (*Signer, error) {
	if len(key) < 32 {
		return nil, errors.New("executor attestation key must contain at least 32 bytes")
	}
	executorVersion = strings.TrimSpace(executorVersion)
	if executorVersion == "" {
		executorVersion = "companion-v2"
	}
	return &Signer{key: append([]byte(nil), key...), executorVersion: executorVersion}, nil
}

func DeriveDevelopmentKey(internalToken string) []byte {
	sum := sha256.Sum256([]byte("axis-v2/dev-executor-attestation\x00" + strings.TrimSpace(internalToken)))
	return append([]byte(nil), sum[:]...)
}

func (s *Signer) Sign(payload Payload) (Signed, error) {
	if s == nil || len(s.key) == 0 {
		return Signed{}, errors.New("executor attestation signer is not configured")
	}
	payload.Version = Version
	payload.ExecutorVersion = s.executorVersion
	raw, err := canonicalBytes(payload)
	if err != nil {
		return Signed{}, err
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(raw)
	return Signed{
		Version: Version, ExecutorVersion: s.executorVersion,
		Signature: base64.RawURLEncoding.EncodeToString(mac.Sum(nil)),
	}, nil
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
