package executiongate

import "github.com/google/uuid"

type ConversationScopeInput struct {
	OrgID       string
	VirployeeID uuid.UUID
	JobRoleID   uuid.UUID
	Query       string
}

type ConversationScopeResult struct {
	Allowed            bool
	Decision           string
	Reason             string
	SnapshotHash       string
	ScopeRevision      int64
	PolicyRevisionHash string
}
