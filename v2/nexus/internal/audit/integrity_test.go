package audit

import (
	"context"
	"testing"
	"time"

	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// sealChain seals a slice of events into a proper chain the way Append would
// (previous_hash = prior event_hash), but without a DB — so the pure hashing +
// chain + signing logic can be tested in isolation.
func sealChain(t *testing.T, r *Repository, events []auditdomain.AuditEvent) []auditdomain.AuditEvent {
	t.Helper()
	var prev string
	base := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	out := make([]auditdomain.AuditEvent, 0, len(events))
	for i, e := range events {
		e.ID = uuid.New()
		e.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if e.Data == nil {
			e.Data = map[string]any{}
		}
		if e.ChainScope == "" {
			e.ChainScope = auditdomain.ChainScopeFor(e.TenantID, e.VirployeeID)
		}
		e.PreviousHash = prev
		ph, eh, sig, err := r.sealEvent(e)
		if err != nil {
			t.Fatalf("seal event %d: %v", i, err)
		}
		e.PayloadHash, e.EventHash, e.Signature = ph, eh, sig
		if len(r.signingKey) > 0 {
			e.SignatureKeyID = r.signingKeyID
		}
		prev = e.EventHash
		out = append(out, e)
	}
	return out
}

func sampleEvents() []auditdomain.AuditEvent {
	return []auditdomain.AuditEvent{
		{
			TenantID: "tenant-1", VirployeeID: "vp-1", EventType: auditdomain.EventAssistCompleted,
			SubjectType: "assist_run", SubjectID: "run-1", ActorType: "service", ActorID: "service:medmory",
			Summary: "Diagnóstico completado", Data: map[string]any{"output_hash": "abc", "answered": true},
		},
		{
			TenantID: "tenant-1", VirployeeID: "vp-1", EventType: auditdomain.EventExecutionSucceeded,
			SubjectType: "binding", SubjectID: "bind-1", ActorType: "service", ActorID: "service:medmory",
			Summary: "Ejecución ok", Data: map[string]any{"binding_hash": "def"},
		},
	}
}

func TestChainVerifies(t *testing.T) {
	r := NewRepository(nil)
	events := sealChain(t, r, sampleEvents())
	out := verifyEvents(events)
	if out.Status != "ok" {
		t.Fatalf("expected ok, got %+v", out)
	}
	if out.CheckedEvents != 2 {
		t.Fatalf("expected 2 checked, got %d", out.CheckedEvents)
	}
	if out.FirstHash != events[0].EventHash || out.LastHash != events[1].EventHash {
		t.Fatalf("first/last hash mismatch: %+v", out)
	}
}

func TestPayloadTamperDetected(t *testing.T) {
	r := NewRepository(nil)
	events := sealChain(t, r, sampleEvents())
	events[1].Summary = "resultado alterado" // edit content after sealing
	out := verifyEvents(events)
	if out.Status != "failed" || out.Error != "payload hash mismatch" {
		t.Fatalf("expected payload hash mismatch, got %+v", out)
	}
	if out.CheckedEvents != 1 {
		t.Fatalf("expected failure at index 1, got %d", out.CheckedEvents)
	}
}

func TestReorderDetected(t *testing.T) {
	r := NewRepository(nil)
	events := sealChain(t, r, sampleEvents())
	events[0], events[1] = events[1], events[0] // swap: now first event carries a non-empty previous_hash
	out := verifyEvents(events)
	if out.Status != "failed" || out.Error != "previous hash mismatch" {
		t.Fatalf("expected previous hash mismatch on reorder, got %+v", out)
	}
}

func TestEventHashTamperDetected(t *testing.T) {
	r := NewRepository(nil)
	events := sealChain(t, r, sampleEvents())
	events[1].EventHash = "deadbeef" // forge the sealed event hash
	out := verifyEvents(events)
	if out.Status != "failed" || out.Error != "event hash mismatch" {
		t.Fatalf("expected event hash mismatch, got %+v", out)
	}
}

func TestUnsealedEventDetected(t *testing.T) {
	events := sampleEvents() // never sealed → empty hashes
	out := verifyEvents(events)
	if out.Status != "failed" || out.Error != "unsealed event encountered" {
		t.Fatalf("expected unsealed detection, got %+v", out)
	}
}

func TestSignaturesVerify(t *testing.T) {
	r := NewRepository(nil, WithSigner("super-secret", "k1"))
	if !r.Signed() {
		t.Fatal("expected a signed repository")
	}
	events := sealChain(t, r, sampleEvents())
	if err := r.VerifySignatures(events); err != nil {
		t.Fatalf("valid signatures must verify: %v", err)
	}

	wrongKey := NewRepository(nil, WithSigner("other-key", "k1"))
	if err := wrongKey.VerifySignatures(events); err == nil {
		t.Fatal("a different key must fail signature verification")
	}

	events[0].Signature = ""
	if err := r.VerifySignatures(events); err == nil {
		t.Fatal("a missing signature must fail when a key is configured")
	}
}

func TestUnsignedRepoSkipsSignatureCheck(t *testing.T) {
	r := NewRepository(nil) // no signing key
	if r.Signed() {
		t.Fatal("expected an unsigned repository")
	}
	events := sealChain(t, r, sampleEvents())
	if events[0].Signature != "" {
		t.Fatal("an unsigned repository must not produce signatures")
	}
	if err := r.VerifySignatures(events); err != nil {
		t.Fatalf("signature verification must be a no-op without a key: %v", err)
	}
	if out := verifyEvents(events); out.Status != "ok" {
		t.Fatalf("unsigned chain must still verify: %+v", out)
	}
}

func TestSignedRepoAcceptsHistoricalUnsignedPrefix(t *testing.T) {
	unsigned := sealChain(t, NewRepository(nil), sampleEvents()[:1])
	signedRepo := NewRepository(nil, WithSigner("super-secret", "k1"))
	next := sampleEvents()[1]
	next.ID = uuid.New()
	next.CreatedAt = unsigned[0].CreatedAt.Add(time.Second)
	next.ChainScope = unsigned[0].ChainScope
	next.PreviousHash = unsigned[0].EventHash
	ph, eh, signature, err := signedRepo.sealEvent(next)
	if err != nil {
		t.Fatalf("seal signed suffix: %v", err)
	}
	next.PayloadHash, next.EventHash, next.Signature = ph, eh, signature
	next.SignatureKeyID = signedRepo.signingKeyID
	events := append(unsigned, next)
	if err := signedRepo.VerifySignatures(events); err != nil {
		t.Fatalf("historical unsigned prefix must remain verifiable: %v", err)
	}
	if out := verifyEvents(events); out.Status != "ok" || !out.Signed {
		t.Fatalf("mixed chain must be valid and report signatures: %+v", out)
	}
}

// --- usecases (validation + replay) with a fake repo ---

type fakeRepo struct {
	appended []auditdomain.AuditEvent
	list     []auditdomain.AuditEvent
}

func (f *fakeRepo) Append(_ context.Context, e auditdomain.AuditEvent) (auditdomain.AuditEvent, error) {
	f.appended = append(f.appended, e)
	return e, nil
}
func (f *fakeRepo) ListByScope(_ context.Context, _ string) ([]auditdomain.AuditEvent, error) {
	return f.list, nil
}
func (f *fakeRepo) VerifySignatures([]auditdomain.AuditEvent) error { return nil }

func TestAppendValidation(t *testing.T) {
	uc := NewUseCases(&fakeRepo{})
	cases := []struct {
		name   string
		tenant string
		in     auditdomain.AppendInput
	}{
		{"missing tenant", "", auditdomain.AppendInput{VirployeeID: "vp", EventType: "x"}},
		{"missing virployee", "t", auditdomain.AppendInput{EventType: "x"}},
		{"missing event_type", "t", auditdomain.AppendInput{VirployeeID: "vp"}},
		{"invalid idempotency key", "t", auditdomain.AppendInput{VirployeeID: "vp", EventType: "x", IdempotencyKey: "not-a-uuid"}},
	}
	for _, tc := range cases {
		if _, err := uc.Append(context.Background(), tc.tenant, tc.in); !domainerr.IsValidation(err) {
			t.Fatalf("%s: expected validation error, got %v", tc.name, err)
		}
	}
}

func TestAppendUsesUUIDIdempotencyKeyAsEventID(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewUseCases(repo)
	id := uuid.New()
	_, err := uc.Append(context.Background(), "tenant-1", auditdomain.AppendInput{
		IdempotencyKey: id.String(), VirployeeID: "vp-1", EventType: auditdomain.EventAssistCompleted,
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(repo.appended) != 1 || repo.appended[0].ID != id {
		t.Fatalf("idempotency key must become the stable event id: %+v", repo.appended)
	}
}

func TestSameAuditAppendRequestIgnoresDeliveryTimeButRejectsChangedPayload(t *testing.T) {
	base := auditdomain.AuditEvent{
		ID: uuid.New(), TenantID: "tenant-1", ChainScope: "tenant-1/vp-1", VirployeeID: "vp-1",
		SubjectType: "delegation", SubjectID: uuid.NewString(), EventType: "delegation_revoked",
		ActorType: "human", ActorID: "owner-1", Summary: "professional delegation revoked",
		Data: map[string]any{"revision": float64(2), "snapshot_hash": "abc"}, CreatedAt: time.Now().UTC(),
	}
	retry := base
	retry.CreatedAt = base.CreatedAt.Add(time.Hour)
	retry.PreviousHash = "ignored-on-retry"
	if !sameAuditAppendRequest(base, retry) {
		t.Fatal("same logical append must be idempotent regardless of retry time")
	}
	retry.Data = map[string]any{"revision": float64(3), "snapshot_hash": "abc"}
	if sameAuditAppendRequest(base, retry) {
		t.Fatal("same id with changed payload must conflict")
	}
}

func TestAppendBuildsScopeAndDefaultsActor(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewUseCases(repo)
	_, err := uc.Append(context.Background(), "tenant-1", auditdomain.AppendInput{
		VirployeeID: "vp-1", EventType: auditdomain.EventAssistCompleted,
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(repo.appended) != 1 {
		t.Fatalf("expected one appended event, got %d", len(repo.appended))
	}
	got := repo.appended[0]
	if got.ChainScope != "tenant-1/vp-1" {
		t.Fatalf("expected per-virployee scope, got %q", got.ChainScope)
	}
	if got.ActorType != "service" {
		t.Fatalf("expected actor_type defaulted to service, got %q", got.ActorType)
	}
}

func TestReplayReturnsTimelineAndIntegrity(t *testing.T) {
	sealed := sealChain(t, NewRepository(nil), sampleEvents())
	uc := NewUseCases(&fakeRepo{list: sealed})
	out, err := uc.Replay(context.Background(), "tenant-1", "vp-1")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if out.EventCount != 2 || len(out.Timeline) != 2 {
		t.Fatalf("expected 2 events in timeline, got %+v", out)
	}
	if out.Integrity == nil || out.Integrity.Status != "ok" {
		t.Fatalf("expected ok integrity, got %+v", out.Integrity)
	}
	if out.Scope != "tenant-1/vp-1" {
		t.Fatalf("expected scope tenant-1/vp-1, got %q", out.Scope)
	}
}
