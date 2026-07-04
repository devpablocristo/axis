package domain

import (
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type State string

const (
	StateActive   State = "active"
	StateArchived State = "archived"
	StateTrashed  State = "trashed"
)

type Virployee struct {
	ID               uuid.UUID
	Name             string
	JobRoleID        uuid.UUID
	CapabilityIDs    []uuid.UUID
	Description      string
	SupervisorUserID string
	Autonomy         AutonomyLevel

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type CreateInput struct {
	Name             string
	JobRoleID        string
	CapabilityIDs    []string
	Description      string
	SupervisorUserID string
	Autonomy         string
}

type UpdateInput struct {
	Name             string
	JobRoleID        string
	CapabilityIDs    []string
	Description      string
	SupervisorUserID string
	Autonomy         string
}

type NormalizedCreateInput struct {
	Name             string
	JobRoleID        uuid.UUID
	CapabilityIDs    []uuid.UUID
	Description      string
	SupervisorUserID string
	Autonomy         AutonomyLevel
}

type NormalizedUpdateInput struct {
	Name             string
	JobRoleID        uuid.UUID
	CapabilityIDs    []uuid.UUID
	Description      string
	SupervisorUserID string
	Autonomy         AutonomyLevel
}

func (v Virployee) State() State {
	switch {
	case v.TrashedAt != nil:
		return StateTrashed
	case v.ArchivedAt != nil:
		return StateArchived
	default:
		return StateActive
	}
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	supervisorID, err := parseRequiredString(in.SupervisorUserID, "supervisor_user_id")
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	jobRoleID, err := parseRequiredUUID(in.JobRoleID, "job_role_id")
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	capabilityIDs, err := parseOptionalUUIDList(in.CapabilityIDs, "capability_ids")
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	autonomy, err := normalizeAutonomy(in.Autonomy)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	out := NormalizedCreateInput{
		Name:             strings.TrimSpace(in.Name),
		JobRoleID:        jobRoleID,
		CapabilityIDs:    capabilityIDs,
		Description:      strings.TrimSpace(in.Description),
		SupervisorUserID: supervisorID,
		Autonomy:         autonomy,
	}
	if out.Name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	return out, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	supervisorID, err := parseRequiredString(in.SupervisorUserID, "supervisor_user_id")
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	jobRoleID, err := parseRequiredUUID(in.JobRoleID, "job_role_id")
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	capabilityIDs, err := parseOptionalUUIDList(in.CapabilityIDs, "capability_ids")
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	autonomy, err := normalizeAutonomy(in.Autonomy)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	out := NormalizedUpdateInput{
		Name:             strings.TrimSpace(in.Name),
		JobRoleID:        jobRoleID,
		CapabilityIDs:    capabilityIDs,
		Description:      strings.TrimSpace(in.Description),
		SupervisorUserID: supervisorID,
		Autonomy:         autonomy,
	}
	if out.Name == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("name is required")
	}
	return out, nil
}

func parseRequiredUUID(raw, field string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, domainerr.Validation(field + " is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domainerr.Validation(field + " must be a valid UUID")
	}
	return id, nil
}

func parseRequiredString(raw, field string) (string, error) {
	out := strings.TrimSpace(raw)
	if out == "" {
		return "", domainerr.Validation(field + " is required")
	}
	return out, nil
}

func parseOptionalUUIDList(raw []string, field string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	seen := map[uuid.UUID]struct{}{}
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, domainerr.Validation(field + " must contain valid UUIDs")
		}
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil {
			return nil, domainerr.Validation(field + " must contain valid UUIDs")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}
