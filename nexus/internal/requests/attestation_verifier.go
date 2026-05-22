package requests

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
)

type HMACAttestationVerifier struct {
	secret []byte
}

func NewHMACAttestationVerifier(secret string) (*HMACAttestationVerifier, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("attestation hmac secret is required")
	}
	return &HMACAttestationVerifier{secret: []byte(secret)}, nil
}

func (v *HMACAttestationVerifier) Verify(_ context.Context, att requestdomain.Attestation) error {
	expected, err := attestationHMAC(v.secret, att)
	if err != nil {
		return err
	}
	signature := strings.TrimSpace(att.Signature)
	signature = strings.TrimPrefix(signature, "hmac-sha256:")
	got, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("invalid_attestation_signature")
	}
	if !hmac.Equal(got, expected) {
		return fmt.Errorf("attestation_signature_mismatch")
	}
	return nil
}

func attestationHMAC(secret []byte, att requestdomain.Attestation) ([]byte, error) {
	payload := map[string]any{
		"request_id":    att.RequestID.String(),
		"status":        strings.TrimSpace(att.Status),
		"provider_refs": att.ProviderRefs,
		"attester":      strings.TrimSpace(att.Attester),
		"metadata":      att.Metadata,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal_attestation_payload: %w", err)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(raw)
	return mac.Sum(nil), nil
}

func SignAttestationForTest(secret []byte, att requestdomain.Attestation) (string, error) {
	sum, err := attestationHMAC(secret, att)
	if err != nil {
		return "", err
	}
	return "hmac-sha256:" + hex.EncodeToString(sum), nil
}
