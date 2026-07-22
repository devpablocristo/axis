package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"golang.org/x/oauth2"
)

func TestResolverChecksResourceAndCRC(t *testing.T) {
	value := []byte("01234567890123456789012345678901")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"payload": map[string]any{"data": base64.StdEncoding.EncodeToString(value), "dataCrc32c": strconv.FormatUint(uint64(crc32.Checksum(value, crc32.MakeTable(crc32.Castagnoli))), 10)}})
	}))
	defer server.Close()
	resolver, _ := NewGCPResolver(server.URL, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"}), server.Client())
	got, err := resolver.Resolve(context.Background(), Ref("projects/p/secrets/attestation/versions/latest"))
	if err != nil || string(got.Bytes) != string(value) {
		t.Fatalf("value=%q err=%v", got.Bytes, err)
	}
	got.Destroy()
	if _, err := resolver.Resolve(context.Background(), Ref("https://invalid")); err == nil {
		t.Fatal("expected invalid reference")
	}
}
