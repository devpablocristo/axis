package workforcerouting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateWorkSubject(ctx context.Context, tenantID string, in NormalizedWorkSubjectInput) (WorkSubject, error) {
	return scanWorkSubject(r.pool.QueryRow(ctx, `
		INSERT INTO companion_work_subjects(id,tenant_id,kind,display_name,external_ref)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id,tenant_id,kind,display_name,external_ref,created_at,updated_at,archived_at
	`, uuid.New(), tenantID, in.Kind, in.DisplayName, in.ExternalRef))
}

func (r *Repository) ListWorkSubjects(ctx context.Context, tenantID string, state ResourceState, kind SubjectKind) ([]WorkSubject, error) {
	archived := state == ResourceStateArchived
	rows, err := r.pool.Query(ctx, `
		SELECT id,tenant_id,kind,display_name,external_ref,created_at,updated_at,archived_at
		FROM companion_work_subjects
		WHERE tenant_id=$1 AND (($2 AND archived_at IS NOT NULL) OR (NOT $2 AND archived_at IS NULL))
		  AND ($3='' OR kind=$3)
		ORDER BY display_name,id
	`, tenantID, archived, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]WorkSubject, 0)
	for rows.Next() {
		item, err := scanWorkSubject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GetWorkSubject(ctx context.Context, tenantID string, id uuid.UUID) (WorkSubject, error) {
	return scanWorkSubject(r.pool.QueryRow(ctx, `
		SELECT id,tenant_id,kind,display_name,external_ref,created_at,updated_at,archived_at
		FROM companion_work_subjects WHERE tenant_id=$1 AND id=$2
	`, tenantID, id))
}

func (r *Repository) UpdateWorkSubject(ctx context.Context, tenantID string, id uuid.UUID, in NormalizedWorkSubjectInput) (WorkSubject, error) {
	item, err := scanWorkSubject(r.pool.QueryRow(ctx, `
		UPDATE companion_work_subjects SET kind=$3,display_name=$4,external_ref=$5,updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL
		RETURNING id,tenant_id,kind,display_name,external_ref,created_at,updated_at,archived_at
	`, tenantID, id, in.Kind, in.DisplayName, in.ExternalRef))
	if !domainerr.IsNotFound(err) {
		return item, err
	}
	existing, getErr := r.GetWorkSubject(ctx, tenantID, id)
	if getErr != nil {
		return WorkSubject{}, getErr
	}
	if existing.ArchivedAt != nil {
		return WorkSubject{}, domainerr.Conflict("work subject is archived")
	}
	return WorkSubject{}, err
}

func (r *Repository) SetWorkSubjectArchived(ctx context.Context, tenantID string, id uuid.UUID, archived bool) error {
	var tag pgconn.CommandTag
	var err error
	if archived {
		tag, err = r.pool.Exec(ctx, `UPDATE companion_work_subjects SET archived_at=now(),updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL`, tenantID, id)
	} else {
		tag, err = r.pool.Exec(ctx, `UPDATE companion_work_subjects SET archived_at=NULL,updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND archived_at IS NOT NULL`, tenantID, id)
	}
	return r.lifecycleResult(ctx, tenantID, id, "work subject", tag, err, func(ctx context.Context, tenantID string, id uuid.UUID) error {
		_, getErr := r.GetWorkSubject(ctx, tenantID, id)
		return getErr
	})
}

func (r *Repository) CreateRoutingPool(ctx context.Context, tenantID string, in NormalizedRoutingPoolInput) (RoutingPool, error) {
	if err := r.ensureActiveJobRole(ctx, tenantID, in.JobRoleID); err != nil {
		return RoutingPool{}, err
	}
	item, err := scanRoutingPool(r.pool.QueryRow(ctx, `
		INSERT INTO companion_routing_pools(id,tenant_id,job_role_id,name)
		VALUES ($1,$2,$3,$4)
		RETURNING id,tenant_id,job_role_id,name,created_at,updated_at,archived_at
	`, uuid.New(), tenantID, in.JobRoleID, in.Name))
	if err != nil {
		return RoutingPool{}, mapConflict(err, "an active routing pool already exists for this Job Role")
	}
	return item, nil
}

func (r *Repository) ListRoutingPools(ctx context.Context, tenantID string, state ResourceState) ([]RoutingPool, error) {
	archived := state == ResourceStateArchived
	rows, err := r.pool.Query(ctx, `
		SELECT id,tenant_id,job_role_id,name,created_at,updated_at,archived_at
		FROM companion_routing_pools
		WHERE tenant_id=$1 AND (($2 AND archived_at IS NOT NULL) OR (NOT $2 AND archived_at IS NULL))
		ORDER BY name,id
	`, tenantID, archived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RoutingPool, 0)
	for rows.Next() {
		item, err := scanRoutingPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GetRoutingPool(ctx context.Context, tenantID string, id uuid.UUID) (RoutingPool, error) {
	return scanRoutingPool(r.pool.QueryRow(ctx, `
		SELECT id,tenant_id,job_role_id,name,created_at,updated_at,archived_at
		FROM companion_routing_pools WHERE tenant_id=$1 AND id=$2
	`, tenantID, id))
}

func (r *Repository) UpdateRoutingPool(ctx context.Context, tenantID string, id uuid.UUID, in NormalizedRoutingPoolInput) (RoutingPool, error) {
	if err := r.ensureActiveJobRole(ctx, tenantID, in.JobRoleID); err != nil {
		return RoutingPool{}, err
	}
	item, err := scanRoutingPool(r.pool.QueryRow(ctx, `
		UPDATE companion_routing_pools SET job_role_id=$3,name=$4,updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL
		RETURNING id,tenant_id,job_role_id,name,created_at,updated_at,archived_at
	`, tenantID, id, in.JobRoleID, in.Name))
	if !domainerr.IsNotFound(err) {
		return item, mapConflict(err, "an active routing pool already exists for this Job Role")
	}
	existing, getErr := r.GetRoutingPool(ctx, tenantID, id)
	if getErr != nil {
		return RoutingPool{}, getErr
	}
	if existing.ArchivedAt != nil {
		return RoutingPool{}, domainerr.Conflict("routing pool is archived")
	}
	return RoutingPool{}, err
}

func (r *Repository) SetRoutingPoolArchived(ctx context.Context, tenantID string, id uuid.UUID, archived bool) error {
	var tag pgconn.CommandTag
	var err error
	if archived {
		tag, err = r.pool.Exec(ctx, `UPDATE companion_routing_pools SET archived_at=now(),updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL`, tenantID, id)
	} else {
		tag, err = r.pool.Exec(ctx, `UPDATE companion_routing_pools SET archived_at=NULL,updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND archived_at IS NOT NULL`, tenantID, id)
	}
	if err != nil {
		return mapConflict(err, "an active routing pool already exists for this Job Role")
	}
	return r.lifecycleResult(ctx, tenantID, id, "routing pool", tag, nil, func(ctx context.Context, tenantID string, id uuid.UUID) error {
		_, getErr := r.GetRoutingPool(ctx, tenantID, id)
		return getErr
	})
}

func (r *Repository) UpsertPoolMember(ctx context.Context, tenantID string, poolID, virployeeID uuid.UUID, in UpsertPoolMemberInput) (PoolMember, error) {
	var valid bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM companion_routing_pools p
			JOIN virployees v ON v.tenant_id=p.tenant_id AND v.id=$3
			WHERE p.tenant_id=$1 AND p.id=$2 AND p.archived_at IS NULL
			  AND v.archived_at IS NULL AND v.trashed_at IS NULL AND v.job_role_id=p.job_role_id
		)
	`, tenantID, poolID, virployeeID).Scan(&valid)
	if err != nil {
		return PoolMember{}, err
	}
	if !valid {
		return PoolMember{}, domainerr.Conflict("pool and active virployee must belong to the same tenant and job role")
	}
	return scanPoolMember(r.pool.QueryRow(ctx, `
		WITH saved AS (
			INSERT INTO companion_routing_pool_members(tenant_id,pool_id,virployee_id,max_active_subjects,enabled)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (tenant_id,pool_id,virployee_id) DO UPDATE SET
				max_active_subjects=EXCLUDED.max_active_subjects,enabled=EXCLUDED.enabled,updated_at=now()
			RETURNING tenant_id,pool_id,virployee_id,max_active_subjects,enabled,created_at,updated_at
		)
		SELECT s.tenant_id,s.pool_id,s.virployee_id,s.max_active_subjects,s.enabled,
			COUNT(a.id) FILTER (WHERE ws.archived_at IS NULL),s.created_at,s.updated_at
		FROM saved s
		LEFT JOIN companion_continuity_assignments a
		  ON a.tenant_id=s.tenant_id AND a.pool_id=s.pool_id AND a.virployee_id=s.virployee_id
		LEFT JOIN companion_work_subjects ws ON ws.tenant_id=a.tenant_id AND ws.id=a.subject_id
		GROUP BY s.tenant_id,s.pool_id,s.virployee_id,s.max_active_subjects,s.enabled,s.created_at,s.updated_at
	`, tenantID, poolID, virployeeID, in.MaxActiveSubjects, in.Enabled))
}

func (r *Repository) ListPoolMembers(ctx context.Context, tenantID string, poolID uuid.UUID) ([]PoolMember, error) {
	if _, err := r.GetRoutingPool(ctx, tenantID, poolID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT m.tenant_id,m.pool_id,m.virployee_id,m.max_active_subjects,m.enabled,
			COUNT(a.id) FILTER (WHERE ws.archived_at IS NULL),m.created_at,m.updated_at
		FROM companion_routing_pool_members m
		LEFT JOIN companion_continuity_assignments a
		  ON a.tenant_id=m.tenant_id AND a.pool_id=m.pool_id AND a.virployee_id=m.virployee_id
		LEFT JOIN companion_work_subjects ws ON ws.tenant_id=a.tenant_id AND ws.id=a.subject_id
		WHERE m.tenant_id=$1 AND m.pool_id=$2
		GROUP BY m.tenant_id,m.pool_id,m.virployee_id,m.max_active_subjects,m.enabled,m.created_at,m.updated_at
		ORDER BY m.created_at,m.virployee_id
	`, tenantID, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PoolMember, 0)
	for rows.Next() {
		item, err := scanPoolMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ListRelationships(ctx context.Context, tenantID string, virployeeID uuid.UUID) ([]VirployeeRelationship, error) {
	if err := r.ensureVirployeeExists(ctx, tenantID, virployeeID, false); err != nil {
		return nil, err
	}
	return r.listRelationships(ctx, r.pool, tenantID, virployeeID)
}

func (r *Repository) ReplaceRelationships(ctx context.Context, tenantID string, virployeeID uuid.UUID, items []NormalizedRelationshipInput) ([]VirployeeRelationship, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureVirployeeExistsWith(ctx, tx, tenantID, virployeeID, true); err != nil {
		return nil, err
	}
	for _, item := range items {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM companion_work_subjects
			WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL)`, tenantID, item.SubjectID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, domainerr.NotFoundf("work subject", item.SubjectID.String())
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM companion_virployee_relationships WHERE tenant_id=$1 AND virployee_id=$2`, tenantID, virployeeID); err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO companion_virployee_relationships(
			id,tenant_id,virployee_id,subject_id,relationship_type,is_primary)
			VALUES ($1,$2,$3,$4,$5,$6)`, uuid.New(), tenantID, virployeeID, item.SubjectID, item.RelationshipType, item.IsPrimary); err != nil {
			return nil, mapConflict(err, "relationship already exists")
		}
	}
	out, err := r.listRelationships(ctx, tx, tenantID, virployeeID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) Resolve(ctx context.Context, tenantID string, in NormalizedResolveInput) (ResolveResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ResolveResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockPool(ctx, tx, tenantID, in.PoolID); err != nil {
		return ResolveResult{}, err
	}
	pool, err := scanRoutingPool(tx.QueryRow(ctx, `SELECT id,tenant_id,job_role_id,name,created_at,updated_at,archived_at
		FROM companion_routing_pools WHERE tenant_id=$1 AND id=$2`, tenantID, in.PoolID))
	if err != nil {
		return ResolveResult{}, err
	}
	var subjectArchivedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT archived_at FROM companion_work_subjects WHERE tenant_id=$1 AND id=$2`, tenantID, in.SubjectID).Scan(&subjectArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolveResult{}, domainerr.NotFoundf("work subject", in.SubjectID.String())
	}
	if err != nil {
		return ResolveResult{}, err
	}
	if subjectArchivedAt != nil {
		return ResolveResult{}, domainerr.Conflict("work subject is archived")
	}

	existing, eligible, err := scanAssignmentEligibility(tx.QueryRow(ctx, assignmentEligibilitySQL, tenantID, in.PoolID, in.SubjectID))
	if err == nil {
		status := ResolveStatusAssigned
		if !eligible {
			status = ResolveStatusReassignmentRequired
		}
		if err := tx.Commit(ctx); err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{Status: status, Created: false, Assignment: &existing}, nil
	}
	if !domainerr.IsNotFound(err) {
		return ResolveResult{}, err
	}
	if pool.ArchivedAt != nil {
		return ResolveResult{}, domainerr.Conflict("routing pool is archived")
	}

	var virployeeID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT m.virployee_id
		FROM companion_routing_pool_members m
		JOIN virployees v ON v.tenant_id=m.tenant_id AND v.id=m.virployee_id
		JOIN companion_routing_pools p ON p.tenant_id=m.tenant_id AND p.id=m.pool_id
		WHERE m.tenant_id=$1 AND m.pool_id=$2 AND m.enabled
		  AND p.archived_at IS NULL AND v.archived_at IS NULL AND v.trashed_at IS NULL
		  AND v.job_role_id=p.job_role_id
		  AND (SELECT COUNT(*) FROM companion_continuity_assignments a
		       JOIN companion_work_subjects ws ON ws.tenant_id=a.tenant_id AND ws.id=a.subject_id
		       WHERE a.tenant_id=m.tenant_id AND a.pool_id=m.pool_id
		         AND a.virployee_id=m.virployee_id AND ws.archived_at IS NULL) < m.max_active_subjects
		ORDER BY (SELECT COUNT(*) FROM companion_continuity_assignments a
		          JOIN companion_work_subjects ws ON ws.tenant_id=a.tenant_id AND ws.id=a.subject_id
		          WHERE a.tenant_id=m.tenant_id AND a.pool_id=m.pool_id
		            AND a.virployee_id=m.virployee_id AND ws.archived_at IS NULL),
		         m.created_at,m.virployee_id
		LIMIT 1
	`, tenantID, in.PoolID).Scan(&virployeeID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{Status: ResolveStatusUnavailable}, nil
	}
	if err != nil {
		return ResolveResult{}, err
	}

	assignment, err := scanAssignment(tx.QueryRow(ctx, `
		INSERT INTO companion_continuity_assignments(id,tenant_id,pool_id,subject_id,virployee_id)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id,tenant_id,pool_id,subject_id,virployee_id,status,version,assigned_at,updated_at
	`, uuid.New(), tenantID, in.PoolID, in.SubjectID, virployeeID))
	if err != nil {
		return ResolveResult{}, mapConflict(err, "subject is already assigned in this pool")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO companion_continuity_assignment_events(
		id,tenant_id,assignment_id,event_type,virployee_id,actor_id,reason_code,assignment_version)
		VALUES ($1,$2,$3,'assigned',$4,$5,'automatic_pool_assignment',$6)`,
		uuid.New(), tenantID, assignment.ID, assignment.VirployeeID, in.ActorID, assignment.Version); err != nil {
		return ResolveResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{Status: ResolveStatusAssigned, Created: true, Assignment: &assignment}, nil
}

func (r *Repository) ListAssignments(ctx context.Context, tenantID string, poolID, subjectID uuid.UUID) ([]ContinuityAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id,tenant_id,pool_id,subject_id,virployee_id,status,version,assigned_at,updated_at
		FROM companion_continuity_assignments
		WHERE tenant_id=$1 AND ($2::uuid IS NULL OR pool_id=$2) AND ($3::uuid IS NULL OR subject_id=$3)
		ORDER BY assigned_at,id
	`, tenantID, nullableUUID(poolID), nullableUUID(subjectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ContinuityAssignment, 0)
	for rows.Next() {
		item, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ListAssignmentsForVirployee(ctx context.Context, tenantID string, virployeeID uuid.UUID) ([]ContinuityAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id,tenant_id,pool_id,subject_id,virployee_id,status,version,assigned_at,updated_at
		FROM companion_continuity_assignments
		WHERE tenant_id=$1 AND virployee_id=$2 AND status='active'
		ORDER BY assigned_at,id
	`, tenantID, virployeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ContinuityAssignment, 0)
	for rows.Next() {
		item, scanErr := scanAssignment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ValidateAssistAssignment(ctx context.Context, tenantID string, assignmentID, subjectID, virployeeID uuid.UUID, expectedVersion int64) (int64, error) {
	var version int64
	err := r.pool.QueryRow(ctx, `
		SELECT a.version
		FROM companion_continuity_assignments a
		JOIN companion_routing_pools p ON p.tenant_id=a.tenant_id AND p.id=a.pool_id
		JOIN companion_routing_pool_members m
		  ON m.tenant_id=a.tenant_id AND m.pool_id=a.pool_id AND m.virployee_id=a.virployee_id
		JOIN companion_work_subjects ws ON ws.tenant_id=a.tenant_id AND ws.id=a.subject_id
		JOIN virployees v ON v.tenant_id=a.tenant_id AND v.id=a.virployee_id
		WHERE a.tenant_id=$1 AND a.id=$2 AND a.subject_id=$3 AND a.virployee_id=$4
		  AND a.status='active' AND m.enabled AND p.archived_at IS NULL AND ws.archived_at IS NULL
		  AND v.archived_at IS NULL AND v.trashed_at IS NULL AND v.job_role_id=p.job_role_id
	`, tenantID, assignmentID, subjectID, virployeeID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domainerr.Conflict("continuity assignment is not active for this subject and virployee")
	}
	if err != nil {
		return 0, err
	}
	if expectedVersion > 0 && version != expectedVersion {
		return 0, domainerr.Conflict("continuity assignment changed after the Assist run was accepted")
	}
	return version, nil
}

func (r *Repository) RequiresAssistAssignment(ctx context.Context, tenantID string, subjectID, virployeeID uuid.UUID) (bool, error) {
	var required bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM virployees requested
			WHERE requested.tenant_id=$1 AND requested.id=$2
			  AND requested.archived_at IS NULL AND requested.trashed_at IS NULL
			  AND (
				EXISTS(
					SELECT 1
					FROM companion_routing_pool_members member
					JOIN companion_routing_pools pool
					  ON pool.tenant_id=member.tenant_id AND pool.id=member.pool_id
					WHERE member.tenant_id=requested.tenant_id
					  AND member.virployee_id=requested.id
					  AND pool.job_role_id=requested.job_role_id
				) OR (
					$3::uuid IS NOT NULL AND EXISTS(
						SELECT 1
						FROM companion_continuity_assignments assignment
						JOIN companion_routing_pools pool
						  ON pool.tenant_id=assignment.tenant_id AND pool.id=assignment.pool_id
						WHERE assignment.tenant_id=requested.tenant_id
						  AND assignment.subject_id=$3 AND assignment.status='active'
						  AND pool.job_role_id=requested.job_role_id
					)
				)
			  )
		)
	`, tenantID, virployeeID, nullableUUID(subjectID)).Scan(&required)
	return required, err
}

func (r *Repository) Reassign(ctx context.Context, tenantID string, assignmentID uuid.UUID, in NormalizedReassignInput) (ContinuityAssignment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ContinuityAssignment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	assignment, err := scanAssignment(tx.QueryRow(ctx, `SELECT id,tenant_id,pool_id,subject_id,virployee_id,status,version,assigned_at,updated_at
		FROM companion_continuity_assignments WHERE tenant_id=$1 AND id=$2`, tenantID, assignmentID))
	if err != nil {
		return ContinuityAssignment{}, err
	}
	if err := lockPool(ctx, tx, tenantID, assignment.PoolID); err != nil {
		return ContinuityAssignment{}, err
	}
	assignment, err = scanAssignment(tx.QueryRow(ctx, `SELECT id,tenant_id,pool_id,subject_id,virployee_id,status,version,assigned_at,updated_at
		FROM companion_continuity_assignments WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, assignmentID))
	if err != nil {
		return ContinuityAssignment{}, err
	}
	if assignment.Version != in.ExpectedVersion {
		return ContinuityAssignment{}, domainerr.Conflict("assignment version does not match")
	}
	if assignment.VirployeeID == in.VirployeeID {
		return ContinuityAssignment{}, domainerr.Conflict("assignment already belongs to that virployee")
	}
	var maxActiveSubjects, activeSubjects int
	err = tx.QueryRow(ctx, `
		SELECT m.max_active_subjects,
			(SELECT COUNT(*) FROM companion_continuity_assignments a
			 JOIN companion_work_subjects ws ON ws.tenant_id=a.tenant_id AND ws.id=a.subject_id
			 WHERE a.tenant_id=m.tenant_id AND a.pool_id=m.pool_id AND a.virployee_id=m.virployee_id
			   AND a.id<>$4 AND ws.archived_at IS NULL)
		FROM companion_routing_pool_members m
		JOIN companion_routing_pools p ON p.tenant_id=m.tenant_id AND p.id=m.pool_id
		JOIN virployees v ON v.tenant_id=m.tenant_id AND v.id=m.virployee_id
		WHERE m.tenant_id=$1 AND m.pool_id=$2 AND m.virployee_id=$3 AND m.enabled
		  AND p.archived_at IS NULL AND v.archived_at IS NULL AND v.trashed_at IS NULL
		  AND v.job_role_id=p.job_role_id
	`, tenantID, assignment.PoolID, in.VirployeeID, assignment.ID).Scan(&maxActiveSubjects, &activeSubjects)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContinuityAssignment{}, domainerr.Conflict("target virployee is not an eligible pool member")
	}
	if err != nil {
		return ContinuityAssignment{}, err
	}
	if activeSubjects >= maxActiveSubjects {
		return ContinuityAssignment{}, domainerr.Conflict("target virployee is at capacity")
	}
	previousVirployeeID := assignment.VirployeeID
	assignment, err = scanAssignment(tx.QueryRow(ctx, `
		UPDATE companion_continuity_assignments
		SET virployee_id=$3,version=version+1,updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND version=$4
		RETURNING id,tenant_id,pool_id,subject_id,virployee_id,status,version,assigned_at,updated_at
	`, tenantID, assignmentID, in.VirployeeID, in.ExpectedVersion))
	if err != nil {
		return ContinuityAssignment{}, err
	}
	// Continuity ownership moves atomically with the assignment. The original
	// entrypoint/artifact provenance remains immutable, while the new Virployee
	// becomes the only active owner of patient/case memory and private bindings.
	if _, err := tx.Exec(ctx, `
		UPDATE companion_assist_cases
		SET owner_virployee_id=$4,version=version+1,updated_at=now()
		WHERE tenant_id=$1 AND subject_id=$2 AND owner_virployee_id=$3
	`, tenantID, assignment.SubjectID.String(), previousVirployeeID, assignment.VirployeeID); err != nil {
		return ContinuityAssignment{}, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM companion_knowledge_bindings old_binding
		USING companion_knowledge_bindings target_binding
		WHERE old_binding.tenant_id=$1 AND old_binding.virployee_id=$2
		  AND old_binding.scope_type IN ('subject','case') AND old_binding.subject_id=$4
		  AND target_binding.tenant_id=old_binding.tenant_id
		  AND target_binding.knowledge_base_id=old_binding.knowledge_base_id
		  AND target_binding.scope_type=old_binding.scope_type
		  AND target_binding.virployee_id=$3
		  AND target_binding.subject_id=old_binding.subject_id
		  AND target_binding.case_id IS NOT DISTINCT FROM old_binding.case_id
	`, tenantID, previousVirployeeID, assignment.VirployeeID, assignment.SubjectID.String()); err != nil {
		return ContinuityAssignment{}, err
	}
	if _, err := tx.Exec(ctx, `
			UPDATE companion_knowledge_bindings
		SET virployee_id=$3,version=version+1,updated_at=now()
		WHERE tenant_id=$1 AND virployee_id=$2
		  AND scope_type IN ('subject','case') AND subject_id=$4
		`, tenantID, previousVirployeeID, assignment.VirployeeID, assignment.SubjectID.String()); err != nil {
		return ContinuityAssignment{}, err
	}
	// Binding row revisions are not enough when a duplicate old binding was
	// deleted. Bump every affected private library so any approval bound to
	// the previous authorization graph becomes stale after reassignment.
	if _, err := tx.Exec(ctx, `
			UPDATE companion_knowledge_bases kb
			SET version=version+1,updated_at=now()
			FROM (
				SELECT DISTINCT knowledge_base_id
				FROM companion_knowledge_bindings
				WHERE tenant_id=$1 AND virployee_id=$2
				  AND scope_type IN ('subject','case') AND subject_id=$3
			) affected
			WHERE kb.tenant_id=$1 AND kb.id=affected.knowledge_base_id
		`, tenantID, assignment.VirployeeID, assignment.SubjectID.String()); err != nil {
		return ContinuityAssignment{}, err
	}
	if _, err := tx.Exec(ctx, `
		WITH archived AS (
			UPDATE companion_memories old_memory
			SET lifecycle_state='archived',archived_at=now(),review_state='quarantined',
			    review_reason='duplicate_memory_on_reassignment',version=version+1,updated_at=now()
			WHERE old_memory.tenant_id=$1 AND old_memory.virployee_id=$2
			  AND old_memory.scope_type IN ('subject','case') AND old_memory.subject_id=$4
			  AND old_memory.lifecycle_state='active'
			  AND EXISTS (
				SELECT 1 FROM companion_memories target_memory
				WHERE target_memory.tenant_id=old_memory.tenant_id AND target_memory.virployee_id=$3
				  AND target_memory.scope_type=old_memory.scope_type
				  AND target_memory.subject_id=old_memory.subject_id
				  AND target_memory.case_id IS NOT DISTINCT FROM old_memory.case_id
				  AND target_memory.content_hash=old_memory.content_hash
				  AND target_memory.lifecycle_state='active'
			  )
			RETURNING tenant_id,virployee_id,id,content_hash,version,scope_type,subject_id,case_id
		)
		INSERT INTO companion_memory_audit(
			tenant_id,virployee_id,memory_id,action,actor_id,previous_hash,resulting_hash,
			previous_version,resulting_version,metadata,scope_type,subject_id,case_id)
		SELECT tenant_id,virployee_id,id,'archive',$5,content_hash,content_hash,
		       version-1,version,jsonb_build_object('reason','duplicate_on_reassignment','assignment_id',$6::text),
		       scope_type,subject_id,case_id
		FROM archived
	`, tenantID, previousVirployeeID, assignment.VirployeeID, assignment.SubjectID.String(), in.ActorID, assignment.ID); err != nil {
		return ContinuityAssignment{}, err
	}
	if _, err := tx.Exec(ctx, `
		WITH moved AS (
			UPDATE companion_memories
			SET virployee_id=$3,version=version+1,updated_at=now()
			WHERE tenant_id=$1 AND virployee_id=$2
			  AND scope_type IN ('subject','case') AND subject_id=$4
			  AND review_reason<>'duplicate_memory_on_reassignment'
			RETURNING tenant_id,virployee_id,id,content_hash,version,scope_type,subject_id,case_id
		)
		INSERT INTO companion_memory_audit(
			tenant_id,virployee_id,memory_id,action,actor_id,previous_hash,resulting_hash,
			previous_version,resulting_version,metadata,scope_type,subject_id,case_id)
		SELECT tenant_id,virployee_id,id,'update',$5,content_hash,content_hash,
		       version-1,version,jsonb_build_object('reason','continuity_reassignment','previous_virployee_id',$2::text,'assignment_id',$6::text),
		       scope_type,subject_id,case_id
		FROM moved
	`, tenantID, previousVirployeeID, assignment.VirployeeID, assignment.SubjectID.String(), in.ActorID, assignment.ID); err != nil {
		return ContinuityAssignment{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO companion_continuity_assignment_events(
		id,tenant_id,assignment_id,event_type,previous_virployee_id,virployee_id,actor_id,reason_code,assignment_version)
		VALUES ($1,$2,$3,'reassigned',$4,$5,$6,$7,$8)`, uuid.New(), tenantID, assignment.ID,
		previousVirployeeID, assignment.VirployeeID, in.ActorID, in.Reason, assignment.Version); err != nil {
		return ContinuityAssignment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ContinuityAssignment{}, err
	}
	return assignment, nil
}

