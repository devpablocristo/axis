package governance

import (
	"context"
	"testing"

	actiondomain "github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCheckAllowsLowAndMediumRisk(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	uc := NewUseCases(fakeActionTypeReader{
		"low.action": {
			ActionTypeKey: "low.action",
			RiskClass:     actiondomain.RiskClassLow,
			Enabled:       true,
		},
		"medium.action": {
			ActionTypeKey: "medium.action",
			RiskClass:     actiondomain.RiskClassMedium,
			Enabled:       true,
		},
	}, recorder)

	for _, actionType := range []string{"low.action", "medium.action"} {
		out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
			RequesterID: "virployee-1",
			ActionType:  actionType,
			BindingHash: "binding-" + actionType,
		})
		if err != nil {
			t.Fatalf("Check(%s) error = %v", actionType, err)
		}
		if out.Decision != domain.DecisionAllow || out.Status != domain.StatusAllowed {
			t.Fatalf("Check(%s) = %+v, want allow/allowed", actionType, out)
		}
		if out.WouldRequireApproval {
			t.Fatalf("Check(%s) should not require approval", actionType)
		}
		if out.BindingHash != "binding-"+actionType {
			t.Fatalf("Check(%s) binding_hash = %q", actionType, out.BindingHash)
		}
	}
	if len(recorder.rows) != 2 {
		t.Fatalf("expected 2 recorded checks, got %+v", recorder.rows)
	}
}

func TestCheckRequiresApprovalForHighRisk(t *testing.T) {
	recorder := &fakeCheckRecorder{approvalID: uuid.NewString()}
	uc := NewUseCases(fakeActionTypeReader{
		"high.action": {
			ActionTypeKey: "high.action",
			RiskClass:     actiondomain.RiskClassHigh,
			Enabled:       true,
		},
	}, recorder)

	out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "high.action",
		BindingHash: "binding-high",
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if out.Decision != domain.DecisionRequireApproval || out.Status != domain.StatusPendingApproval {
		t.Fatalf("Check() = %+v, want require_approval/pending_approval", out)
	}
	if !out.WouldRequireApproval {
		t.Fatal("high risk should require approval")
	}
	if out.BindingHash != "binding-high" {
		t.Fatalf("unexpected binding hash: %+v", out)
	}
	if out.ApprovalID != recorder.approvalID || out.ApprovalStatus != "pending" {
		t.Fatalf("expected approval metadata, got %+v", out)
	}
}

func TestCheckRejectsUnknownActionTypes(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	uc := NewUseCases(fakeActionTypeReader{}, recorder)

	_, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "missing.action",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for unknown action type, got %v", err)
	}
	if len(recorder.rows) != 0 {
		t.Fatalf("invalid checks must not be recorded, got %+v", recorder.rows)
	}
}

func TestCheckDeniesDisabledActionTypes(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	uc := NewUseCases(fakeActionTypeReader{
		"disabled.action": {
			ActionTypeKey: "disabled.action",
			RiskClass:     actiondomain.RiskClassMedium,
			Enabled:       false,
		},
	}, recorder)

	out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "disabled.action",
		BindingHash: "binding-disabled",
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if out.Decision != domain.DecisionDeny || out.Status != domain.StatusDenied {
		t.Fatalf("Check() = %+v, want deny/denied", out)
	}
	if out.WouldRequireApproval || out.ApprovalID != "" || out.ApprovalStatus != "" {
		t.Fatalf("deny should not require approval, got %+v", out)
	}
	if out.BindingHash != "binding-disabled" || out.RiskLevel != "medium" {
		t.Fatalf("unexpected deny metadata: %+v", out)
	}
	if len(recorder.rows) != 1 || recorder.rows[0].result.Decision != domain.DecisionDeny {
		t.Fatalf("deny check should be recorded, got %+v", recorder.rows)
	}
}

type fakeActionTypeReader map[string]actiondomain.ActionType

func (r fakeActionTypeReader) GetByKey(_ context.Context, tenantID string, key string) (actiondomain.ActionType, error) {
	row, ok := r[key]
	if !ok {
		return actiondomain.ActionType{}, domainerr.NotFound("action type not found")
	}
	row.ID = uuid.New()
	row.TenantID = tenantID
	return row, nil
}

type recordedCheck struct {
	tenantID string
	input    domain.NormalizedCheckInput
	result   domain.CheckResult
}

type fakeCheckRecorder struct {
	rows       []recordedCheck
	approvalID string
}

func (r *fakeCheckRecorder) RecordCheck(_ context.Context, tenantID string, input domain.NormalizedCheckInput, result domain.CheckResult) (domain.RecordedCheck, error) {
	r.rows = append(r.rows, recordedCheck{tenantID: tenantID, input: input, result: result})
	if result.Decision == domain.DecisionRequireApproval && r.approvalID != "" {
		return domain.RecordedCheck{ApprovalID: r.approvalID, ApprovalStatus: "pending"}, nil
	}
	return domain.RecordedCheck{}, nil
}
