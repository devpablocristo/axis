package dto

import (
	"strings"
	"time"

	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
)

type AppendResponse struct {
	ID           string `json:"id"`
	ChainScope   string `json:"chain_scope"`
	VirployeeID  string `json:"virployee_id"`
	EventType    string `json:"event_type"`
	SubjectType  string `json:"subject_type,omitempty"`
	SubjectID    string `json:"subject_id,omitempty"`
	PreviousHash string `json:"previous_hash,omitempty"`
	PayloadHash  string `json:"payload_hash"`
	EventHash    string `json:"event_hash"`
	Signed       bool   `json:"signed"`
	CreatedAt    string `json:"created_at"`
}

func AppendResponseFromDomain(e auditdomain.AuditEvent) AppendResponse {
	return AppendResponse{
		ID:           e.ID.String(),
		ChainScope:   e.ChainScope,
		VirployeeID:  e.VirployeeID,
		EventType:    e.EventType,
		SubjectType:  e.SubjectType,
		SubjectID:    e.SubjectID,
		PreviousHash: e.PreviousHash,
		PayloadHash:  e.PayloadHash,
		EventHash:    e.EventHash,
		Signed:       strings.TrimSpace(e.SignatureKeyID) != "",
		CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
