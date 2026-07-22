package professionalauthority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/devpablocristo/companion-v2/internal/outbox"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool        *pgxpool.Pool
	nexusOutbox *outbox.Repository
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, nexusOutbox: outbox.NewRepository(pool)}
}

func (r *Repository) EnsureVirployee(ctx context.Context, orgID string, virployeeID uuid.UUID) error {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM virployees WHERE org_id=$1 AND id=$2 AND trashed_at IS NULL
	)`, orgID, virployeeID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return domainerr.NotFound("virployee not found")
	}
	return nil
}

func (r *Repository) GetScopePolicy(ctx context.Context, orgID string, virployeeID uuid.UUID) (ScopePolicy, error) {
	return scanScopePolicy(r.pool.QueryRow(ctx, `
		SELECT org_id, virployee_id, allowed_topics, prohibited_topics, out_of_scope,
		       revision, created_at, updated_at
		FROM professional_scope_policies
		WHERE org_id=$1 AND virployee_id=$2
	`, orgID, virployeeID))
}

func (r *Repository) PutScopePolicy(ctx context.Context, orgID string, virployeeID uuid.UUID, input PutScopePolicyInput, actorID string, at time.Time) (ScopePolicy, error) {
	allowed, _ := json.Marshal(input.AllowedTopics)
	prohibited, _ := json.Marshal(input.ProhibitedTopics)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ScopePolicy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureVirployeeTx(ctx, tx, orgID, virployeeID); err != nil {
		return ScopePolicy{}, err
	}
	var current int64
	err = tx.QueryRow(ctx, `SELECT revision FROM professional_scope_policies
		WHERE org_id=$1 AND virployee_id=$2 FOR UPDATE`, orgID, virployeeID).Scan(&current)
	var policy ScopePolicy
	switch {
	case errors.Is(err, pgx.ErrNoRows) && input.ExpectedRevision == 0:
		policy, err = scanScopePolicy(tx.QueryRow(ctx, `
			INSERT INTO professional_scope_policies
				(org_id,virployee_id,allowed_topics,prohibited_topics,out_of_scope,revision,created_at,updated_at)
			VALUES ($1,$2,$3::jsonb,$4::jsonb,$5,1,$6,$6)
			RETURNING org_id,virployee_id,allowed_topics,prohibited_topics,out_of_scope,revision,created_at,updated_at
		`, orgID, virployeeID, allowed, prohibited, string(input.OutOfScope), at.UTC()))
	case errors.Is(err, pgx.ErrNoRows):
		return ScopePolicy{}, domainerr.Conflict("scope policy revision does not match")
	case err != nil:
		return ScopePolicy{}, err
	case current != input.ExpectedRevision:
		return ScopePolicy{}, domainerr.Conflict("scope policy revision does not match")
	default:
		policy, err = scanScopePolicy(tx.QueryRow(ctx, `
			UPDATE professional_scope_policies
			SET allowed_topics=$3::jsonb,prohibited_topics=$4::jsonb,out_of_scope=$5,
			    revision=revision+1,updated_at=$6
			WHERE org_id=$1 AND virployee_id=$2
			RETURNING org_id,virployee_id,allowed_topics,prohibited_topics,out_of_scope,revision,created_at,updated_at
		`, orgID, virployeeID, allowed, prohibited, string(input.OutOfScope), at.UTC()))
	}
	if err != nil {
		return ScopePolicy{}, err
	}
	if err := r.enqueueAuthorityEvent(ctx, tx, authorityEvent{
		OrgID: orgID, VirployeeID: &virployeeID, EventType: "scope_policy_changed",
		SubjectType: "scope_policy", SubjectID: virployeeID.String(), ActorID: actorID,
		Revision: policy.Revision, SnapshotHash: hashParts(allowed, prohibited, []byte(input.OutOfScope)), At: at,
	}); err != nil {
		return ScopePolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ScopePolicy{}, err
	}
	return policy, nil
}

func (r *Repository) CreatePolicyPack(ctx context.Context, orgID string, input CreatePolicyPackInput, jobRoleID *uuid.UUID, actorID string, at time.Time) (PolicyPack, error) {
	rules, err := json.Marshal(input.Rules)
	if err != nil {
		return PolicyPack{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PolicyPack{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if jobRoleID != nil {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_roles
			WHERE org_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL)`, orgID, *jobRoleID).Scan(&exists); err != nil {
			return PolicyPack{}, err
		}
		if !exists {
			return PolicyPack{}, domainerr.Validation("job_role_id must reference an active job role in the same organization")
		}
	}
	id := uuid.New()
	pack, err := scanPolicyPack(tx.QueryRow(ctx, `
		INSERT INTO professional_policy_packs
			(id,org_id,policy_key,name,version,job_role_id,rules,revision,active,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,1,true,$8,$8)
		RETURNING id,org_id,policy_key,name,version,job_role_id,rules,revision,active,created_at,updated_at
	`, id, orgID, input.PolicyKey, input.Name, input.Version, jobRoleID, rules, at.UTC()))
	if err != nil {
		if isUniqueViolation(err) {
			return PolicyPack{}, domainerr.Conflict("policy pack version already exists")
		}
		return PolicyPack{}, err
	}
	if err := r.enqueueAuthorityEvent(ctx, tx, authorityEvent{
		OrgID: orgID, EventType: "professional_policy_pack_created", SubjectType: "professional_policy_pack",
		SubjectID: id.String(), ActorID: actorID, Revision: pack.Revision, SnapshotHash: hashParts(rules), At: at,
	}); err != nil {
		return PolicyPack{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyPack{}, err
	}
	return pack, nil
}

