package callbacks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestHTTPApprovalPublisher_PublishPending(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var received []ApprovalEvent
	var gotToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotToken = r.Header.Get("X-Internal-Service-Token")
		var evt ApprovalEvent
		if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
			t.Errorf("decode: %v", err)
		}
		received = append(received, evt)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pub := NewHTTPApprovalPublisher("secret-tok", []string{srv.URL}, nil)

	err := pub.Publish(context.Background(), ApprovalEvent{
		Event:     EventApprovalPending,
		RequestID: "req-123",
		RiskLevel: "high",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(received))
	}
	if received[0].RequestID != "req-123" {
		t.Fatalf("unexpected request_id: %s", received[0].RequestID)
	}
	if gotToken != "secret-tok" {
		t.Fatalf("expected token secret-tok, got %s", gotToken)
	}
}

func TestHTTPApprovalPublisher_PublishResolved(t *testing.T) {
	t.Parallel()

	var received ApprovalEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pub := NewHTTPApprovalPublisher("", nil, []string{srv.URL})

	err := pub.Publish(context.Background(), ApprovalEvent{
		Event:      EventApprovalResolved,
		RequestID:  "req-456",
		Decision:   "approved",
		DecidedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received.Decision != "approved" {
		t.Fatalf("unexpected decision: %s", received.Decision)
	}
}

func TestHTTPApprovalPublisher_NoURLsIsNoop(t *testing.T) {
	t.Parallel()

	pub := NewHTTPApprovalPublisher("tok", nil, nil)

	err := pub.Publish(context.Background(), ApprovalEvent{
		Event:     EventApprovalPending,
		RequestID: "req-789",
	})
	if err != nil {
		t.Fatalf("expected no error for empty URLs, got %v", err)
	}
}

func TestHTTPApprovalPublisher_UnknownEventIsNoop(t *testing.T) {
	t.Parallel()

	pub := NewHTTPApprovalPublisher("tok", []string{"http://localhost:9999"}, nil)

	err := pub.Publish(context.Background(), ApprovalEvent{
		Event:     "unknown_event",
		RequestID: "req-000",
	})
	if err != nil {
		t.Fatalf("expected no error for unknown event, got %v", err)
	}
}

func TestHTTPApprovalPublisher_ReturnsErrorOnServerFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	pub := NewHTTPApprovalPublisher("", []string{srv.URL}, nil)

	err := pub.Publish(context.Background(), ApprovalEvent{
		Event:     EventApprovalPending,
		RequestID: "req-err",
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
