package evidence

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	approvaldomain "github.com/devpablocristo/nexus/governance/internal/approvals/usecases/domain"
	auditdomain "github.com/devpablocristo/nexus/governance/internal/audit/usecases/domain"
	evidencedomain "github.com/devpablocristo/nexus/governance/internal/evidence/usecases/domain"
	requestdomain "github.com/devpablocristo/nexus/governance/internal/requests/usecases/domain"
)

// fakeAttestationReader simula el store de attestations
type fakeAttestationReader struct {
	att requestdomain.Attestation
	err error
}

func (f *fakeAttestationReader) GetByRequestID(_ context.Context, _ uuid.UUID) (requestdomain.Attestation, error) {
	return f.att, f.err
}

// --- Fakes ---

type fakeRequestReader struct {
	req requestdomain.Request
	err error
}

func (f *fakeRequestReader) GetByID(_ context.Context, _ uuid.UUID) (requestdomain.Request, error) {
	return f.req, f.err
}

type fakeApprovalReader struct {
	approval *approvaldomain.Approval
	err      error
}

func (f *fakeApprovalReader) GetByRequestID(_ context.Context, _ uuid.UUID) (*approvaldomain.Approval, error) {
	return f.approval, f.err
}

type fakeEventLister struct {
	events []auditdomain.RequestEvent
	err    error
}

func (f *fakeEventLister) ListByRequestID(_ context.Context, _ uuid.UUID) ([]auditdomain.RequestEvent, error) {
	return f.events, f.err
}

type fakeSigner struct{}

func (f *fakeSigner) SignPack(pack *evidencedomain.EvidencePack) error {
	pack.Signature = evidencedomain.Signature{
		Algorithm: "hmac-sha256",
		KeyID:     "test",
		SignedAt:  time.Now().UTC().Format(time.RFC3339),
		Value:     "fake-signature",
	}
	return nil
}

// --- Tests ---