func (r *Repository) ListPolicyPacks(ctx context.Context, orgID string) ([]PolicyPack, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id,org_id,policy_key,name,version,job_role_id,rules,revision,active,created_at,updated_at
		FROM professional_policy_packs WHERE org_id=$1 AND active=true
		ORDER BY policy_key,version DESC,id
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPolicyPacks(rows)
}

func (r *Repository) GetPolicyPack(ctx context.Context, orgID string, id uuid.UUID) (PolicyPack, error) {
	return scanPolicyPack(r.pool.QueryRow(ctx, `
		SELECT id,org_id,policy_key,name,version,job_role_id,rules,revision,active,created_at,updated_at
		FROM professional_policy_packs WHERE org_id=$1 AND id=$2 AND active=true
	`, orgID, id))
}

func (r *Repository) GetPolicyBinding(ctx context.Context, orgID string, virployeeID uuid.UUID) (PolicyBinding, error) {
	var out PolicyBinding
	err := r.pool.QueryRow(ctx, `SELECT org_id,virployee_id,revision,created_at,updated_at
		FROM virployee_policy_bindings WHERE org_id=$1 AND virployee_id=$2`, orgID, virployeeID).
		Scan(&out.OrgID, &out.VirployeeID, &out.Revision, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PolicyBinding{}, domainerr.NotFound("policy binding not found")
	}
	if err != nil {
		return PolicyBinding{}, err
	}
	rows, err := r.pool.Query(ctx, `SELECT policy_pack_id FROM virployee_policy_pack_assignments
		WHERE org_id=$1 AND virployee_id=$2 ORDER BY policy_pack_id`, orgID, virployeeID)
	if err != nil {
		return PolicyBinding{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return PolicyBinding{}, err
		}
		out.PolicyPackIDs = append(out.PolicyPackIDs, id)
	}
	return out, rows.Err()
}

