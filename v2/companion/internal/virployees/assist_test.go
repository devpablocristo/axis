package virployees

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type fakeAssistRepo struct {
	reserved      bool
	existing      AssistRun
	beginCalls    int
	completeCalls int
	lastComplete  AssistRun
	current       AssistRun
}

func (r *fakeAssistRepo) BeginAssistRun(_ context.Context, tenant string, vid uuid.UUID, metadata AssistMetadata, idem, inputHash, _ string, input json.RawMessage) (AssistRun, bool, error) {
	r.beginCalls++
	if !r.reserved {
		return r.existing, false, nil
	}
	r.current = AssistRun{ID: uuid.New(), TenantID: tenant, VirployeeID: vid, CaseID: metadata.CaseID, AssignmentID: metadata.AssignmentID,
		AssignmentVersion: metadata.AssignmentVersion, AssistType: metadata.AssistType, ProductSurface: metadata.ProductSurface,
		SubjectID: metadata.SubjectID, RepositoryGeneration: metadata.RepositoryGeneration,
		CapabilityKey: metadata.CapabilityKey, CapabilityManifestHash: metadata.CapabilityManifestHash, GroundingMode: metadata.GroundingMode,
		ContextHash: metadata.ContextHash, JobRoleSnapshotHash: metadata.JobRoleSnapshotHash,
		SourceAuthorizationHash: metadata.SourceAuthorizationHash,
		IdempotencyKey:          idem, Status: "received", InputHash: inputHash, InputJSON: input}
	return r.current, true, nil
}

func TestClinicalAssistInputIsClosedBeforeArtifactProcessing(t *testing.T) {
	if err := validateClinicalAssistInput(CapabilityClinicalRecordsSearch, json.RawMessage(`{"query":"labs","documents":[]}`)); err == nil {
		t.Fatal("clinical search accepted a non-contract documents field")
	}
	if err := validateClinicalAssistInput(CapabilityClinicalTimelineBuild, json.RawMessage(`{"order":"desc","unknown":true}`)); err == nil {
		t.Fatal("clinical timeline accepted an unknown field")
	}
}

func TestAssistRunScopeIncludesCanonicalCapabilitySnapshot(t *testing.T) {
	metadata := AssistMetadata{CapabilityKey: CapabilityClinicalTimelineBuild, CapabilityManifestHash: strings.Repeat("a", 64)}
	run := AssistRun{CapabilityKey: metadata.CapabilityKey, CapabilityManifestHash: metadata.CapabilityManifestHash}
	if !assistRunScopeMatches(run, metadata) {
		t.Fatal("matching capability snapshot was not considered the same idempotency scope")
	}
	metadata.CapabilityManifestHash = strings.Repeat("b", 64)
	if assistRunScopeMatches(run, metadata) {
		t.Fatal("changed capability manifest reused an idempotency scope")
	}
}

func (r *fakeAssistRepo) ClaimAssistRun(_ context.Context, _ string, id uuid.UUID, recoverPreAnswer bool) (AssistRun, bool, error) {
	if r.current.ID == id && (r.current.Status == "received" || (recoverPreAnswer && (r.current.Status == "staging" || r.current.Status == "extracting" || r.current.Status == "indexing"))) {
		r.current.Status = "staging"
		return r.current, true, nil
	}
	return r.existing, false, nil
}

func (r *fakeAssistRepo) SetAssistRunStatus(_ context.Context, _ string, id uuid.UUID, status string) (AssistRun, error) {
	if r.current.ID == id {
		r.current.Status = status
		return r.current, nil
	}
	return r.existing, nil
}

func (r *fakeAssistRepo) CompleteAssistRun(_ context.Context, _ string, id uuid.UUID, status string, output json.RawMessage, outputText string, answered, degraded bool, model, pv, runErr string, dur int64) (AssistRun, error) {
	r.completeCalls++
	r.lastComplete = r.current
	r.lastComplete.ID, r.lastComplete.Status = id, status
	r.lastComplete.Output, r.lastComplete.OutputText = output, outputText
	r.lastComplete.Answered, r.lastComplete.Degraded = answered, degraded
	r.lastComplete.Model, r.lastComplete.PromptVersion, r.lastComplete.Error, r.lastComplete.DurationMS = model, pv, runErr, dur
	return r.lastComplete, nil
}

