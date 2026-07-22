package memories

import (
	"testing"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestNormalizeAndHashAreDeterministic(t *testing.T) {
	subjectID := uuid.NewString()
	in, err := normalizeCreate(CreateInput{Title: "  Time zone ", Type: "PREFERENCE", Content: "  America/Argentina/Buenos_Aires  ", ActorID: "user_1", Scope: Scope{Type: ScopeSubject, SubjectID: subjectID}})
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Time zone" || in.Type != "preference" || in.Sensitivity != "normal" || in.Provenance != "human" {
		t.Fatalf("unexpected normalization: %+v", in)
	}
	if ContentHash(in.Content) != ContentHash(" America/Argentina/Buenos_Aires ") {
		t.Fatal("hash must ignore surrounding whitespace")
	}
}

func TestNormalizeCreateReservesVirployeeScopeForProcedures(t *testing.T) {
	_, err := normalizeCreate(CreateInput{Title: "Patient preference", Type: "preference", Content: "Morning appointments", ActorID: "user_1"})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected personal global memory rejection, got %v", err)
	}
	if _, err := normalizeCreate(CreateInput{Title: "Triage procedure", Type: "procedure", Content: "Verify identity before opening a chart", ActorID: "user_1"}); err != nil {
		t.Fatalf("expected non-personal procedure to allow Virployee scope: %v", err)
	}
}

func TestMemoryCursorRoundTrip(t *testing.T) {
	wantID := uuid.New()
	wantTime := time.Date(2026, 7, 13, 12, 34, 56, 789, time.UTC)
	encoded, err := encodeMemoryCursor(Memory{ID: wantID, UpdatedAt: wantTime})
	if err != nil {
		t.Fatalf("encodeMemoryCursor: %v", err)
	}
	gotTime, gotID, ok, err := decodeMemoryCursor(encoded)
	if err != nil {
		t.Fatalf("decodeMemoryCursor: %v", err)
	}
	if !ok || gotID != wantID || !gotTime.Equal(wantTime) {
		t.Fatalf("cursor mismatch: ok=%v id=%s time=%s", ok, gotID, gotTime)
	}
}

func TestMemoryCursorRejectsMalformedValue(t *testing.T) {
	if _, _, _, err := decodeMemoryCursor("not-a-cursor"); err == nil {
		t.Fatal("expected malformed cursor to fail")
	}
}

func TestNormalizeRejectsInvalidEnums(t *testing.T) {
	_, err := normalizeCreate(CreateInput{Title: "x", Type: "belief", Content: "y", ActorID: "actor"})
	if err == nil {
		t.Fatal("expected invalid type")
	}
}

func TestNormalizeScopeRequiresExactSubjectAndCase(t *testing.T) {
	caseID := uuid.New()
	subjectID := uuid.New()
	got, err := NormalizeScope(Scope{Type: ScopeCase, SubjectID: " " + subjectID.String() + " ", CaseID: &caseID})
	if err != nil {
		t.Fatalf("NormalizeScope: %v", err)
	}
	if got.SubjectID != subjectID.String() || got.CaseID == nil || *got.CaseID != caseID {
		t.Fatalf("unexpected scope: %+v", got)
	}
	if _, err := NormalizeScope(Scope{Type: ScopeSubject, SubjectID: subjectID.String(), CaseID: &caseID}); !domainerr.IsValidation(err) {
		t.Fatalf("subject scope must reject case_id, got %v", err)
	}
	if _, err := NormalizeScope(Scope{Type: ScopeCase, SubjectID: subjectID.String()}); !domainerr.IsValidation(err) {
		t.Fatalf("case scope must require case_id, got %v", err)
	}
	if _, err := NormalizeScope(Scope{Type: ScopeSubject, SubjectID: "patient-a"}); !domainerr.IsValidation(err) {
		t.Fatalf("subject scope must require a UUID subject_id, got %v", err)
	}
}

func TestRedactSensitiveOutsideDetail(t *testing.T) {
	m := redact(Memory{ID: uuid.New(), Content: "secret", Sensitivity: "sensitive"}, false)
	if m.Content != "" || m.Preview != "[REDACTED]" {
		t.Fatalf("sensitive content leaked: %+v", m)
	}
	detail := redact(Memory{Content: "secret", Sensitivity: "sensitive"}, true)
	if detail.Content != "secret" {
		t.Fatal("authorized detail must preserve content")
	}
}

func TestContextHashIsDeterministicAndVersionBound(t *testing.T) {
	id := uuid.New()
	refs := []Reference{{ID: id, Title: "x", Type: "fact", Version: 1, Hash: "abc", Score: .5}}
	a, b := ContextHash(refs), ContextHash(refs)
	if a != b || a == "" {
		t.Fatal("context hash is not deterministic")
	}
	refs[0].Version++
	if ContextHash(refs) == a {
		t.Fatal("context hash must bind the version")
	}
}

func TestSafeForPromptRejectsSensitivePoisonedConflictingOrExpiredMemory(t *testing.T) {
	base := Memory{State: "active", ReviewState: ReviewApproved, TrustScore: .9, Sensitivity: "normal"}
	if !safeForPrompt(base) {
		t.Fatal("expected approved normal memory to be prompt-safe")
	}
	cases := []Memory{
		func() Memory { value := base; value.Sensitivity = "sensitive"; return value }(),
		func() Memory { value := base; value.PoisoningFlags = []string{"instruction_override"}; return value }(),
		func() Memory { value := base; value.ReviewReason = "conflicting_memory_requires_review"; return value }(),
		func() Memory { value := base; value.ReviewState = ReviewPending; return value }(),
		func() Memory {
			value := base
			expired := time.Now().Add(-time.Minute)
			value.ExpiresAt = &expired
			return value
		}(),
	}
	for index, memory := range cases {
		if safeForPrompt(memory) {
			t.Fatalf("unsafe memory case %d passed the prompt gate: %+v", index, memory)
		}
	}
}
