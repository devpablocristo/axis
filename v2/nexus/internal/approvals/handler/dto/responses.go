package dto

import (
	"time"

	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
)

type ApprovalResponse struct {
	ID                 string             `json:"id"`
	GovernanceCheckID  string             `json:"governance_check_id"`
	RequesterID        string             `json:"requester_id"`
	ActionType         string             `json:"action_type"`
	TargetSystem       string             `json:"target_system"`
	TargetResource     string             `json:"target_resource"`
	RiskLevel          string             `json:"risk_level"`
	Reason             string             `json:"reason"`
	BindingHash        string             `json:"binding_hash"`
	Status             string             `json:"status"`
	ApprovalKind       string             `json:"approval_kind"`
	SupervisorUserID   string             `json:"supervisor_user_id"`
	QuorumRequired     int                `json:"quorum_required"`
	ApprovalCount      int                `json:"approval_count"`
	Decisions          []DecisionResponse `json:"decisions"`
	PostReviewRequired bool               `json:"post_review_required"`
	ReviewedBy         string             `json:"reviewed_by"`
	ReviewNote         string             `json:"review_note"`
	ReviewedAt         *time.Time         `json:"reviewed_at"`
	DecidedBy          string             `json:"decided_by"`
	DecisionNote       string             `json:"decision_note"`
	DecidedAt          *time.Time         `json:"decided_at"`
	ExpiresAt          time.Time          `json:"expires_at"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

type DecisionResponse struct {
	ID        string    `json:"id"`
	ActorID   string    `json:"actor_id"`
	ActorRole string    `json:"actor_role"`
	Decision  string    `json:"decision"`
	Note      string    `json:"note"`
	DecidedAt time.Time `json:"decided_at"`
}

type ListApprovalsResponse struct {
	Items      []ApprovalResponse `json:"items"`
	HasMore    bool               `json:"has_more"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

func ApprovalFromDomain(item domain.Approval) ApprovalResponse {
	decisions := make([]DecisionResponse, 0, len(item.Decisions))
	for _, decision := range item.Decisions {
		decisions = append(decisions, DecisionResponse{ID: decision.ID.String(), ActorID: decision.ActorID, ActorRole: decision.ActorRole, Decision: decision.Decision, Note: decision.Note, DecidedAt: decision.DecidedAt})
	}
	return ApprovalResponse{
		ID:                 item.ID.String(),
		GovernanceCheckID:  item.GovernanceCheckID.String(),
		RequesterID:        item.RequesterID,
		ActionType:         item.ActionType,
		TargetSystem:       item.TargetSystem,
		TargetResource:     item.TargetResource,
		RiskLevel:          item.RiskLevel,
		Reason:             item.Reason,
		BindingHash:        item.BindingHash,
		Status:             string(item.Status),
		ApprovalKind:       item.ApprovalKind,
		SupervisorUserID:   item.SupervisorUserID,
		QuorumRequired:     item.QuorumRequired,
		ApprovalCount:      item.ApprovalCount,
		Decisions:          decisions,
		PostReviewRequired: item.PostReviewRequired,
		ReviewedBy:         item.ReviewedBy,
		ReviewNote:         item.ReviewNote,
		ReviewedAt:         item.ReviewedAt,
		DecidedBy:          item.DecidedBy,
		DecisionNote:       item.DecisionNote,
		DecidedAt:          item.DecidedAt,
		ExpiresAt:          item.ExpiresAt,
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
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