func (r *fakeAssistRepo) CompleteAssistRunWithGrounding(_ context.Context, _ string, id uuid.UUID, completion AssistCompletion) (AssistRun, error) {
	r.completeCalls++
	r.current.ID = id
	r.current.Status, r.current.Output, r.current.OutputText = completion.Status, completion.Output, completion.OutputText
	r.current.Answered, r.current.Degraded = completion.Answered, completion.Degraded
	r.current.Model, r.current.PromptVersion = completion.Model, completion.PromptVersion
	r.current.Error, r.current.DurationMS = completion.RunError, completion.DurationMS
	r.current.GroundingMode, r.current.AnswerStatus, r.current.ContextHash = completion.GroundingMode, completion.AnswerStatus, completion.ContextHash
	r.current.Citations, r.current.SourceContext = completion.Citations, completion.SourceContext
	r.current.MemoryContextHash, r.current.MemoryReferences = completion.MemoryContextHash, completion.MemoryReferences
	r.current.JobRoleSnapshotHash = completion.JobRoleSnapshotHash
	r.current.SourceAuthorizationHash = completion.SourceAuthorizationHash
	r.lastComplete = r.current
	return r.current, nil
}

func (r *fakeAssistRepo) GetAssistRunByKey(context.Context, string, uuid.UUID, string) (AssistRun, error) {
	return r.existing, nil
}

func (r *fakeAssistRepo) GetAssistRunByID(context.Context, string, uuid.UUID) (AssistRun, error) {
	if r.current.ID != uuid.Nil {
		return r.current, nil
	}
	return r.existing, nil
}

func (r *fakeAssistRepo) ListReceivedAssistRuns(context.Context, int) ([]AssistRun, error) {
	if r.current.Status == "received" {
		return []AssistRun{r.current}, nil
	}
	return nil, nil
}

type fakeAnswerer struct {
	called    bool
	lastInput AnswerInput
	out       AnswerOutput
	err       error
}

type fakeQuota struct {
	denyArea string
	calls    []quotas.ConsumeRequest
}

func (q *fakeQuota) Consume(_ context.Context, request quotas.ConsumeRequest) (quotas.Decision, error) {
	q.calls = append(q.calls, request)
	if request.Area == q.denyArea {
		decision := quotas.Decision{Allowed: false, PolicyFound: true, RetryAfterSeconds: 17}
		return decision, &quotas.ExceededError{Key: request.Key, RetryAfter: 17}
	}
	return quotas.Decision{Allowed: true, PolicyFound: true}, nil
}

type fakeUsageLedger struct{ records []quotas.Usage }

func (l *fakeUsageLedger) RecordUsage(_ context.Context, usage quotas.Usage) error {
	l.records = append(l.records, usage)
	return nil
}

func (a *fakeAnswerer) Answer(_ context.Context, in AnswerInput) (AnswerOutput, error) {
	a.called = true
	a.lastInput = in
	return a.out, a.err
}

type fakeDocFetcher struct {
	calls int
	docs  map[string]FetchedDocument
}

type fakeAssistQueue struct {
	runs []AssistRun
	err  error
}

type fakeArtifactIngestor struct {
	request artifacts.IngestRequest
	result  artifacts.IngestResult
	err     error
}

type fakeKnowledgeRetriever struct {
	evidence knowledgebases.Evidence
	err      error
}

type fakeContinuityValidator struct {
	versions         []int64
	errors           []error
	expectedVersion  []int64
	assignmentID     uuid.UUID
	subjectID        uuid.UUID
	virployeeID      uuid.UUID
	requires         bool
	requirementErr   error
	requirementCalls int
}

func (f *fakeContinuityValidator) RequiresAssistAssignment(_ context.Context, _ string, _ uuid.UUID, _ uuid.UUID) (bool, error) {
	f.requirementCalls++
	return f.requires, f.requirementErr
}

func (f *fakeContinuityValidator) ValidateAssistAssignment(_ context.Context, _ string, assignmentID, subjectID, virployeeID uuid.UUID, expectedVersion int64) (int64, error) {
	call := len(f.expectedVersion)
	f.expectedVersion = append(f.expectedVersion, expectedVersion)
	f.assignmentID, f.subjectID, f.virployeeID = assignmentID, subjectID, virployeeID
	if call < len(f.errors) && f.errors[call] != nil {
		return 0, f.errors[call]
	}
	if call < len(f.versions) {
		return f.versions[call], nil
	}
	return 1, nil
}

