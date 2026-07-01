// Package dto contiene los tipos de request/response del módulo watchers.
package dto

import (
	"encoding/json"
	"strings"

	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
)

// --- Requests ---

// CreateWatcherRequest es el request para crear un watcher.
type CreateWatcherRequest struct {
	OrgID               string          `json:"org_id"`
	Name                string          `json:"name"`
	WatcherType         string          `json:"watcher_type"`
	Config              json.RawMessage `json:"config"`
	AssigneeVirployeeID string          `json:"assignee_virployee_id,omitempty"`
	Enabled             bool            `json:"enabled"`
}

// UpdateWatcherRequest es el request para actualizar un watcher.
type UpdateWatcherRequest struct {
	Name                *string          `json:"name,omitempty"`
	Config              *json.RawMessage `json:"config,omitempty"`
	AssigneeVirployeeID *string          `json:"assignee_virployee_id,omitempty"`
	Enabled             *bool            `json:"enabled,omitempty"`
}

// --- Responses ---

// WatcherResponse es la representación HTTP de un watcher.
type WatcherResponse struct {
	ID                  string          `json:"id"`
	OrgID               string          `json:"org_id"`
	Name                string          `json:"name"`
	WatcherType         string          `json:"watcher_type"`
	Config              json.RawMessage `json:"config"`
	AssigneeVirployeeID string          `json:"assignee_virployee_id,omitempty"`
	Enabled             bool            `json:"enabled"`
	LastRunAt           *string         `json:"last_run_at,omitempty"`
	LastResult          json.RawMessage `json:"last_result,omitempty"`
	CreatedAt           string          `json:"created_at"`
	UpdatedAt           string          `json:"updated_at"`
}

// WatcherListResponse es la lista de watchers.
type WatcherListResponse struct {
	Watchers []WatcherResponse `json:"watchers"`
}

// ProposalResponse es la representación HTTP de una propuesta.
type ProposalResponse struct {
	ID              string          `json:"id"`
	WatcherID       string          `json:"watcher_id"`
	OrgID           string          `json:"org_id"`
	ActionType      string          `json:"action_type"`
	TargetResource  string          `json:"target_resource"`
	Params          json.RawMessage `json:"params"`
	Reason          string          `json:"reason"`
	NexusRequestID  *string         `json:"nexus_request_id,omitempty"`
	NexusDecision   *string         `json:"nexus_decision,omitempty"`
	ExecutionStatus string          `json:"execution_status"`
	ExecutionResult json.RawMessage `json:"execution_result,omitempty"`
	CreatedAt       string          `json:"created_at"`
	ResolvedAt      *string         `json:"resolved_at,omitempty"`
}

// ProposalListResponse es la lista de propuestas.
type ProposalListResponse struct {
	Proposals []ProposalResponse `json:"proposals"`
}

// RunResultResponse es el resultado de ejecutar un watcher manualmente.
type RunResultResponse struct {
	Found    int `json:"found"`
	Proposed int `json:"proposed"`
	Executed int `json:"executed"`
}

// WatcherToResponse convierte un watcher de dominio a DTO.
func WatcherToResponse(w domain.Watcher) WatcherResponse {
	resp := WatcherResponse{
		ID:                  w.ID.String(),
		OrgID:               w.OrgID,
		Name:                w.Name,
		WatcherType:         string(w.WatcherType),
		Config:              w.Config,
		AssigneeVirployeeID: ConfigAssigneeVirployeeID(w.Config),
		Enabled:             w.Enabled,
		LastResult:          w.LastResult,
		CreatedAt:           w.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:           w.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if w.LastRunAt != nil {
		s := w.LastRunAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastRunAt = &s
	}
	return resp
}

func ConfigAssigneeVirployeeID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var holder map[string]any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return ""
	}
	value, _ := holder["assignee_virployee_id"].(string)
	return strings.TrimSpace(value)
}

func WithConfigAssigneeVirployeeID(raw json.RawMessage, virployeeID string) json.RawMessage {
	virployeeID = strings.TrimSpace(virployeeID)
	holder := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	if virployeeID == "" {
		delete(holder, "assignee_virployee_id")
	} else {
		holder["assignee_virployee_id"] = virployeeID
	}
	out, err := json.Marshal(holder)
	if err != nil {
		return raw
	}
	return out
}

// ProposalToResponse convierte una propuesta de dominio a DTO.
func ProposalToResponse(p domain.Proposal) ProposalResponse {
	resp := ProposalResponse{
		ID:              p.ID.String(),
		WatcherID:       p.WatcherID.String(),
		OrgID:           p.OrgID,
		ActionType:      p.ActionType,
		TargetResource:  p.TargetResource,
		Params:          p.Params,
		Reason:          p.Reason,
		ExecutionStatus: p.ExecutionStatus,
		ExecutionResult: p.ExecutionResult,
		CreatedAt:       p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if p.NexusRequestID != nil {
		s := p.NexusRequestID.String()
		resp.NexusRequestID = &s
	}
	if p.NexusDecision != nil {
		resp.NexusDecision = p.NexusDecision
	}
	if p.ResolvedAt != nil {
		s := p.ResolvedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ResolvedAt = &s
	}
	return resp
}