const assignmentEligibilitySQL = `
	SELECT a.id,a.tenant_id,a.pool_id,a.subject_id,a.virployee_id,a.status,a.version,a.assigned_at,a.updated_at,
		(m.enabled AND p.archived_at IS NULL AND v.archived_at IS NULL AND v.trashed_at IS NULL
		 AND v.job_role_id=p.job_role_id)
	FROM companion_continuity_assignments a
	JOIN companion_routing_pools p ON p.tenant_id=a.tenant_id AND p.id=a.pool_id
	JOIN companion_routing_pool_members m
	  ON m.tenant_id=a.tenant_id AND m.pool_id=a.pool_id AND m.virployee_id=a.virployee_id
	JOIN virployees v ON v.tenant_id=a.tenant_id AND v.id=a.virployee_id
	WHERE a.tenant_id=$1 AND a.pool_id=$2 AND a.subject_id=$3`

func (r *Repository) ensureActiveJobRole(ctx context.Context, tenantID string, id uuid.UUID) error {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM job_roles
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL)`, tenantID, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domainerr.NotFoundf("active job role", id.String())
	}
	return nil
}

func (r *Repository) ensureVirployeeExists(ctx context.Context, tenantID string, id uuid.UUID, active bool) error {
	return ensureVirployeeExistsWith(ctx, r.pool, tenantID, id, active)
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func ensureVirployeeExistsWith(ctx context.Context, q queryer, tenantID string, id uuid.UUID, active bool) error {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM virployees
		WHERE tenant_id=$1 AND id=$2 AND (NOT $3 OR (archived_at IS NULL AND trashed_at IS NULL)))`, tenantID, id, active).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domainerr.NotFoundf("virployee", id.String())
	}
	return nil
}

