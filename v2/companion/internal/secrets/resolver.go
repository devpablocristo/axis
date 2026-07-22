package secrets

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const maxSecretBytes = 64 << 10

var resourcePattern = regexp.MustCompile(`^projects/[a-zA-Z0-9._:-]+/secrets/[a-zA-Z0-9_-]+/versions/(latest|[0-9]+)$`)

type Ref string

type Value struct{ Bytes []byte }

func (v *Value) Destroy() {
	for i := range v.Bytes {
		v.Bytes[i] = 0
	}
	v.Bytes = nil
}

type ResolverPort interface {
	Resolve(context.Context, Ref) (Value, error)
}

func ValidRef(ref string) bool {
	resource := strings.TrimSpace(strings.TrimPrefix(ref, "secretmanager://"))
	return resourcePattern.MatchString(resource)
}

type GCPResolver struct {
	endpoint string
	tokens   oauth2.TokenSource
	client   *http.Client
}

func NewGCPResolver(endpoint string, tokens oauth2.TokenSource, client *http.Client) (*GCPResolver, error) {
	if tokens == nil {
		return nil, errors.New("secret manager token source is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		endpoint = "https://secretmanager.googleapis.com"
	}
	return &GCPResolver{endpoint: endpoint, tokens: tokens, client: client}, nil
}

func (r *GCPResolver) Resolve(ctx context.Context, ref Ref) (Value, error) {
	resource := strings.TrimSpace(strings.TrimPrefix(string(ref), "secretmanager://"))
	if !ValidRef(string(ref)) {
		return Value{}, errors.New("invalid Secret Manager reference")
	}
	token, err := r.tokens.Token()
	if err != nil {
		return Value{}, fmt.Errorf("secret manager token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint+"/v1/"+resource+":access", nil)
	if err != nil {
		return Value{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	resp, err := r.client.Do(req)
	if err != nil {
		return Value{}, fmt.Errorf("secret manager access: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return Value{}, fmt.Errorf("secret manager access status %d", resp.StatusCode)
	}
	var payload struct {
		Payload struct {
			Data      string `json:"data"`
			DataCRC32 string `json:"dataCrc32c"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSecretBytes*2)).Decode(&payload); err != nil {
		return Value{}, errors.New("invalid Secret Manager response")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Payload.Data)
	if err != nil || len(decoded) == 0 || len(decoded) > maxSecretBytes {
		return Value{}, errors.New("invalid Secret Manager payload")
	}
	if payload.Payload.DataCRC32 != "" {
		want, err := strconv.ParseUint(payload.Payload.DataCRC32, 10, 32)
		if err != nil {
			zero(decoded)
			return Value{}, errors.New("invalid Secret Manager checksum")
		}
		got := crc32.Checksum(decoded, crc32.MakeTable(crc32.Castagnoli))
		if subtle.ConstantTimeEq(int32(got), int32(want)) != 1 {
			zero(decoded)
			return Value{}, errors.New("secret manager checksum mismatch")
		}
	}
	return Value{Bytes: decoded}, nil
}

func zero(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
