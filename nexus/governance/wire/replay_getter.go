package wire

import (
	"context"
	"strings"

	"github.com/devpablocristo/nexus/governance/internal/audit"
	"github.com/devpablocristo/nexus/governance/internal/requests"
	"github.com/google/uuid"
)

type replayRequestGetter struct {
	reqRepo requests.Repository
}

func newReplayRequestGetter(reqRepo requests.Repository) audit.RequestGetter {
	return &replayRequestGetter{reqRepo: reqRepo}
}

func (g *replayRequestGetter) GetReplayInfo(ctx context.Context, id uuid.UUID) (audit.ReplayRequestInfo, error) {
	req, err := g.reqRepo.GetByID(ctx, id)
	if err != nil {
		return audit.ReplayRequestInfo{}, err
	}
	orgID := ""
	if req.OrgID != nil {
		orgID = strings.TrimSpace(*req.OrgID)
	}
	return audit.ReplayRequestInfo{
		OrgID:          orgID,
		RequesterType:  string(req.RequesterType),
		RequesterID:    req.RequesterID,
		ActionType:     req.ActionType,
		TargetSystem:   req.TargetSystem,
		TargetResource: req.TargetResource,
		Status:         string(req.Status),
	}, nil
}
