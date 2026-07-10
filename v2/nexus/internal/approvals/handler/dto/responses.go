package dto

import (
	"time"

	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
)

type ApprovalResponse struct {
	ID             string     `json:"id"`
	RequesterID    string     `json:"requester_id"`
	ActionType     string     `json:"action_type"`
	TargetSystem   string     `json:"target_system"`
	TargetResource string     `json:"target_resource"`
	RiskLevel      string     `json:"risk_level"`
	Reason         string     `json:"reason"`
	BindingHash    string     `json:"binding_hash"`
	Status         string     `json:"status"`
	DecidedBy      string     `json:"decided_by"`
	DecisionNote   string     `json:"decision_note"`
	DecidedAt      *time.Time `json:"decided_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ListApprovalsResponse struct {
	Items      []ApprovalResponse `json:"items"`
	HasMore    bool               `json:"has_more"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

func ApprovalFromDomain(item domain.Approval) ApprovalResponse {
	return ApprovalResponse{
		ID:             item.ID.String(),
		RequesterID:    item.RequesterID,
		ActionType:     item.ActionType,
		TargetSystem:   item.TargetSystem,
		TargetResource: item.TargetResource,
		RiskLevel:      item.RiskLevel,
		Reason:         item.Reason,
		BindingHash:    item.BindingHash,
		Status:         string(item.Status),
		DecidedBy:      item.DecidedBy,
		DecisionNote:   item.DecisionNote,
		DecidedAt:      item.DecidedAt,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

func ListApprovalsFromDomain(page domain.ListPage) ListApprovalsResponse {
	items := make([]ApprovalResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, ApprovalFromDomain(item))
	}
	return ListApprovalsResponse{
		Items:      items,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	}
}
