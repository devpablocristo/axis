package workforcerouting

import (
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type ResourceState string

const (
	ResourceStateActive   ResourceState = "active"
	ResourceStateArchived ResourceState = "archived"
)

type SubjectKind string

const (
	SubjectKindPerson       SubjectKind = "person"
	SubjectKindOrganization SubjectKind = "organization"
	SubjectKindTeam         SubjectKind = "team"
	SubjectKindPatient      SubjectKind = "patient"
	SubjectKindCase         SubjectKind = "case"
)

type RelationshipType string

const (
	RelationshipWorksFor  RelationshipType = "works_for"
	RelationshipServes    RelationshipType = "serves"
	RelationshipReportsTo RelationshipType = "reports_to"
)

type ResolveStatus string

const (
	ResolveStatusAssigned             ResolveStatus = "assigned"
	ResolveStatusUnavailable          ResolveStatus = "unavailable"
	ResolveStatusReassignmentRequired ResolveStatus = "reassignment_required"
)

type WorkSubject struct {
	ID          uuid.UUID
	TenantID    string
	Kind        SubjectKind
	DisplayName string
	ExternalRef string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
}

func (s WorkSubject) State() ResourceState {
	if s.ArchivedAt != nil {
		return ResourceStateArchived
	}
	return ResourceStateActive
}

type RoutingPool struct {
	ID         uuid.UUID
	TenantID   string
	JobRoleID  uuid.UUID
	Name       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}

func (p RoutingPool) State() ResourceState {
	if p.ArchivedAt != nil {
		return ResourceStateArchived
	}
	return ResourceStateActive
}

type PoolMember struct {
	TenantID          string
	PoolID            uuid.UUID
	VirployeeID       uuid.UUID
	MaxActiveSubjects int
	Enabled           bool
	ActiveSubjects    int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type VirployeeRelationship struct {
	ID               uuid.UUID
	TenantID         string
	VirployeeID      uuid.UUID
	SubjectID        uuid.UUID
	RelationshipType RelationshipType
	IsPrimary        bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ContinuityAssignment struct {
	ID          uuid.UUID
	TenantID    string
	PoolID      uuid.UUID
	SubjectID   uuid.UUID
	VirployeeID uuid.UUID
	Status      string
	Version     int64
	AssignedAt  time.Time
	UpdatedAt   time.Time
}

type ResolveResult struct {
	Status     ResolveStatus
	Created    bool
	Assignment *ContinuityAssignment
}

type CreateWorkSubjectInput struct {
	Kind        string
	DisplayName string
	ExternalRef string
}

type UpdateWorkSubjectInput = CreateWorkSubjectInput

type NormalizedWorkSubjectInput struct {
	Kind        SubjectKind
	DisplayName string
	ExternalRef string
}

type CreateRoutingPoolInput struct {
	JobRoleID string
	Name      string
}

type UpdateRoutingPoolInput = CreateRoutingPoolInput

type NormalizedRoutingPoolInput struct {
	JobRoleID uuid.UUID
	Name      string
}

type UpsertPoolMemberInput struct {
	MaxActiveSubjects int
	Enabled           bool
}

type RelationshipInput struct {
	SubjectID string
	Type      string
	IsPrimary bool
}

type NormalizedRelationshipInput struct {
	SubjectID        uuid.UUID
	RelationshipType RelationshipType
	IsPrimary        bool
}

type ResolveInput struct {
	PoolID        string
	SubjectID     string
	CapabilityKey string
	ActorID       string
}

type NormalizedResolveInput struct {
	PoolID        uuid.UUID
	SubjectID     uuid.UUID
	CapabilityKey string
	ActorID       string
}

type ReassignInput struct {
	VirployeeID     string
	ExpectedVersion int64
	Reason          string
	ActorID         string
}

type NormalizedReassignInput struct {
	VirployeeID     uuid.UUID
	ExpectedVersion int64
	Reason          string
	ActorID         string
}

var safeReasonCode = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

func NormalizeWorkSubjectInput(in CreateWorkSubjectInput) (NormalizedWorkSubjectInput, error) {
	kind := SubjectKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	switch kind {
	case SubjectKindPerson, SubjectKindOrganization, SubjectKindTeam, SubjectKindPatient, SubjectKindCase:
	default:
		return NormalizedWorkSubjectInput{}, domainerr.Validation("kind must be person, organization, team, patient or case")
	}
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		return NormalizedWorkSubjectInput{}, domainerr.Validation("display_name is required")
	}
	return NormalizedWorkSubjectInput{Kind: kind, DisplayName: displayName, ExternalRef: strings.TrimSpace(in.ExternalRef)}, nil
}

func NormalizeRoutingPoolInput(in CreateRoutingPoolInput) (NormalizedRoutingPoolInput, error) {
	jobRoleID, err := parseUUID(in.JobRoleID, "job_role_id")
	if err != nil {
		return NormalizedRoutingPoolInput{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return NormalizedRoutingPoolInput{}, domainerr.Validation("name is required")
	}
	return NormalizedRoutingPoolInput{JobRoleID: jobRoleID, Name: name}, nil
}

func NormalizePoolMemberInput(in UpsertPoolMemberInput) (UpsertPoolMemberInput, error) {
	if in.MaxActiveSubjects <= 0 {
		return UpsertPoolMemberInput{}, domainerr.Validation("max_active_subjects must be greater than zero")
	}
	return in, nil
}

func NormalizeRelationships(items []RelationshipInput) ([]NormalizedRelationshipInput, error) {
	out := make([]NormalizedRelationshipInput, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	primaryEmployers := 0
	for _, item := range items {
		subjectID, err := parseUUID(item.SubjectID, "subject_id")
		if err != nil {
			return nil, err
		}
		relationshipType := RelationshipType(strings.ToLower(strings.TrimSpace(item.Type)))
		switch relationshipType {
		case RelationshipWorksFor, RelationshipServes, RelationshipReportsTo:
		default:
			return nil, domainerr.Validation("relationship type must be works_for, serves or reports_to")
		}
		if item.IsPrimary && relationshipType != RelationshipWorksFor {
			return nil, domainerr.Validation("only works_for may be primary")
		}
		if relationshipType == RelationshipWorksFor && item.IsPrimary {
			primaryEmployers++
		}
		key := string(relationshipType) + ":" + subjectID.String()
		if _, exists := seen[key]; exists {
			return nil, domainerr.Validation("relationships must not contain duplicates")
		}
		seen[key] = struct{}{}
		out = append(out, NormalizedRelationshipInput{SubjectID: subjectID, RelationshipType: relationshipType, IsPrimary: item.IsPrimary})
	}
	if primaryEmployers != 1 {
		return nil, domainerr.Validation("exactly one primary works_for relationship is required")
	}
	return out, nil
}

func NormalizeResolveInput(in ResolveInput) (NormalizedResolveInput, error) {
	poolID, err := parseUUID(in.PoolID, "pool_id")
	if err != nil {
		return NormalizedResolveInput{}, err
	}
	subjectID, err := parseUUID(in.SubjectID, "subject_id")
	if err != nil {
		return NormalizedResolveInput{}, err
	}
	actorID := strings.TrimSpace(in.ActorID)
	if actorID == "" {
		actorID = "system"
	}
	capabilityKey := strings.ToLower(strings.TrimSpace(in.CapabilityKey))
	switch capabilityKey {
	case "medmory.search.query":
		capabilityKey = "clinical.records.search"
	case "medmory.timeline.read", "medmory.timeline.build":
		capabilityKey = "clinical.timeline.build"
	}
	if capabilityKey != "" && !regexp.MustCompile(`^[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+$`).MatchString(capabilityKey) {
		return NormalizedResolveInput{}, domainerr.Validation("capability_key must use domain.resource.action")
	}
	return NormalizedResolveInput{PoolID: poolID, SubjectID: subjectID, CapabilityKey: capabilityKey, ActorID: actorID}, nil
}

func NormalizeReassignInput(in ReassignInput) (NormalizedReassignInput, error) {
	virployeeID, err := parseUUID(in.VirployeeID, "virployee_id")
	if err != nil {
		return NormalizedReassignInput{}, err
	}
	if in.ExpectedVersion <= 0 {
		return NormalizedReassignInput{}, domainerr.Validation("expected_version must be greater than zero")
	}
	reason := strings.ToLower(strings.TrimSpace(in.Reason))
	if !safeReasonCode.MatchString(reason) {
		return NormalizedReassignInput{}, domainerr.Validation("reason must be a safe reason code")
	}
	actorID := strings.TrimSpace(in.ActorID)
	if actorID == "" {
		actorID = "system"
	}
	return NormalizedReassignInput{VirployeeID: virployeeID, ExpectedVersion: in.ExpectedVersion, Reason: reason, ActorID: actorID}, nil
}

func parseUUID(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domainerr.Validation(field + " must be a valid UUID")
	}
	return id, nil
}
