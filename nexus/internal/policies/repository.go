package policies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	policydomain "github.com/devpablocristo/nexus/internal/policies/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

// Sentinel errors
var (
	ErrNotFound      = domainerr.NotFound("not found")
	ErrAlreadyExists = errors.New("policy already exists")
	ErrArchived      = errors.New("policy is archived")
)

// ListFilters define los filtros para listar políticas.
type ListFilters struct {
	OrgID           *string // nil = todas; filtrar por org + globales (org_id IS NULL)
	IncludeArchived bool
	EnabledOnly     bool
}

type PolicyVersion struct {
	ID          uuid.UUID
	Version     int
	Status      string
	Mode        string
	Enabled     bool
	Effect      string
	Priority    int
	ContentHash string
	CreatedBy   string
	CreatedAt   time.Time
}

type PolicyPromotion struct {
	ID               uuid.UUID      `json:"id"`
	PolicyArtifactID uuid.UUID      `json:"policy_artifact_id"`
	FromVersionID    *uuid.UUID     `json:"from_version_id,omitempty"`
	ToVersionID      uuid.UUID      `json:"to_version_id"`
	OrgID            *string        `json:"org_id,omitempty"`
	Status           string         `json:"status"`
	RequestedBy      string         `json:"requested_by"`
	ApprovedBy       string         `json:"approved_by,omitempty"`
	EnforcedBy       string         `json:"enforced_by,omitempty"`
	Reason           string         `json:"reason,omitempty"`
	DryRunReport     map[string]any `json:"dry_run_report,omitempty"`
	DryRunHash       string         `json:"dry_run_hash,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	ApprovedAt       *time.Time     `json:"approved_at,omitempty"`
	EnforcedAt       *time.Time     `json:"enforced_at,omitempty"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type PolicyChangelogEntry struct {
	ID               uuid.UUID
	PolicyArtifactID uuid.UUID
	PolicyVersionID  *uuid.UUID
	ActorID          string
	Action           string
	Summary          string
	Data             map[string]any
	CreatedAt        time.Time
}

// Repository define el port de persistencia para políticas.
type Repository interface {
	Create(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error)
	GetByID(ctx context.Context, id uuid.UUID) (policydomain.Policy, error)
	List(ctx context.Context, filters ListFilters) ([]policydomain.Policy, error)
	Update(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error)
	DeleteByID(ctx context.Context, id uuid.UUID) error
	ArchiveByID(ctx context.Context, id uuid.UUID) error
	RestoreByID(ctx context.Context, id uuid.UUID) error
}

// --- Implementación PostgreSQL ---

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectPolicySQL = `
	SELECT id, org_id, name, COALESCE(description, ''), action_type, target_system,
	       expression, effect, risk_override, priority, origin, mode, proposal_id,
	       enabled, shadow_hits, archived_at, created_at, updated_at
	FROM policies`

func (r *PostgresRepository) Create(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error) {
	now := time.Now().UTC()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	p.CreatedAt = now
	p.UpdatedAt = now

	if p.Mode == "" {
		p.Mode = policydomain.PolicyModeEnforced
	}
	tx, err := r.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return policydomain.Policy{}, fmt.Errorf("begin policy create tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO policies (
			id, org_id, name, description, action_type, target_system,
			expression, effect, risk_override, priority, origin, mode, proposal_id,
			enabled, shadow_hits, archived_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
	`,
		p.ID, p.OrgID, p.Name, p.Description, p.ActionType, p.TargetSystem,
		p.Expression, p.Effect, p.RiskOverride, p.Priority, p.Origin, p.Mode, p.ProposalID,
		p.Enabled, p.ShadowHits, p.ArchivedAt, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return policydomain.Policy{}, fmt.Errorf("insert policy: %w", err)
	}
	if err := recordPolicyVersionTx(ctx, tx, p, "create"); err != nil {
		return policydomain.Policy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return policydomain.Policy{}, fmt.Errorf("commit policy create tx: %w", err)
	}
	return p, nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id uuid.UUID) (policydomain.Policy, error) {
	row := r.db.Pool().QueryRow(ctx, selectPolicySQL+` WHERE id = $1`, id)
	p, err := scanPolicy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return policydomain.Policy{}, ErrNotFound
		}
		return policydomain.Policy{}, fmt.Errorf("get policy: %w", err)
	}
	return p, nil
}

func (r *PostgresRepository) List(ctx context.Context, filters ListFilters) ([]policydomain.Policy, error) {
	query := selectPolicySQL + ` WHERE 1=1`
	args := []any{}
	argN := 1

	if filters.OrgID != nil {
		// Policies de la org + globales (org_id IS NULL)
		query += fmt.Sprintf(` AND (org_id = $%d OR org_id IS NULL)`, argN)
		args = append(args, *filters.OrgID)
		argN++
	}
	if !filters.IncludeArchived {
		query += ` AND archived_at IS NULL`
	}
	if filters.EnabledOnly {
		query += fmt.Sprintf(` AND enabled = $%d`, argN)
		args = append(args, true)
		argN++
	}
	query += ` ORDER BY priority ASC, created_at DESC`

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer rows.Close()

	out := make([]policydomain.Policy, 0)
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) Update(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error) {
	p.UpdatedAt = time.Now().UTC()
	tx, err := r.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return policydomain.Policy{}, fmt.Errorf("begin policy update tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE policies SET
			name = $2, description = $3, action_type = $4, target_system = $5,
			expression = $6, effect = $7, risk_override = $8, priority = $9,
			mode = $10, enabled = $11, updated_at = $12
		WHERE id = $1
	`,
		p.ID, p.Name, p.Description, p.ActionType, p.TargetSystem,
		p.Expression, p.Effect, p.RiskOverride, p.Priority,
		p.Mode, p.Enabled, p.UpdatedAt,
	)
	if err != nil {
		return policydomain.Policy{}, fmt.Errorf("update policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return policydomain.Policy{}, ErrNotFound
	}
	if err := recordPolicyVersionTx(ctx, tx, p, "update"); err != nil {
		return policydomain.Policy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return policydomain.Policy{}, fmt.Errorf("commit policy update tx: %w", err)
	}
	return p, nil
}

func (r *PostgresRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin policy delete tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p, err := scanPolicy(tx.QueryRow(ctx, selectPolicySQL+` WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get policy before delete: %w", err)
	}
	if err := recordPolicyLifecycleEventTx(ctx, tx, p, "delete", "Policy deleted via v1 API"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE policy_artifacts
		SET lifecycle_status = 'archived', updated_at = now()
		WHERE legacy_policy_id = $1
	`, id); err != nil {
		return fmt.Errorf("archive policy artifact before delete: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM policies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit policy delete tx: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ArchiveByID(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin policy archive tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p, err := scanPolicy(tx.QueryRow(ctx, selectPolicySQL+` WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get policy before archive: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE policies SET archived_at = now(), updated_at = now()
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("archive policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE policy_artifacts
		SET lifecycle_status = 'archived', updated_at = now()
		WHERE legacy_policy_id = $1
	`, id); err != nil {
		return fmt.Errorf("archive policy artifact: %w", err)
	}
	if err := recordPolicyLifecycleEventTx(ctx, tx, p, "archive", "Policy archived via v1 API"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit policy archive tx: %w", err)
	}
	return nil
}

func (r *PostgresRepository) RestoreByID(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin policy restore tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p, err := scanPolicy(tx.QueryRow(ctx, selectPolicySQL+` WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get policy before restore: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE policies SET archived_at = NULL, updated_at = now()
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("restore policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	p.ArchivedAt = nil
	if _, err := tx.Exec(ctx, `
		UPDATE policy_artifacts
		SET lifecycle_status = $2, updated_at = now()
		WHERE legacy_policy_id = $1
	`, id, lifecycleStatus(p)); err != nil {
		return fmt.Errorf("restore policy artifact: %w", err)
	}
	if err := recordPolicyLifecycleEventTx(ctx, tx, p, "restore", "Policy restored via v1 API"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit policy restore tx: %w", err)
	}
	return nil
}

func (r *PostgresRepository) IncrementShadowHits(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Pool().Exec(ctx,
		`UPDATE policies SET shadow_hits = shadow_hits + 1, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("increment shadow hits: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListVersions(ctx context.Context, legacyPolicyID uuid.UUID) ([]PolicyVersion, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT v.id, v.version, v.status, v.mode, v.enabled, v.effect, v.priority,
		       v.content_hash, v.created_by, v.created_at
		FROM policy_versions v
		JOIN policy_artifacts a ON a.id = v.policy_artifact_id
		WHERE a.legacy_policy_id = $1
		ORDER BY v.version DESC
	`, legacyPolicyID)
	if err != nil {
		return nil, fmt.Errorf("list policy versions: %w", err)
	}
	defer rows.Close()
	out := make([]PolicyVersion, 0)
	for rows.Next() {
		var version PolicyVersion
		if err := rows.Scan(&version.ID, &version.Version, &version.Status, &version.Mode,
			&version.Enabled, &version.Effect, &version.Priority, &version.ContentHash,
			&version.CreatedBy, &version.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan policy version: %w", err)
		}
		out = append(out, version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

func (r *PostgresRepository) RequestPromotion(ctx context.Context, legacyPolicyID uuid.UUID, toVersionID uuid.UUID, actorID, reason string, dryRunReport map[string]any) (PolicyPromotion, error) {
	if toVersionID == uuid.Nil {
		return PolicyPromotion{}, domainerr.Validation("to_version_id is required")
	}
	if len(dryRunReport) == 0 {
		return PolicyPromotion{}, domainerr.Validation("dry_run_report is required before policy promotion")
	}
	hash, err := hashJSON(dryRunReport)
	if err != nil {
		return PolicyPromotion{}, err
	}
	actorID = firstNonEmpty(actorID, "system")
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return PolicyPromotion{}, fmt.Errorf("begin policy promotion tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var promotion PolicyPromotion
	err = tx.QueryRow(ctx, `
		SELECT a.id, a.org_id, a.current_version_id
		FROM policy_artifacts a
		WHERE a.legacy_policy_id = $1
		FOR UPDATE
	`, legacyPolicyID).Scan(&promotion.PolicyArtifactID, &promotion.OrgID, &promotion.FromVersionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PolicyPromotion{}, ErrNotFound
		}
		return PolicyPromotion{}, fmt.Errorf("get policy artifact for promotion: %w", err)
	}
	var targetArtifact uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT policy_artifact_id
		FROM policy_versions
		WHERE id = $1
	`, toVersionID).Scan(&targetArtifact); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PolicyPromotion{}, ErrNotFound
		}
		return PolicyPromotion{}, fmt.Errorf("get target policy version: %w", err)
	}
	if targetArtifact != promotion.PolicyArtifactID {
		return PolicyPromotion{}, domainerr.Validation("target version does not belong to this policy")
	}
	promotion.ID = uuid.New()
	promotion.ToVersionID = toVersionID
	promotion.Status = "validated"
	promotion.RequestedBy = actorID
	promotion.Reason = strings.TrimSpace(reason)
	promotion.DryRunReport = dryRunReport
	promotion.DryRunHash = hash
	err = tx.QueryRow(ctx, `
		INSERT INTO policy_promotion_requests
			(id, policy_artifact_id, from_version_id, to_version_id, org_id, status, requested_by, reason, dry_run_report, dry_run_hash)
		VALUES ($1,$2,$3,$4,$5,'validated',$6,$7,$8,$9)
		RETURNING id, policy_artifact_id, from_version_id, to_version_id, org_id, status, requested_by,
		          COALESCE(approved_by, ''), COALESCE(enforced_by, ''), COALESCE(reason, ''),
		          dry_run_report, COALESCE(dry_run_hash, ''), created_at, approved_at, enforced_at, updated_at
	`, promotion.ID, promotion.PolicyArtifactID, promotion.FromVersionID, promotion.ToVersionID, promotion.OrgID,
		promotion.RequestedBy, promotion.Reason, promotion.DryRunReport, promotion.DryRunHash).Scan(
		&promotion.ID, &promotion.PolicyArtifactID, &promotion.FromVersionID, &promotion.ToVersionID, &promotion.OrgID,
		&promotion.Status, &promotion.RequestedBy, &promotion.ApprovedBy, &promotion.EnforcedBy, &promotion.Reason,
		&promotion.DryRunReport, &promotion.DryRunHash, &promotion.CreatedAt, &promotion.ApprovedAt, &promotion.EnforcedAt, &promotion.UpdatedAt)
	if err != nil {
		return PolicyPromotion{}, fmt.Errorf("insert policy promotion request: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO policy_changelog (policy_artifact_id, policy_version_id, actor_id, action, summary, data)
		VALUES ($1,$2,$3,'promotion_requested','Policy promotion requested',$4)
	`, promotion.PolicyArtifactID, promotion.ToVersionID, actorID, promotionAuditData(promotion, "", "validated", actorID, "requested", "promotion requested")); err != nil {
		return PolicyPromotion{}, fmt.Errorf("insert promotion changelog: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyPromotion{}, fmt.Errorf("commit policy promotion tx: %w", err)
	}
	return promotion, nil
}

func (r *PostgresRepository) ApprovePromotion(ctx context.Context, promotionID uuid.UUID, actorID string) (PolicyPromotion, error) {
	actorID = firstNonEmpty(actorID, "system")
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return PolicyPromotion{}, fmt.Errorf("begin policy promotion approval tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	promotion, err := getPromotionForUpdate(ctx, tx, promotionID)
	if err != nil {
		return PolicyPromotion{}, err
	}
	if promotion.Status != "validated" && promotion.Status != "pending_approval" {
		return PolicyPromotion{}, domainerr.Conflict("policy promotion is not approvable")
	}
	previousStatus := promotion.Status
	if strings.EqualFold(strings.TrimSpace(promotion.RequestedBy), actorID) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO policy_changelog (policy_artifact_id, policy_version_id, actor_id, action, summary, data)
			VALUES ($1,$2,$3,'promotion_approval_denied','Policy promotion approval denied',$4)
		`, promotion.PolicyArtifactID, promotion.ToVersionID, actorID, promotionAuditData(promotion, promotion.Status, promotion.Status, actorID, "separation_of_duties", "self approval rejected")); err != nil {
			return PolicyPromotion{}, fmt.Errorf("insert promotion approval denial changelog: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return PolicyPromotion{}, fmt.Errorf("commit policy promotion approval denial tx: %w", err)
		}
		return PolicyPromotion{}, domainerr.Conflict("policy promotion requires separation of duties")
	}
	err = tx.QueryRow(ctx, `
		UPDATE policy_promotion_requests
		SET status = 'approved', approved_by = $2, approved_at = now(), updated_at = now()
		WHERE id = $1
		RETURNING id, policy_artifact_id, from_version_id, to_version_id, org_id, status, requested_by,
		          COALESCE(approved_by, ''), COALESCE(enforced_by, ''), COALESCE(reason, ''),
		          dry_run_report, COALESCE(dry_run_hash, ''), created_at, approved_at, enforced_at, updated_at
	`, promotionID, actorID).Scan(&promotion.ID, &promotion.PolicyArtifactID, &promotion.FromVersionID, &promotion.ToVersionID, &promotion.OrgID,
		&promotion.Status, &promotion.RequestedBy, &promotion.ApprovedBy, &promotion.EnforcedBy, &promotion.Reason,
		&promotion.DryRunReport, &promotion.DryRunHash, &promotion.CreatedAt, &promotion.ApprovedAt, &promotion.EnforcedAt, &promotion.UpdatedAt)
	if err != nil {
		return PolicyPromotion{}, fmt.Errorf("approve policy promotion: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO policy_changelog (policy_artifact_id, policy_version_id, actor_id, action, summary, data)
		VALUES ($1,$2,$3,'promotion_approved','Policy promotion approved',$4)
	`, promotion.PolicyArtifactID, promotion.ToVersionID, actorID, promotionAuditData(promotion, previousStatus, "approved", actorID, "approved", "promotion approved")); err != nil {
		return PolicyPromotion{}, fmt.Errorf("insert promotion approval changelog: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyPromotion{}, fmt.Errorf("commit policy promotion approval tx: %w", err)
	}
	return promotion, nil
}

func (r *PostgresRepository) EnforcePromotion(ctx context.Context, promotionID uuid.UUID, actorID string) (PolicyPromotion, error) {
	return r.applyPromotionVersion(ctx, promotionID, actorID, false)
}

func (r *PostgresRepository) RollbackPromotion(ctx context.Context, promotionID uuid.UUID, actorID, reason string) (PolicyPromotion, error) {
	_ = strings.TrimSpace(reason)
	return r.applyPromotionVersion(ctx, promotionID, firstNonEmpty(actorID, "system"), true)
}

func (r *PostgresRepository) ListPromotions(ctx context.Context, legacyPolicyID uuid.UUID) ([]PolicyPromotion, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT pr.id, pr.policy_artifact_id, pr.from_version_id, pr.to_version_id, pr.org_id, pr.status,
		       pr.requested_by, COALESCE(pr.approved_by, ''), COALESCE(pr.enforced_by, ''), COALESCE(pr.reason, ''),
		       pr.dry_run_report, COALESCE(pr.dry_run_hash, ''), pr.created_at, pr.approved_at, pr.enforced_at, pr.updated_at
		FROM policy_promotion_requests pr
		JOIN policy_artifacts a ON a.id = pr.policy_artifact_id
		WHERE a.legacy_policy_id = $1
		ORDER BY pr.created_at DESC
	`, legacyPolicyID)
	if err != nil {
		return nil, fmt.Errorf("list policy promotions: %w", err)
	}
	defer rows.Close()
	out := make([]PolicyPromotion, 0)
	for rows.Next() {
		promotion, err := scanPromotion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, promotion)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetPromotion(ctx context.Context, promotionID uuid.UUID) (PolicyPromotion, error) {
	promotion, err := scanPromotion(r.db.Pool().QueryRow(ctx, `
		SELECT id, policy_artifact_id, from_version_id, to_version_id, org_id, status, requested_by,
		       COALESCE(approved_by, ''), COALESCE(enforced_by, ''), COALESCE(reason, ''),
		       dry_run_report, COALESCE(dry_run_hash, ''), created_at, approved_at, enforced_at, updated_at
		FROM policy_promotion_requests
		WHERE id = $1
	`, promotionID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PolicyPromotion{}, ErrNotFound
		}
		return PolicyPromotion{}, fmt.Errorf("get policy promotion: %w", err)
	}
	return promotion, nil
}

func (r *PostgresRepository) ListChangelog(ctx context.Context, legacyPolicyID uuid.UUID) ([]PolicyChangelogEntry, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT c.id, c.policy_artifact_id, c.policy_version_id, c.actor_id, c.action, c.summary, c.data, c.created_at
		FROM policy_changelog c
		JOIN policy_artifacts a ON a.id = c.policy_artifact_id
		WHERE a.legacy_policy_id = $1
		ORDER BY c.created_at DESC
	`, legacyPolicyID)
	if err != nil {
		return nil, fmt.Errorf("list policy changelog: %w", err)
	}
	defer rows.Close()

	out := make([]PolicyChangelogEntry, 0)
	for rows.Next() {
		var entry PolicyChangelogEntry
		var versionID uuid.NullUUID
		var rawData []byte
		if err := rows.Scan(&entry.ID, &entry.PolicyArtifactID, &versionID, &entry.ActorID, &entry.Action, &entry.Summary, &rawData, &entry.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan policy changelog: %w", err)
		}
		if versionID.Valid {
			id := versionID.UUID
			entry.PolicyVersionID = &id
		}
		entry.Data = make(map[string]any)
		if len(rawData) > 0 {
			if err := json.Unmarshal(rawData, &entry.Data); err != nil {
				return nil, fmt.Errorf("decode policy changelog data: %w", err)
			}
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

func (r *PostgresRepository) applyPromotionVersion(ctx context.Context, promotionID uuid.UUID, actorID string, rollback bool) (PolicyPromotion, error) {
	actorID = firstNonEmpty(actorID, "system")
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return PolicyPromotion{}, fmt.Errorf("begin policy promotion enforce tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	promotion, err := getPromotionForUpdate(ctx, tx, promotionID)
	if err != nil {
		return PolicyPromotion{}, err
	}
	previousPromotionStatus := promotion.Status
	if !rollback && promotion.Status != "approved" {
		return PolicyPromotion{}, domainerr.Conflict("policy promotion must be approved before enforce")
	}
	targetVersionID := promotion.ToVersionID
	finalStatus := "enforced"
	action := "promotion_enforced"
	summary := "Policy promotion enforced"
	if rollback {
		if promotion.FromVersionID == nil {
			return PolicyPromotion{}, domainerr.Conflict("policy promotion has no rollback target")
		}
		targetVersionID = *promotion.FromVersionID
		finalStatus = "rolled_back"
		action = "promotion_rolled_back"
		summary = "Policy promotion rolled back"
	}
	if !rollback {
		var frozen bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM policy_freeze_windows
				WHERE starts_at <= now()
				  AND ends_at > now()
				  AND (org_id IS NULL OR org_id = $1)
			)
		`, promotion.OrgID).Scan(&frozen); err != nil {
			return PolicyPromotion{}, fmt.Errorf("check policy freeze window: %w", err)
		}
		if frozen {
			return PolicyPromotion{}, domainerr.Conflict("policy promotion blocked by active freeze window")
		}
	}

	var version PolicyVersion
	var legacyPolicyID *uuid.UUID
	var expression, effect, mode string
	var riskOverride, actionType, targetSystem *string
	err = tx.QueryRow(ctx, `
		SELECT v.id, v.version, v.status, v.mode, v.enabled, v.effect, v.priority,
		       v.content_hash, v.created_by, v.created_at, a.legacy_policy_id,
		       v.expression, v.risk_override, v.action_type, v.target_system
		FROM policy_versions v
		JOIN policy_artifacts a ON a.id = v.policy_artifact_id
		WHERE v.id = $1 AND a.id = $2
		FOR UPDATE
	`, targetVersionID, promotion.PolicyArtifactID).Scan(&version.ID, &version.Version, &version.Status, &mode,
		&version.Enabled, &effect, &version.Priority, &version.ContentHash, &version.CreatedBy, &version.CreatedAt,
		&legacyPolicyID, &expression, &riskOverride, &actionType, &targetSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PolicyPromotion{}, ErrNotFound
		}
		return PolicyPromotion{}, fmt.Errorf("get promotion target version: %w", err)
	}
	if legacyPolicyID == nil {
		return PolicyPromotion{}, domainerr.Conflict("policy has no legacy facade to enforce")
	}
	status := versionStatusFromMode(mode, version.Enabled)
	if _, err := tx.Exec(ctx, `
		UPDATE policies
		SET expression = $2, effect = $3, risk_override = $4, priority = $5,
		    action_type = $6, target_system = $7, mode = $8, enabled = $9,
		    updated_at = now(), archived_at = NULL
		WHERE id = $1
	`, *legacyPolicyID, expression, effect, riskOverride, version.Priority, actionType, targetSystem, mode, version.Enabled); err != nil {
		return PolicyPromotion{}, fmt.Errorf("update policy facade from promotion: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE policy_versions
		SET status = CASE WHEN id = $2 THEN $3 ELSE 'deprecated' END
		WHERE policy_artifact_id = $1
	`, promotion.PolicyArtifactID, targetVersionID, status); err != nil {
		return PolicyPromotion{}, fmt.Errorf("update policy version statuses: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE policy_artifacts
		SET current_version_id = $2, lifecycle_status = $3, updated_at = now()
		WHERE id = $1
	`, promotion.PolicyArtifactID, targetVersionID, status); err != nil {
		return PolicyPromotion{}, fmt.Errorf("update policy artifact current version: %w", err)
	}
	err = tx.QueryRow(ctx, `
		UPDATE policy_promotion_requests
		SET status = $2, enforced_by = $3, enforced_at = now(), updated_at = now()
		WHERE id = $1
		RETURNING id, policy_artifact_id, from_version_id, to_version_id, org_id, status, requested_by,
		          COALESCE(approved_by, ''), COALESCE(enforced_by, ''), COALESCE(reason, ''),
		          dry_run_report, COALESCE(dry_run_hash, ''), created_at, approved_at, enforced_at, updated_at
	`, promotionID, finalStatus, actorID).Scan(&promotion.ID, &promotion.PolicyArtifactID, &promotion.FromVersionID, &promotion.ToVersionID, &promotion.OrgID,
		&promotion.Status, &promotion.RequestedBy, &promotion.ApprovedBy, &promotion.EnforcedBy, &promotion.Reason,
		&promotion.DryRunReport, &promotion.DryRunHash, &promotion.CreatedAt, &promotion.ApprovedAt, &promotion.EnforcedAt, &promotion.UpdatedAt)
	if err != nil {
		return PolicyPromotion{}, fmt.Errorf("update policy promotion status: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO policy_promotions
			(policy_artifact_id, from_version_id, to_version_id, from_status, to_status, requested_by, approved_by, reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, promotion.PolicyArtifactID, promotion.FromVersionID, targetVersionID, version.Status, status, promotion.RequestedBy, promotion.ApprovedBy, promotion.Reason); err != nil {
		return PolicyPromotion{}, fmt.Errorf("insert policy promotion lineage: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO policy_changelog (policy_artifact_id, policy_version_id, actor_id, action, summary, data)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, promotion.PolicyArtifactID, targetVersionID, actorID, action, summary, promotionAuditData(promotion, previousPromotionStatus, finalStatus, actorID, action, firstNonEmpty(summary, action))); err != nil {
		return PolicyPromotion{}, fmt.Errorf("insert promotion enforce changelog: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyPromotion{}, fmt.Errorf("commit policy promotion enforce tx: %w", err)
	}
	return promotion, nil
}

func getPromotionForUpdate(ctx context.Context, tx pgx.Tx, promotionID uuid.UUID) (PolicyPromotion, error) {
	promotion, err := scanPromotion(tx.QueryRow(ctx, `
		SELECT id, policy_artifact_id, from_version_id, to_version_id, org_id, status, requested_by,
		       COALESCE(approved_by, ''), COALESCE(enforced_by, ''), COALESCE(reason, ''),
		       dry_run_report, COALESCE(dry_run_hash, ''), created_at, approved_at, enforced_at, updated_at
		FROM policy_promotion_requests
		WHERE id = $1
		FOR UPDATE
	`, promotionID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PolicyPromotion{}, ErrNotFound
		}
		return PolicyPromotion{}, fmt.Errorf("get policy promotion: %w", err)
	}
	return promotion, nil
}

// --- Scanner ---

type policyScanRow interface {
	Scan(dest ...any) error
}

func scanPromotion(row policyScanRow) (PolicyPromotion, error) {
	var promotion PolicyPromotion
	if err := row.Scan(&promotion.ID, &promotion.PolicyArtifactID, &promotion.FromVersionID, &promotion.ToVersionID,
		&promotion.OrgID, &promotion.Status, &promotion.RequestedBy, &promotion.ApprovedBy, &promotion.EnforcedBy,
		&promotion.Reason, &promotion.DryRunReport, &promotion.DryRunHash, &promotion.CreatedAt,
		&promotion.ApprovedAt, &promotion.EnforcedAt, &promotion.UpdatedAt); err != nil {
		return PolicyPromotion{}, err
	}
	if promotion.DryRunReport == nil {
		promotion.DryRunReport = make(map[string]any)
	}
	return promotion, nil
}

func scanPolicy(row policyScanRow) (policydomain.Policy, error) {
	var p policydomain.Policy
	if err := row.Scan(
		&p.ID, &p.OrgID, &p.Name, &p.Description, &p.ActionType, &p.TargetSystem,
		&p.Expression, &p.Effect, &p.RiskOverride, &p.Priority, &p.Origin, &p.Mode, &p.ProposalID,
		&p.Enabled, &p.ShadowHits, &p.ArchivedAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return policydomain.Policy{}, fmt.Errorf("scan policy: %w", err)
	}
	return p, nil
}

func (r *PostgresRepository) recordPolicyVersion(ctx context.Context, p policydomain.Policy, action string) error {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin policy lifecycle tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := recordPolicyVersionTx(ctx, tx, p, action); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit policy lifecycle tx: %w", err)
	}
	return nil
}

func recordPolicyVersionTx(ctx context.Context, tx pgx.Tx, p policydomain.Policy, action string) error {
	contentHash, err := policyContentHash(p)
	if err != nil {
		return err
	}

	var artifactID uuid.UUID
	status := lifecycleStatus(p)
	err = tx.QueryRow(ctx, `
		INSERT INTO policy_artifacts (legacy_policy_id, org_id, name, description, lifecycle_status, created_by)
		VALUES ($1,$2,$3,$4,$5,'legacy-api')
		ON CONFLICT (legacy_policy_id) DO UPDATE SET
			org_id = EXCLUDED.org_id,
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			lifecycle_status = EXCLUDED.lifecycle_status,
			updated_at = now()
		RETURNING id
	`, p.ID, p.OrgID, p.Name, p.Description, status).Scan(&artifactID)
	if err != nil {
		return fmt.Errorf("upsert policy artifact: %w", err)
	}

	var version int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM policy_versions WHERE policy_artifact_id = $1`, artifactID).Scan(&version); err != nil {
		return fmt.Errorf("next policy version: %w", err)
	}
	var versionID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO policy_versions
			(policy_artifact_id, version, expression, effect, risk_override, priority, action_type,
			 target_system, mode, enabled, status, content_hash, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'legacy-api')
		RETURNING id
	`, artifactID, version, p.Expression, p.Effect, p.RiskOverride, p.Priority, p.ActionType,
		p.TargetSystem, p.Mode, p.Enabled, status, contentHash).Scan(&versionID)
	if err != nil {
		return fmt.Errorf("insert policy version: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE policy_artifacts SET current_version_id = $1, updated_at = now() WHERE id = $2`, versionID, artifactID); err != nil {
		return fmt.Errorf("set current policy version: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO policy_changelog (policy_artifact_id, policy_version_id, actor_id, action, summary, data)
		VALUES ($1,$2,'legacy-api',$3,$4,$5)
	`, artifactID, versionID, action, "Policy "+action+" via v1 API", map[string]any{"legacy_policy_id": p.ID.String(), "content_hash": contentHash}); err != nil {
		return fmt.Errorf("insert policy changelog: %w", err)
	}
	return nil
}

func (r *PostgresRepository) recordPolicyLifecycleEvent(ctx context.Context, p policydomain.Policy, action, summary string) error {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin policy lifecycle event tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := recordPolicyLifecycleEventTx(ctx, tx, p, action, summary); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit policy lifecycle event tx: %w", err)
	}
	return nil
}

func recordPolicyLifecycleEventTx(ctx context.Context, tx pgx.Tx, p policydomain.Policy, action, summary string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO policy_changelog (policy_artifact_id, actor_id, action, summary, data)
		SELECT id, 'legacy-api', $2, $3, $4
		FROM policy_artifacts
		WHERE legacy_policy_id = $1
	`, p.ID, action, summary, map[string]any{"legacy_policy_id": p.ID.String()})
	if err != nil {
		return fmt.Errorf("insert policy lifecycle event: %w", err)
	}
	return nil
}

func policyContentHash(p policydomain.Policy) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"name":          p.Name,
		"description":   p.Description,
		"action_type":   p.ActionType,
		"target_system": p.TargetSystem,
		"expression":    p.Expression,
		"effect":        p.Effect,
		"risk_override": p.RiskOverride,
		"priority":      p.Priority,
		"mode":          p.Mode,
		"enabled":       p.Enabled,
	})
	if err != nil {
		return "", fmt.Errorf("marshal policy content: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func lifecycleStatus(p policydomain.Policy) string {
	if p.ArchivedAt != nil {
		return "archived"
	}
	if !p.Enabled {
		return "deprecated"
	}
	if p.Mode == policydomain.PolicyModeShadow {
		return "shadow"
	}
	return "enforced"
}

func versionStatusFromMode(mode string, enabled bool) string {
	if !enabled {
		return "deprecated"
	}
	if mode == string(policydomain.PolicyModeShadow) {
		return "shadow"
	}
	return "enforced"
}

func hashJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal hash payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func promotionAuditData(p PolicyPromotion, previousStatus, nextStatus, actorID, decision, reason string) map[string]any {
	data := map[string]any{
		"promotion_id":       p.ID.String(),
		"correlation_id":     p.ID.String(),
		"policy_artifact_id": p.PolicyArtifactID.String(),
		"policy_version_id":  p.ToVersionID.String(),
		"to_version_id":      p.ToVersionID.String(),
		"previous_status":    previousStatus,
		"next_status":        nextStatus,
		"requested_by":       strings.TrimSpace(p.RequestedBy),
		"approved_by":        strings.TrimSpace(p.ApprovedBy),
		"enforced_by":        strings.TrimSpace(p.EnforcedBy),
		"actor_id":           strings.TrimSpace(actorID),
		"decision":           strings.TrimSpace(decision),
		"reason":             strings.TrimSpace(reason),
		"dry_run_hash":       strings.TrimSpace(p.DryRunHash),
	}
	if p.FromVersionID != nil {
		data["from_version_id"] = p.FromVersionID.String()
	}
	if p.OrgID != nil {
		data["org_id"] = strings.TrimSpace(*p.OrgID)
	}
	return data
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