type trackingKnowledgeRetriever struct{ calls int }

func (f *trackingKnowledgeRetriever) Retrieve(context.Context, knowledgebases.RetrievalScope, string, int) (knowledgebases.Evidence, error) {
	f.calls++
	return knowledgebases.Evidence{}, nil
}

type fakeConversationAuthority struct {
	result executiongate.ConversationScopeResult
	err    error
	calls  int
}

func (f *fakeConversationAuthority) EvaluateAuthority(context.Context, executiongate.AuthorityCheckInput) (executiongate.AuthorityCheckResult, error) {
	return executiongate.AuthorityCheckResult{Allowed: true}, nil
}

func (f *fakeConversationAuthority) EvaluateConversationScope(_ context.Context, _ executiongate.ConversationScopeInput) (executiongate.ConversationScopeResult, error) {
	f.calls++
	return f.result, f.err
}

func (f fakeKnowledgeRetriever) Retrieve(context.Context, knowledgebases.RetrievalScope, string, int) (knowledgebases.Evidence, error) {
	return f.evidence, f.err
}

func (f *fakeArtifactIngestor) Ingest(_ context.Context, request artifacts.IngestRequest) (artifacts.IngestResult, error) {
	f.request = request
	return f.result, f.err
}

func (q *fakeAssistQueue) EnqueueAssist(_ context.Context, run AssistRun) error {
	q.runs = append(q.runs, run)
	return q.err
}

func (f *fakeDocFetcher) Fetch(_ context.Context, key, _, _ string) FetchedDocument {
	f.calls++
	if d, ok := f.docs[key]; ok {
		return d
	}
	return FetchedDocument{Key: key, Note: "not stubbed"}
}

func TestAssistProcessesAndPersistsAnswer(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	ans := &fakeAnswerer{out: AnswerOutput{OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true, ModelID: "gemini", PromptVersion: "answer.v1"}}
	uc.assistRepo = repo
	uc.answerer = ans

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"documents":[]}`), "idem-1", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if !ans.called {
		t.Fatal("expected the answerer to be called")
	}
	if run.Status != "done" || !run.Answered || run.Degraded {
		t.Fatalf("expected a completed answered run, got %+v", run)
	}
	if string(run.Output) != `{"summary":"ok"}` || repo.completeCalls != 1 {
		t.Fatalf("expected the model output persisted once, got %+v (completes=%d)", run, repo.completeCalls)
	}
}

func TestAssistBindsAndRevalidatesStableAssignment(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	row := uc.repo.(*fakeRepo).rows[created.ID]
	row.GroundingMode = domain.GroundingGeneral
	uc.repo.(*fakeRepo).rows[created.ID] = row
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true, Status: "answered"}}
	validator := &fakeContinuityValidator{versions: []int64{7, 7}}
	uc.assistRepo, uc.answerer = repo, answerer
	uc.SetContinuityAssignmentValidator(validator)
	subjectID, assignmentID := uuid.New(), uuid.New()

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"question":"hello"}`), "stable-assignment", AssistMetadata{
		SubjectID: subjectID.String(), AssignmentID: assignmentID,
	})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if len(validator.expectedVersion) != 2 || validator.expectedVersion[0] != 0 || validator.expectedVersion[1] != 7 {
		t.Fatalf("assignment must be resolved then revalidated: %+v", validator.expectedVersion)
	}
	if validator.assignmentID != assignmentID || validator.subjectID != subjectID || validator.virployeeID != created.ID {
		t.Fatalf("wrong assignment scope was validated: %+v", validator)
	}
	if run.AssignmentID != assignmentID || run.AssignmentVersion != 7 || run.ContextHash == "" || !answerer.called {
		t.Fatalf("assignment/context was not preserved: %+v", run)
	}
}

