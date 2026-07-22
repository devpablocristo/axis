package domain

import (
	"time"

	"github.com/google/uuid"
)

// AuditEvent is one sealed entry in a virployee's tamper-evident ledger. Events
// are chained per virployee (ChainScope = "<tenant_id>/<virployee_id>"): each
// event's PreviousHash points at the prior event's EventHash, so reordering,
// deleting or editing any event breaks the chain and verification detects it.
//
// Data carries only hashes + non-sensitive metadata (never PHI or raw content):
// the content itself lives in the emitting service, bound to the event via a
// content hash (e.g. data.output_hash).
type AuditEvent struct {
	ID             uuid.UUID
	TenantID       string
	ChainScope     string
	VirployeeID    string
	SubjectType    string
	SubjectID      string
	EventType      string
	ActorType      string
	ActorID        string
	Summary        string
	Data           map[string]any
	CreatedAt      time.Time
	PreviousHash   string
	PayloadHash    string
	EventHash      string
	SignatureKeyID string
	Signature      string
}

// Event types emitted into the ledger.
const (
	EventAssistCompleted    = "assist_completed"
	EventAssistFailed       = "assist_failed"
	EventExecutionSucceeded = "execution_succeeded"
	EventExecutionFailed    = "execution_failed"
	EventGovernanceDecided  = "governance_decided"
)

// ChainScopeFor builds the per-virployee chain scope. Tenant is always included
// so two tenants can never share a chain.
func ChainScopeFor(tenantID, virployeeID string) string {
	return tenantID + "/" + virployeeID
}

// AppendInput is the caller-supplied part of an event. Tenant and (optionally)
// the actor come from the trusted request headers, not the body.
type AppendInput struct {
	VirployeeID string
	SubjectType string
	SubjectID   string
	EventType   string
	ActorType   string
	ActorID     string
	Summary     string
	Data        map[string]any
}
