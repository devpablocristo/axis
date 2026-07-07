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
	})

	for _, actionType := range []string{"low.action", "medium.action"} {
		out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
			RequesterID: "virployee-1",
			ActionType:  actionType,
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
	}
}

func TestCheckRequiresApprovalForHighRisk(t *testing.T) {
	uc := NewUseCases(fakeActionTypeReader{
		"high.action": {
			ActionTypeKey: "high.action",
			RiskClass:     actiondomain.RiskClassHigh,
			Enabled:       true,
		},
	})

	out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "high.action",
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
}

func TestCheckRejectsUnknownAndDisabledActionTypes(t *testing.T) {
	uc := NewUseCases(fakeActionTypeReader{
		"disabled.action": {
			ActionTypeKey: "disabled.action",
			RiskClass:     actiondomain.RiskClassLow,
			Enabled:       false,
		},
	})

	_, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "missing.action",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for unknown action type, got %v", err)
	}

	_, err = uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "disabled.action",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for disabled action type, got %v", err)
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
