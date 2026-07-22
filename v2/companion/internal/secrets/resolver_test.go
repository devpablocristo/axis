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

func TestGCPResolverVerifiesCRCAndNeverAcceptsArbitraryURL(t *testing.T) {
	secret := []byte("credential-json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/project/secrets/calendar/versions/latest:access" || r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("path/auth = %q %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"payload": map[string]any{
			"data":       base64.StdEncoding.EncodeToString(secret),
			"dataCrc32c": strconv.FormatUint(uint64(crc32.Checksum(secret, crc32.MakeTable(crc32.Castagnoli))), 10),
		}})
	}))
	defer server.Close()
	resolver, err := NewGCPResolver(server.URL, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"}), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	value, err := resolver.Resolve(context.Background(), Ref("secretmanager://projects/project/secrets/calendar/versions/latest"))
	if err != nil || string(value.Bytes) != string(secret) {
		t.Fatalf("value=%q err=%v", value.Bytes, err)
	}
	value.Destroy()
	if value.Bytes != nil {
		t.Fatal("destroy must release secret bytes")
	}
	if _, err := resolver.Resolve(context.Background(), Ref("https://attacker.invalid/secret")); err == nil {
		t.Fatal("arbitrary URLs must be rejected")
	}
}

func TestGCPResolverRejectsChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"payload": map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("secret")), "dataCrc32c": "1"}})
	}))
	defer server.Close()
	resolver, _ := NewGCPResolver(server.URL, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"}), server.Client())
	if _, err := resolver.Resolve(context.Background(), Ref("projects/p/secrets/s/versions/1")); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}