func (r *Repository) PutPolicyBinding(ctx context.Context, orgID string, virployeeID uuid.UUID, ids []uuid.UUID, expectedRevision int64, actorID string, at time.Time) (PolicyBinding, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PolicyBinding{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureVirployeeTx(ctx, tx, orgID, virployeeID); err != nil {
		return PolicyBinding{}, err
	}
	if len(ids) > 0 {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM professional_policy_packs
			WHERE org_id=$1 AND active=true AND id=ANY($2::uuid[])`, orgID, ids).Scan(&count); err != nil {
			return PolicyBinding{}, err
		}
		if count != len(ids) {
			return PolicyBinding{}, domainerr.Validation("policy_pack_ids must reference active packs in the same organization")
		}
	}
	var current int64
	err = tx.QueryRow(ctx, `SELECT revision FROM virployee_policy_bindings
		WHERE org_id=$1 AND virployee_id=$2 FOR UPDATE`, orgID, virployeeID).Scan(&current)
	var revision int64
	switch {
	case errors.Is(err, pgx.ErrNoRows) && expectedRevision == 0:
		revision = 1
		if _, err = tx.Exec(ctx, `INSERT INTO virployee_policy_bindings
			(org_id,virployee_id,revision,created_at,updated_at) VALUES ($1,$2,1,$3,$3)`, orgID, virployeeID, at.UTC()); err != nil {
			return PolicyBinding{}, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		return PolicyBinding{}, domainerr.Conflict("policy binding revision does not match")
	case err != nil:
		return PolicyBinding{}, err
	case current != expectedRevision:
		return PolicyBinding{}, domainerr.Conflict("policy binding revision does not match")
	default:
		revision = current + 1
		if _, err = tx.Exec(ctx, `UPDATE virployee_policy_bindings SET revision=$3,updated_at=$4
			WHERE org_id=$1 AND virployee_id=$2`, orgID, virployeeID, revision, at.UTC()); err != nil {
			return PolicyBinding{}, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM virployee_policy_pack_assignments WHERE org_id=$1 AND virployee_id=$2`, orgID, virployeeID); err != nil {
		return PolicyBinding{}, err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO virployee_policy_pack_assignments
			(org_id,virployee_id,policy_pack_id,created_at) VALUES ($1,$2,$3,$4)`, orgID, virployeeID, id, at.UTC()); err != nil {
			return PolicyBinding{}, err
		}
	}
	if err := r.enqueueAuthorityEvent(ctx, tx, authorityEvent{
		OrgID: orgID, VirployeeID: &virployeeID, EventType: "professional_policy_binding_changed",
		SubjectType: "professional_policy_binding", SubjectID: virployeeID.String(), ActorID: actorID,
		Revision: revision, SnapshotHash: hashUUIDs(ids), At: at,
	}); err != nil {
		return PolicyBinding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyBinding{}, err
	}
	return r.GetPolicyBinding(ctx, orgID, virployeeID)
}

func (r *Repository) CreateDelegation(ctx context.Context, orgID string, virployeeID uuid.UUID, input CreateDelegationInput, actorID string, at time.Time) (Delegation, error) {
	scopes, _ := json.Marshal(input.CapabilityScopes)
	products, _ := json.Marshal(input.ProductScopes)
	resources, _ := json.Marshal(input.ResourceScopes)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Delegation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureVirployeeTx(ctx, tx, orgID, virployeeID); err != nil {
		return Delegation{}, err
	}
	id := uuid.New()
	delegation, err := scanDelegation(tx.QueryRow(ctx, `
		INSERT INTO professional_delegations
			(id,org_id,virployee_id,principal_type,principal_id,capability_scopes,product_scopes,resource_scopes,
			 max_risk_class,purpose,granted_by,valid_from,valid_until,revision,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9,$10,$11,$12,$13,1,$14,$14)
		RETURNING id,org_id,virployee_id,principal_type,principal_id,capability_scopes,
		          product_scopes,resource_scopes,max_risk_class,purpose,granted_by,valid_from,valid_until,revision,
		          revoked_at,revoked_by,revocation_reason,reviewed_at,reviewed_by,review_note,created_at,updated_at
	`, id, orgID, virployeeID, input.PrincipalType, input.PrincipalID, scopes, products, resources,
		input.MaxRiskClass, input.Purpose, actorID, *input.ValidFrom, input.ValidUntil, at.UTC()))
	if err != nil {
		return Delegation{}, err
	}
	if err := r.enqueueAuthorityEvent(ctx, tx, authorityEvent{
		OrgID: orgID, VirployeeID: &virployeeID, EventType: "delegation_created",
		SubjectType: "delegation", SubjectID: id.String(), ActorID: actorID,
		Revision: delegation.Revision, SnapshotHash: delegation.ConditionsHash(), At: at,
	}); err != nil {
		return Delegation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Delegation{}, err
	}
	return delegation, nil
}

func (r *Repository) ListDelegations(ctx context.Context, orgID string, virployeeID uuid.UUID) ([]Delegation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id,org_id,virployee_id,principal_type,principal_id,capability_scopes,
		       product_scopes,resource_scopes,max_risk_class,purpose,granted_by,valid_from,valid_until,revision,
		       revoked_at,revoked_by,revocation_reason,reviewed_at,reviewed_by,review_note,created_at,updated_at
		FROM professional_delegations WHERE org_id=$1 AND virployee_id=$2
		ORDER BY created_at DESC,id
	`, orgID, virployeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delegation{}
	for rows.Next() {
		item, err := scanDelegation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) RevokeDelegation(ctx context.Context, orgID string, virployeeID, delegationID uuid.UUID, input RevokeDelegationInput, actorID string, at time.Time) (Delegation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Delegation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	delegation, err := scanDelegation(tx.QueryRow(ctx, `
		UPDATE professional_delegations
		SET revoked_at=$5,revoked_by=$6,revocation_reason=$7,revision=revision+1,updated_at=$5
		WHERE org_id=$1 AND virployee_id=$2 AND id=$3 AND revision=$4 AND revoked_at IS NULL
		RETURNING id,org_id,virployee_id,principal_type,principal_id,capability_scopes,
		          product_scopes,resource_scopes,max_risk_class,purpose,granted_by,valid_from,valid_until,revision,
		          revoked_at,revoked_by,revocation_reason,reviewed_at,reviewed_by,review_note,created_at,updated_at
	`, orgID, virployeeID, delegationID, input.ExpectedRevision, at.UTC(), actorID, input.Reason))
	if domainerr.IsNotFound(err) {
		var exists bool
		if scanErr := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM professional_delegations
			WHERE org_id=$1 AND virployee_id=$2 AND id=$3)`, orgID, virployeeID, delegationID).Scan(&exists); scanErr != nil {
			return Delegation{}, scanErr
		}
		if !exists {
			return Delegation{}, domainerr.NotFound("delegation not found")
		}
		return Delegation{}, domainerr.Conflict("delegation is revoked or revision does not match")
	}
	if err != nil {
		return Delegation{}, err
	}
	if err := r.enqueueAuthorityEvent(ctx, tx, authorityEvent{
		OrgID: orgID, VirployeeID: &virployeeID, EventType: "delegation_revoked",
		SubjectType: "delegation", SubjectID: delegationID.String(), ActorID: actorID,
		Revision: delegation.Revision, SnapshotHash: hashParts([]byte(delegationID.String()), []byte(fmt.Sprint(delegation.Revision))), At: at,
	}); err != nil {
		return Delegation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Delegation{}, err
	}
	return delegation, nil
}

func (r *Repository) ReviewDelegation(ctx context.Context, orgID string, virployeeID, delegationID uuid.UUID, input ReviewDelegationInput, actorID string, at time.Time) (Delegation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Delegation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	delegation, err := scanDelegation(tx.QueryRow(ctx, `UPDATE professional_delegations
		SET reviewed_at=$5,reviewed_by=$6,review_note=$7,revision=revision+1,updated_at=$5
		WHERE org_id=$1 AND virployee_id=$2 AND id=$3 AND revision=$4 AND revoked_at IS NULL AND valid_until>$5
		RETURNING id,org_id,virployee_id,principal_type,principal_id,capability_scopes,
		 product_scopes,resource_scopes,max_risk_class,purpose,granted_by,valid_from,valid_until,revision,
		 revoked_at,revoked_by,revocation_reason,reviewed_at,reviewed_by,review_note,created_at,updated_at`,
		orgID, virployeeID, delegationID, input.ExpectedRevision, at.UTC(), actorID, input.Note))
	if domainerr.IsNotFound(err) {
		return Delegation{}, domainerr.Conflict("delegation is unavailable or revision does not match")
	}
	if err != nil {
		return Delegation{}, err
	}
	if err := r.enqueueAuthorityEvent(ctx, tx, authorityEvent{OrgID: orgID, VirployeeID: &virployeeID,
		EventType: "delegation_reviewed", SubjectType: "delegation", SubjectID: delegationID.String(), ActorID: actorID,
		Revision: delegation.Revision, SnapshotHash: hashParts([]byte(delegation.ConditionsHash()), []byte(fmt.Sprint(delegation.Revision))), At: at}); err != nil {
		return Delegation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Delegation{}, err
	}
	return delegation, nil
}

func (r *Repository) ResolveAuthority(ctx context.Context, orgID string, virployeeID uuid.UUID) (ResolvedAuthority, error) {
	out := ResolvedAuthority{OrgID: orgID, VirployeeID: virployeeID}
	if err := r.pool.QueryRow(ctx, `SELECT job_role_id FROM virployees
		WHERE org_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL`, orgID, virployeeID).Scan(&out.JobRoleID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedAuthority{}, domainerr.NotFound("active virployee not found")
		}
		return ResolvedAuthority{}, err
	}
	scope, err := r.GetScopePolicy(ctx, orgID, virployeeID)
	if err == nil {
		out.Scope = scope
	} else if domainerr.IsNotFound(err) {
		out.Scope = ScopePolicy{OrgID: orgID, VirployeeID: virployeeID, OutOfScope: OutOfScopeAbstain}
	} else {
		return ResolvedAuthority{}, err
	}
	binding, err := r.GetPolicyBinding(ctx, orgID, virployeeID)
	if err == nil {
		out.BindingRevision = binding.Revision
	} else if !domainerr.IsNotFound(err) {
		return ResolvedAuthority{}, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT p.id,p.org_id,p.policy_key,p.name,p.version,p.job_role_id,p.rules,p.revision,p.active,p.created_at,p.updated_at
		FROM professional_policy_packs p
		WHERE p.org_id=$1 AND p.active=true AND (
			(p.job_role_id=$3 AND NOT EXISTS (
				SELECT 1 FROM professional_policy_packs newer
				WHERE newer.org_id=p.org_id AND newer.policy_key=p.policy_key
				  AND newer.job_role_id=p.job_role_id AND newer.active=true AND newer.version>p.version
			)) OR EXISTS (
				SELECT 1 FROM virployee_policy_pack_assignments a
				WHERE a.org_id=p.org_id AND a.policy_pack_id=p.id AND a.virployee_id=$2
			)
		)
		ORDER BY p.id
	`, orgID, virployeeID, out.JobRoleID)
	if err != nil {
		return ResolvedAuthority{}, err
	}
	out.PolicyPacks, err = scanPolicyPacks(rows)
	rows.Close()
	if err != nil {
		return ResolvedAuthority{}, err
	}
	out.Delegations, err = r.ListDelegations(ctx, orgID, virployeeID)
	if err != nil {
		return ResolvedAuthority{}, err
	}
	return out, nil
}

type scanner interface{ Scan(...any) error }

func scanScopePolicy(row scanner) (ScopePolicy, error) {
	var out ScopePolicy
	var allowed, prohibited []byte
	var outOfScope string
	err := row.Scan(&out.OrgID, &out.VirployeeID, &allowed, &prohibited, &outOfScope, &out.Revision, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScopePolicy{}, domainerr.NotFound("scope policy not found")
	}
	if err != nil {
		return ScopePolicy{}, err
	}
	if err := json.Unmarshal(allowed, &out.AllowedTopics); err != nil {
		return ScopePolicy{}, err
	}
	if err := json.Unmarshal(prohibited, &out.ProhibitedTopics); err != nil {
		return ScopePolicy{}, err
	}
	out.OutOfScope = OutOfScope(outOfScope)
	return out, nil
}

func scanPolicyPack(row scanner) (PolicyPack, error) {
	var out PolicyPack
	var jobRoleID *uuid.UUID
	var rules []byte
	err := row.Scan(&out.ID, &out.OrgID, &out.PolicyKey, &out.Name, &out.Version, &jobRoleID, &rules,
		&out.Revision, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PolicyPack{}, domainerr.NotFound("professional policy pack not found")
	}
	if err != nil {
		return PolicyPack{}, err
	}
	out.JobRoleID = jobRoleID
	if err := json.Unmarshal(rules, &out.Rules); err != nil {
		return PolicyPack{}, err
	}
	return out, nil
}

func scanPolicyPacks(rows pgx.Rows) ([]PolicyPack, error) {
	out := []PolicyPack{}
	for rows.Next() {
		item, err := scanPolicyPack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanDelegation(row scanner) (Delegation, error) {
	var out Delegation
	var scopes, products, resources []byte
	err := row.Scan(&out.ID, &out.OrgID, &out.VirployeeID, &out.PrincipalType, &out.PrincipalID,
		&scopes, &products, &resources, &out.MaxRiskClass, &out.Purpose, &out.GrantedBy,
		&out.ValidFrom, &out.ValidUntil, &out.Revision, &out.RevokedAt, &out.RevokedBy,
		&out.RevocationReason, &out.ReviewedAt, &out.ReviewedBy, &out.ReviewNote, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Delegation{}, domainerr.NotFound("delegation not found")
	}
	if err != nil {
		return Delegation{}, err
	}
	if err := json.Unmarshal(scopes, &out.CapabilityScopes); err != nil {
		return Delegation{}, err
	}
	if err := json.Unmarshal(products, &out.ProductScopes); err != nil {
		return Delegation{}, err
	}
	if err := json.Unmarshal(resources, &out.ResourceScopes); err != nil {
		return Delegation{}, err
	}
	return out, nil
}

func ensureVirployeeTx(ctx context.Context, tx pgx.Tx, orgID string, virployeeID uuid.UUID) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM virployees
		WHERE org_id=$1 AND id=$2 AND trashed_at IS NULL)`, orgID, virployeeID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domainerr.NotFound("virployee not found")
	}
	return nil
}

type authorityEvent struct {
	OrgID        string
	VirployeeID  *uuid.UUID
	EventType    string
	SubjectType  string
	SubjectID    string
	ActorID      string
	Revision     int64
	SnapshotHash string
	At           time.Time
}

func (r *Repository) enqueueAuthorityEvent(ctx context.Context, tx pgx.Tx, event authorityEvent) error {
	subjectType, summary, ok := outbox.ProfessionalAuthorityAuditSpec(event.EventType)
	if !ok || event.SubjectType != subjectType {
		return fmt.Errorf("invalid professional authority audit event")
	}
	aggregateID, err := uuid.Parse(event.SubjectID)
	if err != nil || event.Revision <= 0 || event.ActorID == "" || event.SnapshotHash == "" {
		return fmt.Errorf("invalid professional authority audit metadata")
	}
	virployeeID := "service:professional-authority"
	if event.VirployeeID != nil {
		virployeeID = event.VirployeeID.String()
	}
	payload, err := json.Marshal(outbox.NexusAuditEvent{
		VirployeeID:  virployeeID,
		ActorType:    "human",
		ActorID:      event.ActorID,
		SubjectType:  subjectType,
		SubjectID:    event.SubjectID,
		EventType:    event.EventType,
		Summary:      summary,
		Revision:     event.Revision,
		SnapshotHash: event.SnapshotHash,
	})
	if err != nil {
		return err
	}
	_, _, err = r.nexusOutbox.EnqueueTx(ctx, tx, outbox.EnqueueInput{
		ID:            uuid.New(),
		OrgID:         event.OrgID,
		AggregateType: outbox.AggregateTypeProfessionalAuthority,
		AggregateID:   aggregateID,
		Kind:          outbox.KindAuditEvent,
		DedupeKey:     fmt.Sprintf("%s:%s:%d", event.EventType, event.SubjectID, event.Revision),
		Payload:       payload,
	})
	return err
}

func hashParts(parts ...[]byte) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write(part)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashUUIDs(ids []uuid.UUID) string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}
	sort.Strings(values)
	raw, _ := json.Marshal(values)
	return hashParts(raw)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var _ RepositoryPort = (*Repository)(nil)