func TestAssistCannotOmitAssignmentWhenRoutingIsAuthoritative(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	validator := &fakeContinuityValidator{requires: true}
	uc.assistRepo, uc.answerer = repo, &fakeAnswerer{}
	uc.SetContinuityAssignmentValidator(validator)

	_, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"question":"hello"}`), "routing-bypass", AssistMetadata{
		SubjectID: uuid.NewString(),
	})
	if !domainerr.IsConflict(err) {
		t.Fatalf("routed Assist without assignment_id must be rejected, got %v", err)
	}
	if validator.requirementCalls != 1 || repo.beginCalls != 0 {
		t.Fatalf("routing must be checked before reserving work: validator=%d begin=%d", validator.requirementCalls, repo.beginCalls)
	}
}

func TestAssistStopsBeforeContextWhenAssignmentChanged(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	validator := &fakeContinuityValidator{
		versions: []int64{2},
		errors:   []error{nil, domainerr.Conflict("assignment changed")},
	}
	uc.assistRepo, uc.answerer = repo, answerer
	uc.SetContinuityAssignmentValidator(validator)

	_, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"question":"hello"}`), "changed-assignment", AssistMetadata{
		SubjectID: uuid.NewString(), AssignmentID: uuid.New(),
	})
	if !domainerr.IsConflict(err) || answerer.called {
		t.Fatalf("changed assignment must fail before runtime, err=%v called=%v", err, answerer.called)
	}
	if repo.lastComplete.Error != "assignment_changed" {
		t.Fatalf("expected stable failure code, got %+v", repo.lastComplete)
	}
}

func TestAssistSourcesOnlyAbstainsWithoutEvidenceBeforeRuntime(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	row := uc.repo.(*fakeRepo).rows[created.ID]
	row.GroundingMode = domain.GroundingSourcesOnly
	uc.repo.(*fakeRepo).rows[created.ID] = row
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	uc.assistRepo, uc.answerer = repo, answerer

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"question":"unknown"}`), "sources-none", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if answerer.called || run.Answered || run.AnswerStatus != "abstained" || run.Degraded {
		t.Fatalf("expected non-degraded abstention before runtime, got %+v called=%v", run, answerer.called)
	}
}

func TestAssistScopePolicyStopsBeforeKnowledgeAndRuntime(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	retriever := &trackingKnowledgeRetriever{}
	authority := &fakeConversationAuthority{result: executiongate.ConversationScopeResult{
		Allowed: false, Decision: "abstain", Reason: "prohibited_topic",
	}}
	uc.assistRepo, uc.answerer, uc.knowledge = repo, answerer, retriever
	uc.SetAuthorityEvaluator(authority)

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"question":"tema prohibido"}`), "scope-abstain", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if authority.calls != 1 || retriever.calls != 0 || answerer.called {
		t.Fatalf("scope must be evaluated before knowledge/runtime: authority=%d retrieval=%d runtime=%v", authority.calls, retriever.calls, answerer.called)
	}
	if run.Status != "done" || run.AnswerStatus != "abstained" || run.Answered || run.Degraded {
		t.Fatalf("expected safe non-degraded abstention, got %+v", run)
	}
	if strings.Contains(string(run.Output), "tema prohibido") || !strings.Contains(run.OutputText, "fuera de mi alcance") {
		t.Fatalf("scope response must not echo the query, got output=%s text=%q", run.Output, run.OutputText)
	}
}

func TestAssistScopePolicyEscalatesWithoutCallingRuntime(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	authority := &fakeConversationAuthority{result: executiongate.ConversationScopeResult{
		Allowed: false, Decision: "escalate", Reason: "outside_allowed_topics",
	}}
	uc.assistRepo, uc.answerer = repo, answerer
	uc.SetAuthorityEvaluator(authority)

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"question":"fuera del area"}`), "scope-escalate", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if answerer.called || run.Status != AssistStatusNeedsHuman || run.AnswerStatus != "escalation_required" {
		t.Fatalf("expected human escalation before runtime, got %+v called=%v", run, answerer.called)
	}
}

func TestAssistScopeEvaluationFailureFailsClosed(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	uc.assistRepo, uc.answerer = repo, answerer
	uc.SetAuthorityEvaluator(&fakeConversationAuthority{err: errors.New("database unavailable")})

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"question":"agenda"}`), "scope-error", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if answerer.called || run.AnswerStatus != "abstained" || !strings.Contains(string(run.Output), "scope_evaluation_unavailable") {
		t.Fatalf("scope evaluator failure must abstain before runtime, got %+v called=%v", run, answerer.called)
	}
}

