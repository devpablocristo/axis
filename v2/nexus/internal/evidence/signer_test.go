package evidence

import (
	"context"
	"testing"

	"github.com/devpablocristo/nexus-v2/internal/audit"
)

type fakeAuditReader struct{ out audit.ReplayOutput }

func (f fakeAuditReader) Replay(context.Context, string, string) (audit.ReplayOutput, error) {
	return f.out, nil
}

type fakeSubjectAuditReader struct {
	primary audit.ReplayOutput
	chains  []audit.ReplayOutput
}

func (f fakeSubjectAuditReader) Replay(context.Context, string, string) (audit.ReplayOutput, error) {
	return f.primary, nil
}
func (f fakeSubjectAuditReader) ReplaySubject(context.Context, string, string) ([]audit.ReplayOutput, error) {
	return f.chains, nil
}

func sampleReplay() audit.ReplayOutput {
	return audit.ReplayOutput{
		Scope:       "organization-1/vp-1",
		VirployeeID: "vp-1",
		EventCount:  2,
		Timeline: []audit.TimelineEntry{
			{Event: "assist_completed", Actor: "service:producta", Subject: "assist_run run-1", SubjectID: "run-1", At: "2026-07-21T12:00:00Z", Summary: "dx", Data: map[string]any{"output_hash": "abc"}, EventHash: "h1"},
			{Event: "assist_completed", Actor: "service:producta", Subject: "assist_run run-2", SubjectID: "run-2", At: "2026-07-21T12:01:00Z", Summary: "dx2", EventHash: "h2"},
		},
		Integrity: &audit.IntegrityOutput{Status: "ok", CheckedEvents: 2, FirstHash: "h1", LastHash: "h2", Signed: true},
	}
}

func TestGenerateSignedPackReverifies(t *testing.T) {
	uc := NewUseCases(fakeAuditReader{out: sampleReplay()}, NewSigner("super-secret", "k1"))
	pack, err := uc.Generate(context.Background(), "organization-1", "vp-1", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if pack.Version != EvidenceVersion || pack.EventCount != 2 || len(pack.Timeline) != 2 {
		t.Fatalf("unexpected pack shape: %+v", pack)
	}
	if pack.Integrity.Status != "ok" {
		t.Fatalf("expected ok integrity, got %+v", pack.Integrity)
	}
	if pack.Signature.Algorithm != "hmac-sha256" || pack.Signature.Value == "" {
		t.Fatalf("expected a signed pack, got %+v", pack.Signature)
	}
	if err := VerifyPack("super-secret", pack); err != nil {
		t.Fatalf("valid pack must reverify: %v", err)
	}
	if err := VerifyPack("wrong-key", pack); err == nil {
		t.Fatal("a wrong key must fail verification")
	}
}

func TestGenerateUnsignedPack(t *testing.T) {
	uc := NewUseCases(fakeAuditReader{out: sampleReplay()}, nil)
	pack, err := uc.Generate(context.Background(), "organization-1", "vp-1", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if pack.Signature.Algorithm != "none" || pack.Signature.Value != "" {
		t.Fatalf("expected an unsigned pack, got %+v", pack.Signature)
	}
}

func TestGenerateFocusedOnSubject(t *testing.T) {
	uc := NewUseCases(fakeAuditReader{out: sampleReplay()}, NewSigner("k", ""))
	pack, err := uc.Generate(context.Background(), "organization-1", "vp-1", "run-2")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if pack.EventCount != 1 || len(pack.Timeline) != 1 || pack.Timeline[0].EventHash != "h2" {
		t.Fatalf("expected only the run-2 event, got %+v", pack.Timeline)
	}
	if pack.Subject == nil || pack.Subject.ID != "run-2" || pack.Subject.ChainEventCount != 2 {
		t.Fatalf("expected subject ref with full chain count, got %+v", pack.Subject)
	}
	// integrity is still over the whole chain
	if pack.Integrity.CheckedEvents != 2 {
		t.Fatalf("expected whole-chain integrity, got %+v", pack.Integrity)
	}
}

func TestTamperedPackFailsVerification(t *testing.T) {
	uc := NewUseCases(fakeAuditReader{out: sampleReplay()}, NewSigner("super-secret", "k1"))
	pack, err := uc.Generate(context.Background(), "organization-1", "vp-1", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pack.Timeline[0].Summary = "altered after signing"
	if err := VerifyPack("super-secret", pack); err == nil {
		t.Fatal("a tampered pack must fail verification")
	}
}

func TestFocusedPackLinksAndIndependentlyProvesSpecialistChains(t *testing.T) {
	primary := sampleReplay()
	specialist := audit.ReplayOutput{
		Scope: "organization-1/vp-specialist", VirployeeID: "vp-specialist", EventCount: 2,
		Timeline: []audit.TimelineEntry{
			{Event: "specialist_consult_completed", Actor: "vp-specialist", SubjectID: "run-2", At: "2026-07-21T12:00:30Z", Summary: "opinion", EventHash: "s1"},
			{Event: "other", Actor: "vp-specialist", SubjectID: "other-run", At: "2026-07-21T12:02:00Z", Summary: "other", EventHash: "s2"},
		},
		Integrity: &audit.IntegrityOutput{Status: "ok", CheckedEvents: 2, FirstHash: "s1", LastHash: "s2", Signed: true},
	}
	reader := fakeSubjectAuditReader{primary: primary, chains: []audit.ReplayOutput{primary, specialist}}
	uc := NewUseCases(reader, NewSigner("linked-secret", "k1"))
	pack, err := uc.Generate(context.Background(), "organization-1", "vp-1", "run-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.LinkedChains) != 1 || pack.LinkedChains[0].VirployeeID != "vp-specialist" || pack.LinkedChains[0].EventCount != 1 {
		t.Fatalf("unexpected linked chains: %+v", pack.LinkedChains)
	}
	if pack.LinkedChains[0].Integrity.CheckedEvents != 2 || pack.EventCount != 2 || pack.Subject == nil || pack.Subject.ChainEventCount != 4 {
		t.Fatalf("linked integrity/counts missing: %+v", pack)
	}
	if err := VerifyPack("linked-secret", pack); err != nil {
		t.Fatalf("linked pack must reverify: %v", err)
	}
	pack.LinkedChains[0].Timeline[0].Summary = "tampered"
	if err := VerifyPack("linked-secret", pack); err == nil {
		t.Fatal("tampering a linked specialist chain must invalidate the pack signature")
	}
}
