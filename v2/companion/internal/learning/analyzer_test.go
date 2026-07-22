package learning

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/google/uuid"
)

func TestShouldProposeRules(t *testing.T) {
	dismissed := func(seen int64) *Proposal {
		return &Proposal{Status: StatusDismissed, SucceededWatermark: seen}
	}
	cases := []struct {
		name      string
		latest    *Proposal
		succeeded int64
		want      bool
	}{
		{"no prior proposal proposes", nil, 3, true},
		{"pending blocks", &Proposal{Status: StatusPending}, 99, false},
		{"accepted blocks (skill already learned)", &Proposal{Status: StatusAccepted}, 99, false},
		{"dismissed without new evidence blocks", dismissed(5), 5, false},
		{"dismissed with new evidence proposes", dismissed(5), 6, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldPropose(tc.latest, tc.succeeded); got != tc.want {
				t.Fatalf("ShouldPropose = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDistillNeverLeaksPayloadValues(t *testing.T) {
	title, content := Distill(Candidate{
		VirployeeID:   uuid.NewString(),
		CapabilityKey: "calendar.events.create",
		Succeeded:     5,
		FirstAt:       time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		LastAt:        time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC),
	})
	if !strings.Contains(title, "calendar.events.create") {
		t.Fatalf("title should reference the capability, got %q", title)
	}
	// The template is structural: it must never embed draft values (emails,
	// titles, attendees) from the executions it distills.
	if strings.Contains(content, "@") {
		t.Fatalf("distilled content must not contain payload-looking values: %q", content)
	}
	if !strings.Contains(content, "5 successful executions") || !strings.Contains(content, "governance decision") {
		t.Fatalf("unexpected distilled content: %q", content)
	}
}

type fakeLearningRepo struct {
	candidates    []Candidate
	latest        map[string]*Proposal // key: virployee|capability
	created       []NormalizedCreateInput
	lastThreshold int
}

func (f *fakeLearningRepo) Create(_ context.Context, _ string, input NormalizedCreateInput) (Proposal, error) {
	f.created = append(f.created, input)
	return Proposal{ID: uuid.New(), Status: StatusPending, CapabilityKey: input.CapabilityKey, VirployeeID: input.VirployeeID}, nil
}
func (f *fakeLearningRepo) List(context.Context, string, string, *uuid.UUID) ([]Proposal, error) {
	return nil, nil
}
func (f *fakeLearningRepo) Get(context.Context, string, uuid.UUID) (Proposal, error) {
	return Proposal{}, nil
}
func (f *fakeLearningRepo) Candidates(_ context.Context, _ string, minExecutions int) ([]Candidate, error) {
	f.lastThreshold = minExecutions
	return f.candidates, nil
}
func (f *fakeLearningRepo) LatestForPair(_ context.Context, _ string, virployeeID uuid.UUID, capabilityKey string) (*Proposal, error) {
	return f.latest[virployeeID.String()+"|"+capabilityKey], nil
}
func (f *fakeLearningRepo) Decide(context.Context, string, uuid.UUID, string, string, *uuid.UUID) (Proposal, error) {
	return Proposal{}, nil
}
func (f *fakeLearningRepo) AttachMemory(context.Context, string, uuid.UUID, uuid.UUID) (Proposal, error) {
	return Proposal{}, nil
}
func (f *fakeLearningRepo) SuccessfulExecutionTraceIDs(context.Context, string, uuid.UUID, string, int) ([]string, error) {
	return []string{"trace-a", "trace-b"}, nil
}

func TestScanProposesOnlyForNewPairs(t *testing.T) {
	learnable := uuid.New()
	alreadyPending := uuid.New()
	now := time.Now().UTC()
	repo := &fakeLearningRepo{
		candidates: []Candidate{
			{VirployeeID: learnable.String(), CapabilityKey: "calendar.events.create", Succeeded: 5, FirstAt: now, LastAt: now},
			{VirployeeID: alreadyPending.String(), CapabilityKey: "calendar.events.create", Succeeded: 7, FirstAt: now, LastAt: now},
		},
		latest: map[string]*Proposal{
			alreadyPending.String() + "|calendar.events.create": {Status: StatusPending},
		},
	}
	ucs := NewUseCases(repo)

	result, err := ucs.Scan(context.Background(), "organization-1", 0)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.Candidates != 2 || result.Proposed != 1 || result.Skipped != 1 {
		t.Fatalf("unexpected scan result: %+v", result)
	}
	if len(repo.created) != 1 || repo.created[0].VirployeeID != learnable {
		t.Fatalf("expected one proposal for the learnable pair, got %+v", repo.created)
	}
	if repo.created[0].ProposedBy != ProposedByAnalyzer {
		t.Fatalf("analyzer proposals must be marked analyzer, got %q", repo.created[0].ProposedBy)
	}
	if len(repo.created[0].SourceTraceIDs) != 2 {
		t.Fatalf("expected provenance trace ids, got %+v", repo.created[0].SourceTraceIDs)
	}
}

func TestScanClampsOverrideToConfiguredFloor(t *testing.T) {
	repo := &fakeLearningRepo{}
	ucs := NewUseCases(repo) // configured floor: default 3

	// An override BELOW the floor must be clamped up: thresholds are
	// governance configuration and callers cannot lower them (gate G4.1).
	if result, err := ucs.Scan(context.Background(), "organization-1", 1); err != nil || result.Threshold != 3 || repo.lastThreshold != 3 {
		t.Fatalf("expected floor 3 to win over override 1, got result=%+v lastThreshold=%d err=%v", result, repo.lastThreshold, err)
	}
	// An override ABOVE the floor is honored (stricter is allowed).
	if result, err := ucs.Scan(context.Background(), "organization-1", 5); err != nil || result.Threshold != 5 || repo.lastThreshold != 5 {
		t.Fatalf("expected stricter override 5 to be honored, got result=%+v lastThreshold=%d err=%v", result, repo.lastThreshold, err)
	}
}

func TestScanWatermarkTravelsTyped(t *testing.T) {
	learnable := uuid.New()
	now := time.Now().UTC()
	repo := &fakeLearningRepo{candidates: []Candidate{{VirployeeID: learnable.String(), CapabilityKey: "calendar.events.create", Succeeded: 5, FirstAt: now, LastAt: now}}}
	ucs := NewUseCases(repo)
	if _, err := ucs.Scan(context.Background(), "organization-1", 0); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(repo.created) != 1 || repo.created[0].SucceededWatermark != 5 {
		t.Fatalf("expected typed watermark 5 on the proposal, got %+v", repo.created)
	}
}

func TestScanRejectsInvalidThreshold(t *testing.T) {
	ucs := NewUseCases(&fakeLearningRepo{})
	ucs.SetMinExecutions(0) // ignored: keeps default
	if _, err := ucs.Scan(context.Background(), "organization-1", -1); err == nil {
		t.Fatal("expected validation error for negative threshold")
	}
}

// --- PR5: optional LLM enricher in Scan ---

type fakeEnricher struct {
	out    EnrichOutput
	err    error
	called int
}

func (f *fakeEnricher) Enrich(context.Context, EnrichInput) (EnrichOutput, error) {
	f.called++
	return f.out, f.err
}

func scanOneCandidate(t *testing.T, enricher ProcedureEnricher) (*fakeLearningRepo, ScanResult) {
	t.Helper()
	now := time.Now().UTC()
	repo := &fakeLearningRepo{candidates: []Candidate{
		{VirployeeID: uuid.NewString(), CapabilityKey: "calendar.events.create", Succeeded: 5, FirstAt: now, LastAt: now},
	}}
	ucs := NewUseCases(repo)
	if enricher != nil {
		ucs.SetProcedureEnricher(enricher)
	}
	result, err := ucs.Scan(context.Background(), "organization-1", 0)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return repo, result
}

func TestScanUsesEnrichmentWhenUsable(t *testing.T) {
	enricher := &fakeEnricher{out: EnrichOutput{
		Title:    "Cómo agendar una reunión",
		Content:  "Procedimiento para calendar.events.create:\n1. Confirmar datos.\n2. Pasar por el gate.",
		Enriched: true, ModelID: "gemini-x", PromptVersion: "enrich.v1",
	}}
	repo, _ := scanOneCandidate(t, enricher)
	if enricher.called != 1 || len(repo.created) != 1 {
		t.Fatalf("expected one enrich + one proposal, got called=%d created=%d", enricher.called, len(repo.created))
	}
	got := repo.created[0]
	if got.ProposedBy != ProposedByLLM {
		t.Fatalf("expected proposed_by llm, got %q", got.ProposedBy)
	}
	if got.Title != "Cómo agendar una reunión" || got.Evidence["enriched_by_model"] != "gemini-x" {
		t.Fatalf("expected enriched text + model evidence, got %+v", got)
	}
}

type denyingLearningQuota struct{}

func (denyingLearningQuota) Consume(_ context.Context, request quotas.ConsumeRequest) (quotas.Decision, error) {
	return quotas.Decision{Allowed: false, PolicyFound: true, RetryAfterSeconds: 9}, &quotas.ExceededError{Key: request.Key, RetryAfter: 9}
}

func TestScanDoesNotCallEnricherWhenLLMQuotaIsExceeded(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeLearningRepo{candidates: []Candidate{{
		VirployeeID: uuid.NewString(), CapabilityKey: "calendar.events.create", Succeeded: 5, FirstAt: now, LastAt: now,
	}}}
	enricher := &fakeEnricher{out: EnrichOutput{Title: "should not run", Content: "calendar.events.create", Enriched: true}}
	ucs := NewUseCases(repo)
	ucs.SetProcedureEnricher(enricher)
	ucs.SetQuotaPorts(denyingLearningQuota{}, nil)

	result, err := ucs.Scan(context.Background(), "organization-1", 0)
	if err != nil || result.Proposed != 1 || enricher.called != 0 || repo.created[0].ProposedBy != ProposedByAnalyzer {
		t.Fatalf("quota denial must fall back without a paid call: result=%+v called=%d created=%+v err=%v", result, enricher.called, repo.created, err)
	}
}

func TestScanFallsBackWhenEnricherErrors(t *testing.T) {
	enricher := &fakeEnricher{err: context.DeadlineExceeded}
	repo, _ := scanOneCandidate(t, enricher)
	if len(repo.created) != 1 || repo.created[0].ProposedBy != ProposedByAnalyzer {
		t.Fatalf("an enricher error must fall back to the deterministic analyzer text, got %+v", repo.created)
	}
}

func TestScanFallsBackWhenNotEnriched(t *testing.T) {
	// Enriched=false (the Echo/no-model default whenever the enricher is wired):
	// must file the deterministic distillation as analyzer, never the model text.
	enricher := &fakeEnricher{out: EnrichOutput{Title: "ignored", Content: "ignored", Enriched: false}}
	repo, _ := scanOneCandidate(t, enricher)
	if len(repo.created) != 1 || repo.created[0].ProposedBy != ProposedByAnalyzer {
		t.Fatalf("Enriched=false must fall back to analyzer, got %+v", repo.created)
	}
	if !strings.Contains(repo.created[0].Content, "calendar.events.create") {
		t.Fatalf("fallback content must be the deterministic distillation, got %q", repo.created[0].Content)
	}
}

func TestScanFallsBackWhenEnrichmentHasSecrets(t *testing.T) {
	// Defense in depth: an enriched rewrite carrying a secret/PII must be rejected
	// before it reaches the DB/inbox, not only blocked later at Accept.
	enricher := &fakeEnricher{out: EnrichOutput{
		Title:    "calendar.events.create",
		Content:  "Use calendar.events.create. token: DUMMY_TEST_MARKER",
		Enriched: true,
	}}
	repo, _ := scanOneCandidate(t, enricher)
	if len(repo.created) != 1 || repo.created[0].ProposedBy != ProposedByAnalyzer {
		t.Fatalf("a rewrite with a secret must fall back to analyzer, got %+v", repo.created)
	}
	if strings.Contains(repo.created[0].Content, "DUMMY_TEST_MARKER") {
		t.Fatalf("secret-bearing model text must never be filed, got %q", repo.created[0].Content)
	}
}

func TestScanFallsBackWhenEnrichmentUnusable(t *testing.T) {
	// Enriched=true but the text drops the capability_key (e.g. an Echo reply):
	// must fall back to the deterministic distillation, not file garbage.
	enricher := &fakeEnricher{out: EnrichOutput{Title: "Hi", Content: "Recibido (modo echo).", Enriched: true}}
	repo, _ := scanOneCandidate(t, enricher)
	if len(repo.created) != 1 || repo.created[0].ProposedBy != ProposedByAnalyzer {
		t.Fatalf("an unusable rewrite must fall back to analyzer, got %+v", repo.created)
	}
	if !strings.Contains(repo.created[0].Content, "calendar.events.create") {
		t.Fatalf("fallback content must be the deterministic distillation, got %q", repo.created[0].Content)
	}
}

func TestUsableEnrichment(t *testing.T) {
	ok := EnrichOutput{Title: "T", Content: "does calendar.events.create"}
	if !usableEnrichment(ok, "calendar.events.create") {
		t.Fatal("expected a within-limits, key-anchored rewrite to be usable")
	}
	if usableEnrichment(EnrichOutput{Title: "T", Content: "no key here"}, "calendar.events.create") {
		t.Fatal("content without the capability_key must be rejected")
	}
	if usableEnrichment(EnrichOutput{Title: "", Content: "calendar.events.create"}, "calendar.events.create") {
		t.Fatal("empty title must be rejected")
	}
	if usableEnrichment(EnrichOutput{Title: strings.Repeat("a", 201), Content: "calendar.events.create"}, "calendar.events.create") {
		t.Fatal("oversized title must be rejected")
	}
}