func TestAssistSourcesOnlyCanonicalizesValidCitation(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	row := uc.repo.(*fakeRepo).rows[created.ID]
	row.GroundingMode = domain.GroundingSourcesOnly
	uc.repo.(*fakeRepo).rows[created.ID] = row
	documentID, baseID := uuid.NewString(), uuid.New()
	sha := strings.Repeat("a", 64)
	canonical := knowledgebases.Citation{KnowledgeBaseID: &baseID, DocumentID: documentID, SourceVersion: "v1", SHA256: sha, Locator: json.RawMessage(`{"page":2}`)}
	uc.knowledge = fakeKnowledgeRetriever{evidence: knowledgebases.Evidence{
		Parts:     []artifacts.ContentPart{{Kind: artifacts.PartText, Text: "Verified fact", DocumentID: documentID, SHA256: sha, Locator: &artifacts.Locator{Page: 2}}},
		Citations: []knowledgebases.Citation{canonical},
	}}
	answerer := &fakeAnswerer{out: AnswerOutput{
		OutputJSON: json.RawMessage(`{"status":"answered","answer":"Verified fact","citations":[{"document_id":"` + documentID + `"}]}`),
		Answered:   true, Status: "answered", Citations: []RuntimeCitation{{DocumentID: documentID}},
	}}
	uc.assistRepo, uc.answerer = &fakeAssistRepo{reserved: true}, answerer

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"question":"fact?"}`), "sources-cited", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if !run.Answered || run.AnswerStatus != "answered" || len(run.Citations) != 1 || run.Citations[0].DocumentID != documentID {
		t.Fatalf("expected canonical grounded citation, got %+v", run)
	}
	if run.Citations[0].KnowledgeBaseID == nil || *run.Citations[0].KnowledgeBaseID != baseID {
		t.Fatalf("expected canonical knowledge base id, got %+v", run.Citations)
	}
}

func TestAssistStopsBeforeRuntimeWhenLLMQuotaIsExceeded(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	quota := &fakeQuota{denyArea: quotas.AreaLLM}
	uc.assistRepo = repo
	uc.answerer = answerer
	uc.SetQuotaPorts(quota, nil)

	_, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"documents":[]}`), "quota-run", AssistMetadata{ProductSurface: "medmory"})
	if retryAfter, ok := quotas.RetryAfter(err); !ok || retryAfter != 17 {
		t.Fatalf("expected quota error with Retry-After 17, got %v", err)
	}
	if answerer.called {
		t.Fatal("runtime must not be called after quota denial")
	}
	if len(quota.calls) != 2 || quota.calls[0].Area != quotas.AreaInbound || quota.calls[1].Area != quotas.AreaLLM {
		t.Fatalf("unexpected quota sequence: %+v", quota.calls)
	}
}

