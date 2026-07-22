package governancepolicies

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCreateVersionRejectsInvalidCELBeforePersistence(t *testing.T) {
	uc := NewUseCases(&policyRepoStub{}, NewEvaluator(nil), nil)
	_, err := uc.CreateVersion(context.Background(), "tenant-1", "owner-1", "owner", uuid.New(), CreateVersionInput{
		ActionTypePattern: "calendar.*", Expression: "action.type ==", Effect: EffectDeny,
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected invalid CEL validation, got %v", err)
	}
}

func TestPromotionRejectsRequesterAndVersionCreator(t *testing.T) {
	versionID, promotionID := uuid.New(), uuid.New()
	repo := &policyRepoStub{
		version:   Version{ID: versionID, TenantID: "tenant-1", PolicyID: uuid.New(), State: StateShadow, CreatedBy: "creator-1", ActionTypePattern: "*"},
		promotion: Promotion{ID: promotionID, TenantID: "tenant-1", PolicyVersionID: versionID, Status: "pending", RequestedBy: "requester-1"},
	}
	uc := NewUseCases(repo, NewEvaluator(nil), nil)

	if _, err := uc.DecidePromotion(context.Background(), "tenant-1", "requester-1", "owner", promotionID, true, PromotionDecisionInput{}); !domainerr.IsForbidden(err) {
		t.Fatalf("requester must not approve own promotion: %v", err)
	}
	if _, err := uc.DecidePromotion(context.Background(), "tenant-1", "creator-1", "admin", promotionID, true, PromotionDecisionInput{}); !domainerr.IsForbidden(err) {
		t.Fatalf("version creator must not approve promotion: %v", err)
	}
}

type policyRepoStub struct {
	RepositoryPort
	version   Version
	promotion Promotion
}

func (r *policyRepoStub) GetVersion(_ context.Context, _ string, _ uuid.UUID) (Version, error) {
	return r.version, nil
}

func (r *policyRepoStub) GetPromotion(_ context.Context, _ string, _ uuid.UUID) (Promotion, error) {
	return r.promotion, nil
}

func (r *policyRepoStub) GetSimulation(_ context.Context, _ string, _ uuid.UUID) (Simulation, error) {
	return Simulation{ID: uuid.New(), PolicyVersionID: r.version.ID, CreatedAt: time.Now().UTC()}, nil
}