func TestGenerate_HappyPath_AllowedRequest(t *testing.T) {
	t.Parallel()
	reqID := uuid.New()
	now := time.Now().UTC()

	uc := NewUsecases(
		&fakeRequestReader{req: requestdomain.Request{
			ID:             reqID,
			RequesterType:  requestdomain.RequesterTypeAgent,
			RequesterID:    "ops-bot",
			RequesterName:  "Operations Bot",
			ActionType:     "alert.silence",
			TargetSystem:   "prometheus",
			TargetResource: "alert-123",
			RiskLevel:      requestdomain.RiskLow,
			Decision:       requestdomain.DecisionAllow,
			DecisionReason: "Policy 'Low risk auto-allow'",
			Status:         requestdomain.StatusAllowed,
			CreatedAt:      now,
			UpdatedAt:      now,
		}},
		&fakeApprovalReader{approval: nil},
		&fakeEventLister{events: []auditdomain.RequestEvent{
			{ID: uuid.New(), RequestID: reqID, EventType: auditdomain.EventReceived, ActorType: "requester", ActorID: "ops-bot", Summary: "Request received", CreatedAt: now},
			{ID: uuid.New(), RequestID: reqID, EventType: auditdomain.EventAllowed, ActorType: "system", ActorID: "system", Summary: "Auto-allowed", CreatedAt: now.Add(100 * time.Millisecond)},
		}},
		&fakeSigner{},
	)

	pack, err := uc.Generate(context.Background(), reqID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Version != EvidenceVersion {
		t.Errorf("version = %q, want %q", pack.Version, EvidenceVersion)
	}
	if pack.Request.ID != reqID.String() {
		t.Errorf("request.id = %q, want %q", pack.Request.ID, reqID.String())
	}
	if pack.Request.Requester.Type != "agent" {
		t.Errorf("requester.type = %q, want %q", pack.Request.Requester.Type, "agent")
	}
	if pack.Request.Action.Type != "alert.silence" {
		t.Errorf("action.type = %q, want %q", pack.Request.Action.Type, "alert.silence")
	}
	if pack.PolicyEval.Decision != "allow" {
		t.Errorf("policy_eval.decision = %q, want %q", pack.PolicyEval.Decision, "allow")
	}
	if pack.Approval != nil {
		t.Error("approval should be nil for auto-allowed request")
	}
	if pack.Execution != nil {
		t.Error("execution should be nil for allowed (not executed) request")
	}
	if len(pack.Timeline) != 2 {
		t.Errorf("timeline length = %d, want 2", len(pack.Timeline))
	}
	if pack.Signature.Algorithm != "hmac-sha256" {
		t.Errorf("signature.algorithm = %q, want %q", pack.Signature.Algorithm, "hmac-sha256")
	}
	if pack.Signature.Value == "" {
		t.Error("signature.value should not be empty")
	}
}

func TestGenerate_WithApproval_BreakGlass(t *testing.T) {
	t.Parallel()
	reqID := uuid.New()
	approvalID := uuid.New()
	now := time.Now().UTC()
	decidedAt := now.Add(5 * time.Minute)

	uc := NewUsecases(
		&fakeRequestReader{req: requestdomain.Request{
			ID:             reqID,
			RequesterType:  requestdomain.RequesterTypeService,
			RequesterID:    "treasury-bot",
			ActionType:     "treasury.transfer",
			TargetSystem:   "bank",
			RiskLevel:      requestdomain.RiskHigh,
			Decision:       requestdomain.DecisionRequireApproval,
			DecisionReason: "High risk: require approval",
			Status:         requestdomain.StatusApproved,
			ApprovalID:     &approvalID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}},
		&fakeApprovalReader{approval: &approvaldomain.Approval{
			ID:                approvalID,
			RequestID:         reqID,
			Status:            approvaldomain.ApprovalStatusApproved,
			BreakGlass:        true,
			RequiredApprovals: 2,
			DecidedBy:         "bob",
			DecidedAt:         &decidedAt,
			Decisions: []approvaldomain.ApprovalDecision{
				{ApproverID: "alice", Action: "approve", Note: "Verified amount", DecidedAt: now.Add(3 * time.Minute)},
				{ApproverID: "bob", Action: "approve", Note: "Confirmed", DecidedAt: decidedAt},
			},
			CreatedAt: now,
		}},
		&fakeEventLister{events: []auditdomain.RequestEvent{
			{ID: uuid.New(), RequestID: reqID, EventType: auditdomain.EventReceived, ActorID: "treasury-bot", Summary: "Request received", CreatedAt: now},
		}},
		&fakeSigner{},
	)

	pack, err := uc.Generate(context.Background(), reqID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Approval == nil {
		t.Fatal("approval should not be nil")
	}
	if !pack.Approval.BreakGlass {
		t.Error("approval.break_glass should be true")
	}
	if pack.Approval.RequiredApprovals != 2 {
		t.Errorf("approval.required_approvals = %d, want 2", pack.Approval.RequiredApprovals)
	}
	if len(pack.Approval.Decisions) != 2 {
		t.Errorf("approval.decisions length = %d, want 2", len(pack.Approval.Decisions))
	}
	if pack.Approval.Decisions[0].ApproverID != "alice" {
		t.Errorf("first decision approver = %q, want %q", pack.Approval.Decisions[0].ApproverID, "alice")
	}
}

func TestGenerate_WithExecution(t *testing.T) {
	t.Parallel()
	reqID := uuid.New()
	now := time.Now().UTC()
	executedAt := now.Add(10 * time.Second)

	uc := NewUsecases(
		&fakeRequestReader{req: requestdomain.Request{
			ID:              reqID,
			RequesterType:   requestdomain.RequesterTypeAgent,
			RequesterID:     "deploy-bot",
			ActionType:      "deploy.trigger",
			Status:          requestdomain.StatusExecuted,
			RiskLevel:       requestdomain.RiskMedium,
			Decision:        requestdomain.DecisionAllow,
			ExecutionResult: map[string]any{"deploy_id": "d-123"},
			ExecutedAt:      &executedAt,
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
		&fakeApprovalReader{approval: nil},
		&fakeEventLister{events: nil},
		&fakeSigner{},
	)

	pack, err := uc.Generate(context.Background(), reqID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Execution == nil {
		t.Fatal("execution should not be nil")
	}
	if pack.Execution.Status != "executed" {
		t.Errorf("execution.status = %q, want %q", pack.Execution.Status, "executed")
	}
	if pack.Execution.Result["deploy_id"] != "d-123" {
		t.Error("execution.result missing deploy_id")
	}
}

func TestGenerate_FailedExecution(t *testing.T) {
	t.Parallel()
	reqID := uuid.New()
	now := time.Now().UTC()
	executedAt := now.Add(10 * time.Second)

	uc := NewUsecases(
		&fakeRequestReader{req: requestdomain.Request{
			ID:           reqID,
			RequesterType: requestdomain.RequesterTypeHuman,
			RequesterID:  "admin-1",
			ActionType:   "config.update",
			Status:       requestdomain.StatusFailed,
			RiskLevel:    requestdomain.RiskLow,
			Decision:     requestdomain.DecisionAllow,
			ErrorMessage: "timeout connecting to target",
			ExecutedAt:   &executedAt,
			CreatedAt:    now,
			UpdatedAt:    now,
		}},
		&fakeApprovalReader{approval: nil},
		&fakeEventLister{events: nil},
		&fakeSigner{},
	)

	pack, err := uc.Generate(context.Background(), reqID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Execution == nil {
		t.Fatal("execution should not be nil for failed request")
	}
	if pack.Execution.Status != "failed" {
		t.Errorf("execution.status = %q, want %q", pack.Execution.Status, "failed")
	}
	if pack.Execution.Error != "timeout connecting to target" {
		t.Errorf("execution.error = %q, want timeout msg", pack.Execution.Error)
	}
}

func TestGenerate_WithAttestation(t *testing.T) {
	t.Parallel()
	reqID := uuid.New()
	now := time.Now().UTC()
	executedAt := now.Add(10 * time.Second)

	uc := NewUsecases(
		&fakeRequestReader{req: requestdomain.Request{
			ID:              reqID,
			RequesterType:   requestdomain.RequesterTypeService,
			RequesterID:     "treasury-bot",
			ActionType:      "treasury.transfer",
			TargetSystem:    "bank",
			Status:          requestdomain.StatusExecuted,
			RiskLevel:       requestdomain.RiskHigh,
			Decision:        requestdomain.DecisionAllow,
			ExecutionResult: map[string]any{"amount": 5000},
			ExecutedAt:      &executedAt,
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
		&fakeApprovalReader{approval: nil},
		&fakeEventLister{events: nil},
		&fakeSigner{},
	)
	uc.WithAttestationReader(&fakeAttestationReader{
		att: requestdomain.Attestation{
			ID:           uuid.New(),
			RequestID:    reqID,
			Status:       "success",
			ProviderRefs: map[string]any{"tx_id": "bank_tx_555"},
			Signature:    "jws_signature_here",
			Attester:     "pep:treasury_gateway",
			Metadata:     map[string]any{"latency_ms": 42},
			CreatedAt:    executedAt.Add(time.Second),
		},
	})

	pack, err := uc.Generate(context.Background(), reqID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Attestation == nil {
		t.Fatal("attestation should not be nil")
	}
	if pack.Attestation.Status != "success" {
		t.Errorf("attestation.status = %q, want %q", pack.Attestation.Status, "success")
	}
	if pack.Attestation.Attester != "pep:treasury_gateway" {
		t.Errorf("attestation.attester = %q, want %q", pack.Attestation.Attester, "pep:treasury_gateway")
	}
	if pack.Attestation.ProviderRefs["tx_id"] != "bank_tx_555" {
		t.Error("attestation.provider_refs missing tx_id")
	}
	if pack.Attestation.Signature != "jws_signature_here" {
		t.Errorf("attestation.signature = %q, want %q", pack.Attestation.Signature, "jws_signature_here")
	}
	// Cadena completa: execution + attestation
	if pack.Execution == nil {
		t.Fatal("execution should also be present")
	}
}

func TestGenerate_RequestNotFound(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(
		&fakeRequestReader{err: fmt.Errorf("request not found")},
		&fakeApprovalReader{},
		&fakeEventLister{},
		&fakeSigner{},
	)

	_, err := uc.Generate(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
