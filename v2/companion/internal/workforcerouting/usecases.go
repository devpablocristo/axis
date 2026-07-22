package workforcerouting

import (
	"context"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	CreateWorkSubject(context.Context, string, NormalizedWorkSubjectInput) (WorkSubject, error)
	ListWorkSubjects(context.Context, string, ResourceState, SubjectKind) ([]WorkSubject, error)
	GetWorkSubject(context.Context, string, uuid.UUID) (WorkSubject, error)
	UpdateWorkSubject(context.Context, string, uuid.UUID, NormalizedWorkSubjectInput) (WorkSubject, error)
	SetWorkSubjectArchived(context.Context, string, uuid.UUID, bool) error

	CreateRoutingPool(context.Context, string, NormalizedRoutingPoolInput) (RoutingPool, error)
	ListRoutingPools(context.Context, string, ResourceState) ([]RoutingPool, error)
	GetRoutingPool(context.Context, string, uuid.UUID) (RoutingPool, error)
	UpdateRoutingPool(context.Context, string, uuid.UUID, NormalizedRoutingPoolInput) (RoutingPool, error)
	SetRoutingPoolArchived(context.Context, string, uuid.UUID, bool) error
	UpsertPoolMember(context.Context, string, uuid.UUID, uuid.UUID, UpsertPoolMemberInput) (PoolMember, error)
	ListPoolMembers(context.Context, string, uuid.UUID) ([]PoolMember, error)

	ListRelationships(context.Context, string, uuid.UUID) ([]VirployeeRelationship, error)
	ReplaceRelationships(context.Context, string, uuid.UUID, []NormalizedRelationshipInput) ([]VirployeeRelationship, error)

	Resolve(context.Context, string, NormalizedResolveInput) (ResolveResult, error)
	ListAssignments(context.Context, string, uuid.UUID, uuid.UUID) ([]ContinuityAssignment, error)
	ListAssignmentsForVirployee(context.Context, string, uuid.UUID) ([]ContinuityAssignment, error)
	Reassign(context.Context, string, uuid.UUID, NormalizedReassignInput) (ContinuityAssignment, error)
	ValidateAssistAssignment(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int64) (int64, error)
	RequiresAssistAssignment(context.Context, string, uuid.UUID, uuid.UUID) (bool, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) CreateWorkSubject(ctx context.Context, tenantID string, in CreateWorkSubjectInput) (WorkSubject, error) {
	normalized, err := NormalizeWorkSubjectInput(in)
	if err != nil {
		return WorkSubject{}, err
	}
	return u.repo.CreateWorkSubject(ctx, normalizeTenantID(tenantID), normalized)
}

func (u *UseCases) ListWorkSubjects(ctx context.Context, tenantID, state, kind string) ([]WorkSubject, error) {
	normalizedState, err := normalizeResourceState(state)
	if err != nil {
		return nil, err
	}
	normalizedKind := SubjectKind(strings.ToLower(strings.TrimSpace(kind)))
	if normalizedKind != "" {
		switch normalizedKind {
		case SubjectKindPerson, SubjectKindOrganization, SubjectKindTeam, SubjectKindPatient, SubjectKindCase:
		default:
			return nil, domainerr.Validation("invalid subject kind")
		}
	}
	return u.repo.ListWorkSubjects(ctx, normalizeTenantID(tenantID), normalizedState, normalizedKind)
}

func (u *UseCases) GetWorkSubject(ctx context.Context, tenantID string, id uuid.UUID) (WorkSubject, error) {
	return u.repo.GetWorkSubject(ctx, normalizeTenantID(tenantID), id)
}

func (u *UseCases) UpdateWorkSubject(ctx context.Context, tenantID string, id uuid.UUID, in UpdateWorkSubjectInput) (WorkSubject, error) {
	normalized, err := NormalizeWorkSubjectInput(in)
	if err != nil {
		return WorkSubject{}, err
	}
	return u.repo.UpdateWorkSubject(ctx, normalizeTenantID(tenantID), id, normalized)
}

func (u *UseCases) ArchiveWorkSubject(ctx context.Context, tenantID string, id uuid.UUID) error {
	return u.repo.SetWorkSubjectArchived(ctx, normalizeTenantID(tenantID), id, true)
}

func (u *UseCases) UnarchiveWorkSubject(ctx context.Context, tenantID string, id uuid.UUID) error {
	return u.repo.SetWorkSubjectArchived(ctx, normalizeTenantID(tenantID), id, false)
}

func (u *UseCases) CreateRoutingPool(ctx context.Context, tenantID string, in CreateRoutingPoolInput) (RoutingPool, error) {
	normalized, err := NormalizeRoutingPoolInput(in)
	if err != nil {
		return RoutingPool{}, err
	}
	return u.repo.CreateRoutingPool(ctx, normalizeTenantID(tenantID), normalized)
}

func (u *UseCases) ListRoutingPools(ctx context.Context, tenantID, state string) ([]RoutingPool, error) {
	normalizedState, err := normalizeResourceState(state)
	if err != nil {
		return nil, err
	}
	return u.repo.ListRoutingPools(ctx, normalizeTenantID(tenantID), normalizedState)
}

func (u *UseCases) GetRoutingPool(ctx context.Context, tenantID string, id uuid.UUID) (RoutingPool, error) {
	return u.repo.GetRoutingPool(ctx, normalizeTenantID(tenantID), id)
}

func (u *UseCases) UpdateRoutingPool(ctx context.Context, tenantID string, id uuid.UUID, in UpdateRoutingPoolInput) (RoutingPool, error) {
	normalized, err := NormalizeRoutingPoolInput(in)
	if err != nil {
		return RoutingPool{}, err
	}
	return u.repo.UpdateRoutingPool(ctx, normalizeTenantID(tenantID), id, normalized)
}

func (u *UseCases) ArchiveRoutingPool(ctx context.Context, tenantID string, id uuid.UUID) error {
	return u.repo.SetRoutingPoolArchived(ctx, normalizeTenantID(tenantID), id, true)
}

func (u *UseCases) UnarchiveRoutingPool(ctx context.Context, tenantID string, id uuid.UUID) error {
	return u.repo.SetRoutingPoolArchived(ctx, normalizeTenantID(tenantID), id, false)
}

func (u *UseCases) UpsertPoolMember(ctx context.Context, tenantID string, poolID, virployeeID uuid.UUID, in UpsertPoolMemberInput) (PoolMember, error) {
	normalized, err := NormalizePoolMemberInput(in)
	if err != nil {
		return PoolMember{}, err
	}
	return u.repo.UpsertPoolMember(ctx, normalizeTenantID(tenantID), poolID, virployeeID, normalized)
}

func (u *UseCases) ListPoolMembers(ctx context.Context, tenantID string, poolID uuid.UUID) ([]PoolMember, error) {
	return u.repo.ListPoolMembers(ctx, normalizeTenantID(tenantID), poolID)
}

func (u *UseCases) ListRelationships(ctx context.Context, tenantID string, virployeeID uuid.UUID) ([]VirployeeRelationship, error) {
	return u.repo.ListRelationships(ctx, normalizeTenantID(tenantID), virployeeID)
}

func (u *UseCases) ReplaceRelationships(ctx context.Context, tenantID string, virployeeID uuid.UUID, items []RelationshipInput) ([]VirployeeRelationship, error) {
	normalized, err := NormalizeRelationships(items)
	if err != nil {
		return nil, err
	}
	return u.repo.ReplaceRelationships(ctx, normalizeTenantID(tenantID), virployeeID, normalized)
}

func (u *UseCases) Resolve(ctx context.Context, tenantID string, in ResolveInput) (ResolveResult, error) {
	normalized, err := NormalizeResolveInput(in)
	if err != nil {
		return ResolveResult{}, err
	}
	return u.repo.Resolve(ctx, normalizeTenantID(tenantID), normalized)
}

func (u *UseCases) ListAssignments(ctx context.Context, tenantID, poolID, subjectID string) ([]ContinuityAssignment, error) {
	var parsedPoolID uuid.UUID
	var parsedSubjectID uuid.UUID
	var err error
	if strings.TrimSpace(poolID) != "" {
		parsedPoolID, err = parseUUID(poolID, "pool_id")
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(subjectID) != "" {
		parsedSubjectID, err = parseUUID(subjectID, "subject_id")
		if err != nil {
			return nil, err
		}
	}
	return u.repo.ListAssignments(ctx, normalizeTenantID(tenantID), parsedPoolID, parsedSubjectID)
}

func (u *UseCases) ListAssignmentsForVirployee(ctx context.Context, tenantID string, virployeeID uuid.UUID) ([]ContinuityAssignment, error) {
	if virployeeID == uuid.Nil {
		return nil, domainerr.Validation("virployee_id is required")
	}
	return u.repo.ListAssignmentsForVirployee(ctx, normalizeTenantID(tenantID), virployeeID)
}

func (u *UseCases) Reassign(ctx context.Context, tenantID string, assignmentID uuid.UUID, in ReassignInput) (ContinuityAssignment, error) {
	normalized, err := NormalizeReassignInput(in)
	if err != nil {
		return ContinuityAssignment{}, err
	}
	return u.repo.Reassign(ctx, normalizeTenantID(tenantID), assignmentID, normalized)
}

// ValidateAssistAssignment binds Assist work to the current stable assignment.
// expectedVersion is zero while accepting a new run and the persisted version
// when a worker revalidates it before reading subject-scoped context.
func (u *UseCases) ValidateAssistAssignment(ctx context.Context, tenantID string, assignmentID, subjectID, virployeeID uuid.UUID, expectedVersion int64) (int64, error) {
	if assignmentID == uuid.Nil || subjectID == uuid.Nil || virployeeID == uuid.Nil {
		return 0, domainerr.Validation("assignment_id, subject_id and virployee_id are required")
	}
	if expectedVersion < 0 {
		return 0, domainerr.Validation("expected assignment version cannot be negative")
	}
	return u.repo.ValidateAssistAssignment(ctx, normalizeTenantID(tenantID), assignmentID, subjectID, virployeeID, expectedVersion)
}

// RequiresAssistAssignment prevents callers from bypassing continuity routing
// by omitting assignment_id. A Virployee in any active pool is routed for all
// work, and an existing subject+profession assignment is authoritative even if
// the caller targets another Virployee of that profession.
func (u *UseCases) RequiresAssistAssignment(ctx context.Context, tenantID string, subjectID, virployeeID uuid.UUID) (bool, error) {
	if virployeeID == uuid.Nil {
		return false, domainerr.Validation("virployee_id is required")
	}
	return u.repo.RequiresAssistAssignment(ctx, normalizeTenantID(tenantID), subjectID, virployeeID)
}

func normalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "default"
	}
	return tenantID
}

func normalizeResourceState(raw string) (ResourceState, error) {
	switch ResourceState(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ResourceStateActive:
		return ResourceStateActive, nil
	case ResourceStateArchived:
		return ResourceStateArchived, nil
	default:
		return "", domainerr.Validation("lifecycle must be active or archived")
	}
}
