package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type staticSecret []byte

const testProductID = "11111111-1111-4111-8111-111111111111"

func (s staticSecret) Resolve(context.Context, string) ([]byte, error) {
	return append([]byte(nil), s...), nil
}

func testDescriptor(baseURL, capabilityID string) Descriptor {
	return Descriptor{
		SchemaVersion: SchemaVersion, BindingID: "records.connector", BaseURL: baseURL,
		ProductID: testProductID, SecretRef: "env://CONNECTOR_TEST_SECRET",
		Operations: []OperationDescriptor{{
			Name: "records.search", CapabilityID: capabilityID,
			InputSchema: map[string]any{"type": "object"}, OutputSchema: map[string]any{"type": "object"},
			TimeoutMS: 1000,
		}},
	}
}

func TestHTTPAdapterSignsValidatesAndInvokes(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	invocationID := uuid.NewString()
	capabilityID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := json.RawMessage(`{"schema_version":"axis.connector.v1","invocation_id":"` + invocationID +
			`","product_id":"` + testProductID + `","capability_id":"` + capabilityID +
			`","operation":"records.search","status":"succeeded","payload":{"ok":true}}`)
		if !strings.HasPrefix(r.Header.Get(headerSignature), "v1=") || r.Header.Get(headerIdempotency) != "idem-1" {
			t.Errorf("request was not signed or idempotent")
		}
		w.Header().Set(headerResponseSignature, signature(secret, canonicalResponse(http.StatusOK, "idem-1", raw)))
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	validator := SchemaValidatorFunc(func(_ map[string]any, value map[string]any) error {
		if value == nil {
			return errors.New("nil value")
		}
		return nil
	})
	adapter, err := NewHTTPAdapter(testDescriptor(server.URL, capabilityID), "test", server.Client(), staticSecret(secret), validator)
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Invoke(context.Background(), InvokeRequest{
		InvocationID: invocationID, OrgID: "org-1", ProductID: testProductID,
		CapabilityID: capabilityID, Operation: "records.search",
		Arguments: map[string]any{"query": "labs"}, IdempotencyKey: "idem-1",
	})
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("Invoke: result=%+v err=%v", result, err)
	}
}

func TestHTTPAdapterRejectsUnsignedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schema_version":"axis.connector.v1"}`))
	}))
	defer server.Close()
	capabilityID := uuid.NewString()
	adapter, err := NewHTTPAdapter(testDescriptor(server.URL, capabilityID), "test", server.Client(), staticSecret(strings.Repeat("s", 32)), SchemaValidatorFunc(func(map[string]any, map[string]any) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Invoke(context.Background(), InvokeRequest{
		InvocationID: uuid.NewString(), OrgID: "org-1", ProductID: testProductID,
		CapabilityID: capabilityID, Operation: "records.search", Arguments: map[string]any{}, IdempotencyKey: "idem",
	})
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("unsigned response must fail, got %v", err)
	}
}

func TestNormalizeDescriptorRequiresHTTPSOutsideDevelopment(t *testing.T) {
	_, err := NormalizeDescriptor(testDescriptor("http://connector.example", uuid.NewString()), "production")
	if err == nil {
		t.Fatal("production connector must require HTTPS")
	}
}

func TestHTTPAdapterMarksPOSTTransportFailureAmbiguous(t *testing.T) {
	capabilityID := uuid.NewString()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset")
	})}
	adapter, err := NewHTTPAdapter(testDescriptor("http://connector.invalid", capabilityID), "test", client, staticSecret(strings.Repeat("s", 32)), SchemaValidatorFunc(func(map[string]any, map[string]any) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	invocationID := uuid.NewString()
	_, err = adapter.Invoke(context.Background(), InvokeRequest{
		InvocationID: invocationID, OrgID: "org-1", ProductID: testProductID,
		CapabilityID: capabilityID, Operation: "records.search", Arguments: map[string]any{}, IdempotencyKey: "idem",
	})
	var transportErr *TransportError
	if !errors.As(err, &transportErr) || !transportErr.Ambiguous || transportErr.InvocationID != invocationID {
		t.Fatalf("POST failure must be recoverable through status: %T %v", err, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