func (r *Repository) listRelationships(ctx context.Context, q queryer, tenantID string, virployeeID uuid.UUID) ([]VirployeeRelationship, error) {
	rows, err := q.Query(ctx, `SELECT id,tenant_id,virployee_id,subject_id,relationship_type,is_primary,created_at,updated_at
		FROM companion_virployee_relationships WHERE tenant_id=$1 AND virployee_id=$2
		ORDER BY relationship_type,is_primary DESC,subject_id`, tenantID, virployeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VirployeeRelationship, 0)
	for rows.Next() {
		item, err := scanRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func lockPool(ctx context.Context, tx pgx.Tx, tenantID string, poolID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenantID+":"+poolID.String())
	return err
}

type rowScanner interface {
	Scan(...any) error
}

func scanWorkSubject(row rowScanner) (WorkSubject, error) {
	var out WorkSubject
	if err := row.Scan(&out.ID, &out.TenantID, &out.Kind, &out.DisplayName, &out.ExternalRef, &out.CreatedAt, &out.UpdatedAt, &out.ArchivedAt); err != nil {
		return WorkSubject{}, scanError(err, "work subject")
	}
	return out, nil
}

func scanRoutingPool(row rowScanner) (RoutingPool, error) {
	var out RoutingPool
	if err := row.Scan(&out.ID, &out.TenantID, &out.JobRoleID, &out.Name, &out.CreatedAt, &out.UpdatedAt, &out.ArchivedAt); err != nil {
		return RoutingPool{}, scanError(err, "routing pool")
	}
	return out, nil
}

func scanPoolMember(row rowScanner) (PoolMember, error) {
	var out PoolMember
	if err := row.Scan(&out.TenantID, &out.PoolID, &out.VirployeeID, &out.MaxActiveSubjects, &out.Enabled, &out.ActiveSubjects, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return PoolMember{}, scanError(err, "routing pool member")
	}
	return out, nil
}

func scanRelationship(row rowScanner) (VirployeeRelationship, error) {
	var out VirployeeRelationship
	if err := row.Scan(&out.ID, &out.TenantID, &out.VirployeeID, &out.SubjectID, &out.RelationshipType, &out.IsPrimary, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return VirployeeRelationship{}, scanError(err, "virployee relationship")
	}
	return out, nil
}

func scanAssignment(row rowScanner) (ContinuityAssignment, error) {
	var out ContinuityAssignment
	if err := row.Scan(&out.ID, &out.TenantID, &out.PoolID, &out.SubjectID, &out.VirployeeID, &out.Status, &out.Version, &out.AssignedAt, &out.UpdatedAt); err != nil {
		return ContinuityAssignment{}, scanError(err, "continuity assignment")
	}
	return out, nil
}

func scanAssignmentEligibility(row rowScanner) (ContinuityAssignment, bool, error) {
	var out ContinuityAssignment
	var eligible bool
	if err := row.Scan(&out.ID, &out.TenantID, &out.PoolID, &out.SubjectID, &out.VirployeeID, &out.Status, &out.Version, &out.AssignedAt, &out.UpdatedAt, &eligible); err != nil {
		return ContinuityAssignment{}, false, scanError(err, "continuity assignment")
	}
	return out, eligible, nil
}

func scanError(err error, resource string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.NotFound(resource + " not found")
	}
	return mapConflict(err, resource+" already exists")
}

func mapConflict(err error, message string) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domainerr.Conflict(message)
	}
	return err
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func (r *Repository) lifecycleResult(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	resource string,
	tag pgconn.CommandTag,
	err error,
	exists func(context.Context, string, uuid.UUID) error,
) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if getErr := exists(ctx, tenantID, id); getErr != nil {
		return getErr
	}
	return domainerr.Conflict(fmt.Sprintf("%s lifecycle transition is invalid", resource))
}