func TestAssistRecordsRuntimeUsageWithoutPromptContent(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{
		OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true, ModelID: "gemini",
		InputTokens: 11, OutputTokens: 7, EstimatedCostMicroUSD: 3,
	}}
	ledger := &fakeUsageLedger{}
	uc.assistRepo = repo
	uc.answerer = answerer
	uc.SetQuotaPorts(&fakeQuota{}, ledger)

	if _, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"secret_note":"never persist this"}`), "usage-run", AssistMetadata{ProductSurface: "medmory"}); err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if len(ledger.records) != 1 {
		t.Fatalf("expected one actual usage record, got %d", len(ledger.records))
	}
	usage := ledger.records[0]
	if usage.Area != quotas.AreaLLM || usage.Units != 18 || usage.Model != "gemini" || usage.EstimatedCostMicroUSD != 3 {
		t.Fatalf("unexpected usage record: %+v", usage)
	}
	encoded, _ := json.Marshal(usage.Metadata)
	if strings.Contains(string(encoded), "never persist this") {
		t.Fatalf("usage metadata must not contain prompt content: %s", encoded)
	}
}

func TestAssistFetchesDocumentsAndPassesContentToModel(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	ans := &fakeAnswerer{out: AnswerOutput{OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true}}
	fetcher := &fakeDocFetcher{docs: map[string]FetchedDocument{
		"labs.txt": {Key: "labs.txt", ContentType: "text/plain", Content: "Glucosa 126 mg/dL", Readable: true},
	}}
	uc.assistRepo = repo
	uc.answerer = ans
	uc.docFetcher = fetcher

	input := json.RawMessage(`{"schema_version":"medmory.diagnosis_input.v1","documents":[{"key":"labs.txt","read_url":"https://x/labs","content_type":"text/plain"}]}`)
	if _, err := uc.Assist(context.Background(), "tenant-1", created.ID, input, "idem-docs", AssistMetadata{}); err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected the document to be fetched once, got %d", fetcher.calls)
	}
	// The model must receive the fetched CONTENT, not the raw read_url reference.
	got := string(ans.lastInput.InputJSON)
	if !strings.Contains(got, "Glucosa 126 mg/dL") {
		t.Fatalf("model input must contain the fetched content, got %s", got)
	}
	if strings.Contains(got, "read_url") || strings.Contains(got, "https://x/labs") {
		t.Fatalf("model input must not leak the presigned read_url, got %s", got)
	}
}

func TestAssistUsesArtifactPipelineAndNeverForwardsSignedURLToRuntime(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true}}
	legacyFetcher := &fakeDocFetcher{}
	ingestor := &fakeArtifactIngestor{result: artifacts.IngestResult{Parts: []artifacts.ContentPart{{
		Kind: artifacts.PartText, Text: "Glucosa 126 mg/dL", DocumentID: "doc-1", SHA256: "abc",
	}}}}
	uc.assistRepo = repo
	uc.answerer = answerer
	uc.docFetcher = legacyFetcher
	uc.artifactIngestor = ingestor

	input := json.RawMessage(`{"documents":[{"document_id":"doc-1","key":"labs.pdf","read_url":"https://signed.example/labs?secret=yes","content_type":"application/pdf","sha256":"abc","size_bytes":120}]}`)
	metadata := AssistMetadata{AssistType: "clinical_diagnosis", ProductSurface: "medmory", SubjectID: "patient-a", RepositoryGeneration: "generation-a"}
	if _, err := uc.Assist(context.Background(), "tenant-1", created.ID, input, "idem-artifacts", metadata); err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if legacyFetcher.calls != 0 {
		t.Fatal("legacy document fetcher must be bypassed when artifact ingestion is configured")
	}
	if ingestor.request.Scope.SubjectID != "patient-a" || ingestor.request.Scope.RepositoryGeneration != "generation-a" || len(ingestor.request.Artifacts) != 1 || !ingestor.request.Artifacts[0].Required {
		t.Fatalf("artifact manifest scope not propagated: %+v", ingestor.request)
	}
	if len(answerer.lastInput.ContentParts) != 1 || answerer.lastInput.ContentParts[0].Text != "Glucosa 126 mg/dL" {
		t.Fatalf("verified content parts not propagated: %+v", answerer.lastInput.ContentParts)
	}
	if got := string(answerer.lastInput.InputJSON); strings.Contains(got, "signed.example") || strings.Contains(got, "read_url") {
		t.Fatalf("runtime input must not contain signed URL after staging: %s", got)
	}
}

func TestAssistMarksDegradedWhenModelDidNotAnswer(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.assistRepo = &fakeAssistRepo{reserved: true}
	uc.answerer = &fakeAnswerer{out: AnswerOutput{OutputText: "Recibido (modo echo).", Answered: false}}

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "idem-2", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if run.Status != "done" || run.Answered || !run.Degraded {
		t.Fatalf("Echo/no-answer must yield a degraded run, got %+v", run)
	}
}

func TestAssistReplayReturnsExistingRunWithoutCallingModel(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	input := json.RawMessage(`{"x":1}`)
	existing := AssistRun{ID: uuid.New(), Status: "done", Answered: true, InputHash: runtraces.HashString(string(input)), Output: json.RawMessage(`{"summary":"prev"}`)}
	repo := &fakeAssistRepo{reserved: false, existing: existing}
	ans := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	uc.assistRepo = repo
	uc.answerer = ans

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, input, "idem-dup", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if ans.called {
		t.Fatal("a completed run must NOT re-invoke the model")
	}
	if run.ID != existing.ID || string(run.Output) != `{"summary":"prev"}` {
		t.Fatalf("replay must return the stored run, got %+v", run)
	}
}

func TestSubmitAssistAsyncPersistsAndQueuesIdentifiersWithoutCallingModel(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	queue := &fakeAssistQueue{}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	uc.assistRepo = repo
	uc.assistQueue = queue
	uc.answerer = answerer

	run, err := uc.SubmitAssistAsync(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"document_id":"doc-1"}`), "manifest-generation-1", AssistMetadata{})
	if err != nil {
		t.Fatalf("SubmitAssistAsync: %v", err)
	}
	if run.Status != "received" || len(queue.runs) != 1 || queue.runs[0].ID != run.ID {
		t.Fatalf("expected one received run queued, run=%+v queue=%+v", run, queue.runs)
	}
	if answerer.called {
		t.Fatal("request path must not invoke the model")
	}
}

