package evidence

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	evidencedomain "github.com/devpablocristo/nexus/internal/evidence/usecases/domain"
)

// Signer firma evidence packs con HMAC-SHA256.
type Signer struct {
	key   []byte
	keyID string
}

// NewSigner crea un signer con la clave proporcionada.
func NewSigner(signingKey string, keyID string) (*Signer, error) {
	if signingKey == "" {
		return nil, fmt.Errorf("signing key is required")
	}
	if keyID == "" {
		keyID = "default"
	}
	return &Signer{key: []byte(signingKey), keyID: keyID}, nil
}

// Sign calcula la firma HMAC-SHA256 del payload y retorna la Signature.
func (s *Signer) Sign(payload []byte) evidencedomain.Signature {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	return evidencedomain.Signature{
		Algorithm: "hmac-sha256",
		KeyID:     s.keyID,
		SignedAt:  time.Now().UTC().Format(time.RFC3339),
		Value:     hex.EncodeToString(mac.Sum(nil)),
	}
}

// SignPack firma un EvidencePack: serializa todo menos la firma, calcula HMAC y la agrega.
func (s *Signer) SignPack(pack *evidencedomain.EvidencePack) error {
	// Limpiar firma existente para calcular sobre el contenido puro
	pack.Signature = evidencedomain.Signature{}
	payload, err := json.Marshal(pack)
	if err != nil {
		return fmt.Errorf("marshal evidence pack for signing: %w", err)
	}
	pack.Signature = s.Sign(payload)
	return nil
}
