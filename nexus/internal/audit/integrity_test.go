package audit

import (
	"testing"
	"time"

	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	"github.com/google/uuid"
)

func TestVerifyEventsDetectsTampering(t *testing.T) {
	requestID := uuid.New()
	createdAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	first := sealedTestEvent(t, auditdomain.RequestEvent{
		ID:         uuid.New(),
		RequestID:  requestID,
		EventType:  auditdomain.EventReceived,
		ActorType:  "requester",
		ActorID:    "agent-1",
		Summary:    "received",
		Data:       map[string]any{"a": "b"},
		CreatedAt:  createdAt,
		ChainScope: requestID.String(),
	})
	second := sealedTestEvent(t, auditdomain.RequestEvent{
		ID:           uuid.New(),
		RequestID:    requestID,
		EventType:    auditdomain.EventEvaluated,
		ActorType:    "system",
		ActorID:      "nexus",
		Summary:      "evaluated",
		Data:         map[string]any{"risk": "low"},
		CreatedAt:    createdAt.Add(time.Second),
		ChainScope:   requestID.String(),
		PreviousHash: first.EventHash,
	})

	if out := verifyEvents([]auditdomain.RequestEvent{first, second}); out.Status != "ok" {
		t.Fatalf("expected ok integrity, got %#v", out)
	}

	second.Summary = "tampered"
	if out := verifyEvents([]auditdomain.RequestEvent{first, second}); out.Status != "failed" || out.Error != "payload hash mismatch" {
		t.Fatalf("expected payload hash mismatch, got %#v", out)
	}
}

func sealedTestEvent(t *testing.T, event auditdomain.RequestEvent) auditdomain.RequestEvent {
	t.Helper()
	payloadHash, err := ComputePayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.PayloadHash = payloadHash
	eventHash, err := ComputeEventHash(event, payloadHash)
	if err != nil {
		t.Fatal(err)
	}
	event.EventHash = eventHash
	return event
}