func TestRequeueReceivedAssistRunsClosesPersistEnqueueGap(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	queue := &fakeAssistQueue{}
	uc.assistRepo = repo
	uc.assistQueue = queue
	uc.answerer = &fakeAnswerer{}

	run, _, err := uc.SubmitAssist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "generation-a", AssistMetadata{})
	if err != nil {
		t.Fatalf("SubmitAssist: %v", err)
	}
	count, err := uc.RequeueReceivedAssistRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("RequeueReceivedAssistRuns: %v", err)
	}
	if count != 1 || len(queue.runs) != 1 || queue.runs[0].ID != run.ID {
		t.Fatalf("expected persisted run to be requeued: count=%d runs=%+v", count, queue.runs)
	}
}

func TestProcessAssistRunRecoversPreAnswerStateOnDurableJobRetry(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{OutputJSON: json.RawMessage(`{"summary":"recovered"}`), Answered: true}}
	uc.assistRepo = repo
	uc.answerer = answerer
	run, _, err := uc.SubmitAssist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"documents":[]}`), "generation-a", AssistMetadata{})
	if err != nil {
		t.Fatalf("SubmitAssist: %v", err)
	}
	repo.current.Status = "extracting" // previous worker died before the model call
	completed, err := uc.ProcessAssistRun(context.Background(), "tenant-1", run.ID, true)
	if err != nil {
		t.Fatalf("ProcessAssistRun recovery: %v", err)
	}
	if completed.Status != "done" || !answerer.called {
		t.Fatalf("expected safe pre-answer recovery, got %+v called=%v", completed, answerer.called)
	}
}

func TestProcessAssistRunNeverReplaysStaleAnsweringState(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	uc.assistRepo = repo
	uc.answerer = answerer
	run, _, err := uc.SubmitAssist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"documents":[]}`), "generation-a", AssistMetadata{})
	if err != nil {
		t.Fatalf("SubmitAssist: %v", err)
	}
	repo.current.Status = "answering"
	_, err = uc.ProcessAssistRun(context.Background(), "tenant-1", run.ID, true)
	if !domainerr.IsConflict(err) || answerer.called {
		t.Fatalf("answering must not be replayed, err=%v called=%v", err, answerer.called)
	}
}

func TestSubmitAssistRejectsIdempotencyKeyReusedWithDifferentInput(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.assistRepo = &fakeAssistRepo{reserved: false, existing: AssistRun{ID: uuid.New(), Status: "done", InputHash: runtraces.HashString(`{"x":1}`)}}
	uc.answerer = &fakeAnswerer{}

	_, _, err := uc.SubmitAssist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":2}`), "generation-a", AssistMetadata{})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestAssistFailsClosedOnRuntimeError(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	uc.assistRepo = repo
	uc.answerer = &fakeAnswerer{err: errors.New("runtime down")}

	_, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "idem-3", AssistMetadata{})
	if err == nil {
		t.Fatal("expected an error when the runtime fails")
	}
	if repo.lastComplete.Status != "failed" || repo.lastComplete.Error == "" {
		t.Fatalf("a runtime failure must be recorded as failed with an error, got %+v", repo.lastComplete)
	}
}

func TestAssistRequiresAnswerer(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.assistRepo = &fakeAssistRepo{reserved: true}
	// answerer intentionally nil
	_, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "k", AssistMetadata{})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict when the answerer is not configured, got %v", err)
	}
}
