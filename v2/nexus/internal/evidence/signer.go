package evidence

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	evidencedomain "github.com/devpablocristo/nexus-v2/internal/evidence/usecases/domain"
)

// Signer signs evidence packs with HMAC-SHA256. Ported from v1
// (nexus/internal/evidence/signer.go).
type Signer struct {
	key   []byte
	keyID string
}

// NewSigner returns a signer, or nil when no key is configured (local-first:
// packs are then marked algorithm "none" instead of failing).
func NewSigner(signingKey, keyID string) *Signer {
	signingKey = strings.TrimSpace(signingKey)
	if signingKey == "" {
		return nil
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		keyID = "default"
	}
	return &Signer{key: []byte(signingKey), keyID: keyID}
}

// SignPack signs a pack: serialize everything but the signature, HMAC it, set it.
// Verification reproduces this: clear signature, marshal, HMAC, compare.
func (s *Signer) SignPack(pack *evidencedomain.EvidencePack) error {
	pack.Signature = evidencedomain.Signature{}
	payload, err := json.Marshal(pack)
	if err != nil {
		return fmt.Errorf("marshal evidence pack for signing: %w", err)
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(payload)
	pack.Signature = evidencedomain.Signature{
		Algorithm: "hmac-sha256",
		KeyID:     s.keyID,
		SignedAt:  time.Now().UTC().Format(time.RFC3339),
		Value:     hex.EncodeToString(mac.Sum(nil)),
	}
	return nil
}

// VerifyPack recomputes the HMAC over the pack (minus its signature) and checks
// it matches. Exposed so callers/tests can reverify a pack out of band.
func VerifyPack(signingKey string, pack evidencedomain.EvidencePack) error {
	signingKey = strings.TrimSpace(signingKey)
	if signingKey == "" {
		return fmt.Errorf("signing key is required to verify")
	}
	claimed := pack.Signature
	if strings.TrimSpace(claimed.Value) == "" {
		return fmt.Errorf("evidence pack is not signed")
	}
	pack.Signature = evidencedomain.Signature{}
	payload, err := json.Marshal(&pack)
	if err != nil {
		return fmt.Errorf("marshal evidence pack for verify: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(claimed.Value)) {
		return fmt.Errorf("evidence pack signature mismatch")
	}
	return nil
}
