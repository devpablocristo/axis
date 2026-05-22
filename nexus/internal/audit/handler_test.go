package audit_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/devpablocristo/nexus/internal/audit"
	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	"github.com/google/uuid"
)

// --- Fakes ---

type fakeAuditRepo struct {
	mu     sync.RWMutex
	events []auditdomain.RequestEvent
}

func (r *fakeAuditRepo) Append(_ context.Context, e auditdomain.RequestEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	r.events = append(r.events, e)
	return nil
}

func (r *fakeAuditRepo) ListByRequestID(_ context.Context, requestID uuid.UUID) ([]auditdomain.RequestEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []auditdomain.RequestEvent
	for _, e := range r.events {
		if e.RequestID == requestID {
			out = append(out, e)
		}
	}
	return out, nil
}

type fakeRequestGetter struct {
	info audit.ReplayRequestInfo
}

func (s *fakeRequestGetter) GetReplayInfo(_ context.Context, _ uuid.UUID) (audit.ReplayRequestInfo, error) {
	return s.info, nil
}

func TestReplay(t *testing.T) {
	t.Parallel()
	repo := &fakeAuditRepo{}
	requestID := uuid.New()

	events := []auditdomain.RequestEvent{
		{ID: uuid.New(), RequestID: requestID, EventType: auditdomain.EventReceived, ActorType: "requester", ActorID: "ops-bot", Summary: "Request received", CreatedAt: time.Now().Add(-3 * time.Minute)},
		{ID: uuid.New(), RequestID: requestID, EventType: auditdomain.EventEvaluated, ActorType: "system", ActorID: "nexus", Summary: "Risk: high", CreatedAt: time.Now().Add(-2 * time.Minute)},
		{ID: uuid.New(), RequestID: requestID, EventType: auditdomain.EventApproved, ActorType: "human", ActorID: "admin@co", Summary: "Approved", CreatedAt: time.Now()},
	}
	for _, e := range events {
		repo.Append(context.Background(), e)
	}

	reqGetter := &fakeRequestGetter{info: audit.ReplayRequestInfo{
		RequesterType: "agent", RequesterID: "ops-bot", ActionType: "alert.silence",
		TargetSystem: "pagerduty", TargetResource: "CPU-CRITICAL", Status: "approved",
	}}
	uc := audit.NewUsecases(repo, reqGetter)
	mux := http.NewServeMux()
	audit.NewHandler(uc).Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/requests/"+requestID.String()+"/replay", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var replay audit.ReplayOutput
	json.NewDecoder(rec.Body).Decode(&replay)
	if len(replay.Timeline) != 3 {
		t.Fatalf("expected 3 events, got %d", len(replay.Timeline))
	}
	if replay.FinalStatus != "approved" {
		t.Fatalf("expected approved, got %s", replay.FinalStatus)
	}
}

func TestReplayInvalidID(t *testing.T) {
	t.Parallel()
	uc := audit.NewUsecases(&fakeAuditRepo{}, &fakeRequestGetter{})
	mux := http.NewServeMux()
	audit.NewHandler(uc).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/requests/not-a-uuid/replay", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
