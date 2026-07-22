package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Fleet(ctx context.Context, organization, product string) ([]FleetMember, error) {
	rows, err := r.pool.Query(ctx, `
SELECT v.id,v.name,v.job_role_id,COALESCE(j.name,''),v.profile_template_id,v.autonomy,v.grounding_mode,
 (v.archived_at IS NOT NULL OR v.trashed_at IS NOT NULL),
 count(DISTINCT vc.capability_id)::int,count(DISTINCT vc.capability_id) FILTER(WHERE c.promotion_state<>'active' OR c.manifest_hash='' OR c.manifest_hash<>c.conformed_hash OR c.archived_at IS NOT NULL OR c.trashed_at IS NOT NULL)::int,
 count(DISTINCT kb.knowledge_base_id)::int,count(DISTINCT pm.pool_id)::int,COALESCE(sum(DISTINCT pm.max_active_subjects),0)::int,count(DISTINCT a.subject_id)::int,
 count(DISTINCT q.id) FILTER(WHERE q.status IN('queued','running','cancel_requested'))::int,count(DISTINCT ar.id) FILTER(WHERE ar.status='failed' AND ar.started_at>now()-interval '24 hours')::int,
 max(ar.started_at),max(ar.completed_at) FILTER(WHERE ar.status='done'),CASE WHEN sp.virployee_id IS NULL THEN 'missing_scope_policy' ELSE 'valid' END
FROM virployees v LEFT JOIN job_roles j ON j.org_id=v.org_id AND j.id=v.job_role_id
LEFT JOIN virployee_capabilities vc ON vc.org_id=v.org_id AND vc.virployee_id=v.id LEFT JOIN capabilities c ON c.org_id=vc.org_id AND c.id=vc.capability_id
LEFT JOIN companion_knowledge_bindings kb ON kb.org_id=v.org_id AND (kb.virployee_id=v.id OR (kb.scope_type='professional' AND kb.job_role_id=v.job_role_id))
LEFT JOIN companion_routing_pool_members pm ON pm.org_id=v.org_id AND pm.virployee_id=v.id AND pm.enabled LEFT JOIN companion_continuity_assignments a ON a.org_id=pm.org_id AND a.pool_id=pm.pool_id AND a.virployee_id=v.id
LEFT JOIN companion_runtime_jobs q ON q.org_id=v.org_id AND q.product_surface=$2 AND q.payload_json->>'virployee_id'=v.id::text LEFT JOIN companion_assist_runs ar ON ar.org_id=v.org_id AND ar.virployee_id=v.id
LEFT JOIN professional_scope_policies sp ON sp.org_id=v.org_id AND sp.virployee_id=v.id WHERE v.org_id=$1
GROUP BY v.id,v.name,v.job_role_id,j.name,v.profile_template_id,v.autonomy,v.grounding_mode,v.archived_at,v.trashed_at,sp.virployee_id ORDER BY v.name,v.id`, organization, product)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FleetMember{}
	for rows.Next() {
		var x FleetMember
		var inactive bool
		if err := rows.Scan(&x.VirployeeID, &x.Name, &x.JobRoleID, &x.JobRoleName, &x.ProfileTemplateID, &x.Autonomy, &x.GroundingMode, &inactive, &x.CapabilityCount, &x.InvalidCapabilities, &x.KnowledgeBaseCount, &x.PoolCount, &x.MaxActiveSubjects, &x.ActiveSubjects, &x.PendingJobs, &x.RecentErrors, &x.LastRunAt, &x.LastSuccessAt, &x.AuthorityState); err != nil {
			return nil, err
		}
		switch {
		case inactive:
			x.Status = FleetInactive
		case x.JobRoleName == "" || x.InvalidCapabilities > 0 || (x.MaxActiveSubjects > 0 && x.ActiveSubjects > x.MaxActiveSubjects):
			x.Status = FleetBlocked
		case x.RecentErrors > 0 || x.AuthorityState != "valid" || x.PoolCount == 0:
			x.Status = FleetDegraded
		case x.LastRunAt == nil:
			x.Status = FleetUnknown
		default:
			x.Status = FleetReady
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *Repository) Overview(ctx context.Context, t, p string) (Overview, error) {
	fleet, err := r.Fleet(ctx, t, p)
	if err != nil {
		return Overview{}, err
	}
	out := Overview{Service: "companion", Status: "healthy", Fleet: map[string]int{}, Jobs: map[string]int{}, Outbox: map[string]int{}, OpenFindings: map[string]int{}, GeneratedAt: time.Now().UTC()}
	for _, x := range fleet {
		out.Fleet[string(x.Status)]++
	}
	if err = r.groupCounts(ctx, `SELECT status,count(*)::int FROM companion_runtime_jobs WHERE org_id=$1 AND ($2='' OR product_surface=$2) GROUP BY status`, []any{t, p}, out.Jobs); err != nil {
		return out, err
	}
	if err = r.groupCounts(ctx, `SELECT status,count(*)::int FROM companion_nexus_outbox WHERE org_id=$1 GROUP BY status`, []any{t}, out.Outbox); err != nil {
		return out, err
	}
	_ = r.pool.QueryRow(ctx, `SELECT COALESCE(EXTRACT(epoch FROM now()-min(run_after))::bigint,0) FROM companion_runtime_jobs WHERE org_id=$1 AND status='queued'`, t).Scan(&out.OldestQueuedJobAge)
	_ = r.pool.QueryRow(ctx, `SELECT COALESCE(EXTRACT(epoch FROM now()-min(available_at))::bigint,0) FROM companion_nexus_outbox WHERE org_id=$1 AND status='pending'`, t).Scan(&out.OldestOutboxAge)
	if out.Fleet["blocked"] > 0 || out.Jobs["dead_letter"] > 0 || out.Outbox["dead"] > 0 {
		out.Status = "degraded"
	}
	return out, nil
}
func (r *Repository) groupCounts(ctx context.Context, q string, args []any, target map[string]int) error {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		target[k] = n
	}
	return rows.Err()
}

type reconSpec struct{ kind, severity, resourceType, repair, query string }

var reconChecks = []reconSpec{
	{"virployee.invalid_configuration", "critical", "virployee", "manual", `SELECT v.id::text FROM virployees v LEFT JOIN job_roles j ON j.org_id=v.org_id AND j.id=v.job_role_id LEFT JOIN profile_templates p ON p.org_id=v.org_id AND p.id=v.profile_template_id WHERE v.org_id=$1 AND v.archived_at IS NULL AND v.trashed_at IS NULL AND (j.id IS NULL OR j.archived_at IS NOT NULL OR p.id IS NULL OR p.archived_at IS NOT NULL)`},
	{"capability.invalid_assignment", "critical", "virployee", "manual", `SELECT DISTINCT v.id::text FROM virployees v JOIN virployee_capabilities vc ON vc.org_id=v.org_id AND vc.virployee_id=v.id JOIN capabilities c ON c.org_id=vc.org_id AND c.id=vc.capability_id WHERE v.org_id=$1 AND (c.promotion_state<>'active' OR c.manifest_hash='' OR c.manifest_hash<>c.conformed_hash OR c.archived_at IS NOT NULL OR c.trashed_at IS NOT NULL)`},
	{"assignment.invalid_authority", "critical", "assignment", "manual", `SELECT a.id::text FROM companion_continuity_assignments a JOIN companion_routing_pools p ON p.org_id=a.org_id AND p.id=a.pool_id JOIN companion_routing_pool_members m ON m.org_id=a.org_id AND m.pool_id=a.pool_id AND m.virployee_id=a.virployee_id JOIN virployees v ON v.org_id=a.org_id AND v.id=a.virployee_id JOIN companion_work_subjects s ON s.org_id=a.org_id AND s.id=a.subject_id WHERE a.org_id=$1 AND (p.archived_at IS NOT NULL OR NOT m.enabled OR v.archived_at IS NOT NULL OR v.trashed_at IS NOT NULL OR s.archived_at IS NOT NULL OR v.job_role_id<>p.job_role_id)`},
	{"assignment.capacity_exceeded", "high", "virployee", "manual", `SELECT m.virployee_id::text FROM companion_routing_pool_members m LEFT JOIN companion_continuity_assignments a ON a.org_id=m.org_id AND a.pool_id=m.pool_id AND a.virployee_id=m.virployee_id WHERE m.org_id=$1 GROUP BY m.virployee_id,m.pool_id,m.max_active_subjects HAVING count(a.id)>m.max_active_subjects`},
	{"assist.stalled", "warning", "assist_run", "automatic_safe", `SELECT id::text FROM companion_assist_runs WHERE org_id=$1 AND status='running' AND started_at<now()-interval '15 minutes'`},
	{"execution.stalled", "critical", "execution_attempt", "manual", `SELECT id::text FROM companion_execution_attempts WHERE org_id=$1 AND status='running' AND started_at<now()-interval '15 minutes'`},
	{"job.dead_letter", "high", "job", "manual", `SELECT id::text FROM companion_runtime_jobs WHERE org_id=$1 AND status='dead_letter'`},
	{"job.expired_lease", "warning", "job", "automatic_safe", `SELECT id::text FROM companion_runtime_jobs WHERE org_id=$1 AND status='running' AND lease_until<now()`},
	{"outbox.dead", "critical", "outbox", "manual", `SELECT id::text FROM companion_nexus_outbox WHERE org_id=$1 AND status='dead'`},
	{"outbox.expired_lease", "warning", "outbox", "automatic_safe", `SELECT id::text FROM companion_nexus_outbox WHERE org_id=$1 AND status='processing' AND lease_until<now()`},
	{"runtime.orphan_virployee_reference", "critical", "runtime_reference", "manual", `SELECT DISTINCT t.virployee_id::text FROM companion_run_traces t LEFT JOIN virployees v ON v.org_id=t.org_id AND v.id=t.virployee_id WHERE t.org_id=$1 AND v.id IS NULL`},
}

func (r *Repository) CreateAndRunReconciliation(ctx context.Context, t, p, actor string, in CreateReconciliationInput) (ReconciliationRun, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ReconciliationRun{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var run ReconciliationRun
	err = tx.QueryRow(ctx, `INSERT INTO companion_fleet_reconciliation_runs(org_id,product_surface,mode,trigger,status,actor_id,idempotency_key) VALUES($1,$2,$3,$4,'running',$5,$6) ON CONFLICT(org_id,product_surface,idempotency_key) DO NOTHING RETURNING id,org_id,product_surface,mode,trigger,status,actor_id,idempotency_key,findings_count,repaired_count,report_hash,error_code,started_at,completed_at,started_at`, t, p, in.Mode, in.TriggerType, actor, in.IdempotencyKey).Scan(&run.ID, &run.OrgID, &run.ProductSurface, &run.Mode, &run.TriggerType, &run.Status, &run.ActorID, &run.IdempotencyKey, &run.FindingsCount, &run.RepairedCount, &run.ReportHash, &run.ErrorCode, &run.StartedAt, &run.CompletedAt, &run.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, e := r.findRun(ctx, tx, t, in.IdempotencyKey, uuid.Nil)
		if e != nil {
			return ReconciliationRun{}, false, e
		}
		return existing, false, tx.Commit(ctx)
	}
	if err != nil {
		if unique(err) {
			return ReconciliationRun{}, false, domainerr.Conflict("a reconciliation is already running for this scope")
		}
		return ReconciliationRun{}, false, err
	}
	findings, repaired, err := r.detect(ctx, tx, run, in.Mode == string(ModeSafeRepair))
	if err != nil {
		return ReconciliationRun{}, false, err
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Fingerprint < findings[j].Fingerprint })
	fps := make([]string, len(findings))
	for i := range findings {
		fps[i] = findings[i].Fingerprint
	}
	raw, _ := json.Marshal(map[string]any{"fingerprints": fps, "repaired": repaired})
	report := hashSecret(string(raw))
	_, err = tx.Exec(ctx, `UPDATE companion_fleet_reconciliation_runs SET status='succeeded',findings_count=$2,repaired_count=$3,report_hash=$4,completed_at=now() WHERE id=$1`, run.ID, len(findings), repaired, report)
	if err != nil {
		return run, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return run, false, err
	}
	completed, err := r.GetReconciliation(ctx, t, run.ID)
	return completed, true, err
}
func (r *Repository) detect(ctx context.Context, tx pgx.Tx, run ReconciliationRun, repair bool) ([]Finding, int, error) {
	out := []Finding{}
	repaired := 0
	for _, spec := range reconChecks {
		rows, err := tx.Query(ctx, spec.query, run.OrgID)
		if err != nil {
			return nil, repaired, fmt.Errorf("run %s: %w", spec.kind, err)
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, repaired, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			f := Finding{ID: uuid.New(), RunID: run.ID, OrgID: run.OrgID, FindingType: spec.kind, Severity: spec.severity, ResourceType: spec.resourceType, ResourceID: id, Fingerprint: fingerprint(run.OrgID, "companion", spec.kind, spec.resourceType, id), ExpectedHash: hashSecret("healthy"), ObservedHash: hashSecret(spec.kind), RepairClass: spec.repair, Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC()}
			if repair && spec.repair == "automatic_safe" {
				f.Repaired, err = r.safeRepair(ctx, tx, f)
				if err != nil {
					return nil, repaired, err
				}
				if f.Repaired {
					repaired++
				}
			}
			_, err = tx.Exec(ctx, `INSERT INTO companion_fleet_reconciliation_findings(id,run_id,org_id,finding_type,severity,resource_type,resource_id,fingerprint,expected_hash,observed_hash,repair_class,repaired,metadata_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, f.ID, f.RunID, f.OrgID, f.FindingType, f.Severity, f.ResourceType, f.ResourceID, f.Fingerprint, f.ExpectedHash, f.ObservedHash, f.RepairClass, f.Repaired, f.Metadata)
			if err != nil {
				return nil, repaired, err
			}
			payload, _ := json.Marshal(map[string]any{"run_id": run.ID, "finding_type": f.FindingType, "severity": f.Severity, "resource_type": f.ResourceType, "resource_id": f.ResourceID, "fingerprint": f.Fingerprint, "state_based": true, "metadata": map[string]any{}})
			_, err = tx.Exec(ctx, `INSERT INTO companion_nexus_outbox(id,org_id,aggregate_type,aggregate_id,kind,dedupe_key,payload_json) VALUES(gen_random_uuid(),$1,'operational_finding',$2,'operational_finding',$3,$4) ON CONFLICT(org_id,kind,dedupe_key) DO NOTHING`, run.OrgID, f.ID, run.ID.String()+":"+f.Fingerprint, payload)
			if err != nil {
				return nil, repaired, err
			}
			out = append(out, f)
		}
	}
	return out, repaired, nil
}
func (r *Repository) safeRepair(ctx context.Context, tx pgx.Tx, f Finding) (bool, error) {
	id, err := uuid.Parse(f.ResourceID)
	if err != nil {
		return false, nil
	}
	var tag pgconn.CommandTag
	switch f.FindingType {
	case "job.expired_lease":
		tag, err = tx.Exec(ctx, `UPDATE companion_runtime_jobs SET status=CASE WHEN attempts<max_attempts THEN 'queued' ELSE 'dead_letter' END,run_after=now(),lease_owner='',lease_until=NULL,heartbeat_at=NULL,last_error_code='lease_expired',updated_at=now() WHERE org_id=$1 AND id=$2 AND status='running' AND lease_until<now()`, f.OrgID, id)
	case "outbox.expired_lease":
		tag, err = tx.Exec(ctx, `UPDATE companion_nexus_outbox SET status=CASE WHEN attempts<max_attempts THEN 'pending' ELSE 'dead' END,available_at=now(),lease_owner='',lease_until=NULL,heartbeat_at=NULL,last_error_code='lease_expired',updated_at=now() WHERE org_id=$1 AND id=$2 AND status='processing' AND lease_until<now()`, f.OrgID, id)
	case "assist.stalled":
		tag, err = tx.Exec(ctx, `UPDATE companion_assist_runs SET status='failed',error='recovery_required',completed_at=now() WHERE org_id=$1 AND id=$2 AND status='running' AND answered=false`, f.OrgID, id)
	default:
		return false, nil
	}
	return err == nil && tag.RowsAffected() > 0, err
}

func (r *Repository) ListOrgIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT org_id FROM virployees WHERE btrim(org_id)<>'' ORDER BY org_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (r *Repository) ListReconciliations(ctx context.Context, t, p string, l, o int) ([]ReconciliationRun, bool, error) {
	l = normLimit(l)
	rows, err := r.pool.Query(ctx, `SELECT id,org_id,product_surface,mode,trigger,status,actor_id,idempotency_key,findings_count,repaired_count,report_hash,error_code,started_at,completed_at,started_at FROM companion_fleet_reconciliation_runs WHERE org_id=$1 AND ($2='' OR product_surface=$2) ORDER BY started_at DESC,id DESC LIMIT $3 OFFSET $4`, t, p, l+1, o)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []ReconciliationRun{}
	for rows.Next() {
		var x ReconciliationRun
		if err := scanRun(rows, &x); err != nil {
			return nil, false, err
		}
		out = append(out, x)
	}
	more := len(out) > l
	if more {
		out = out[:l]
	}
	return out, more, rows.Err()
}
func (r *Repository) GetReconciliation(ctx context.Context, t string, id uuid.UUID) (ReconciliationRun, error) {
	run, err := r.findRun(ctx, r.pool, t, "", id)
	if err != nil {
		return run, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id,run_id,org_id,finding_type,severity,resource_type,resource_id,fingerprint,expected_hash,observed_hash,repair_class,repaired,metadata_json,created_at FROM companion_fleet_reconciliation_findings WHERE org_id=$1 AND run_id=$2 ORDER BY severity DESC,fingerprint`, t, id)
	if err != nil {
		return run, err
	}
	defer rows.Close()
	for rows.Next() {
		var f Finding
		if err := rows.Scan(&f.ID, &f.RunID, &f.OrgID, &f.FindingType, &f.Severity, &f.ResourceType, &f.ResourceID, &f.Fingerprint, &f.ExpectedHash, &f.ObservedHash, &f.RepairClass, &f.Repaired, &f.Metadata, &f.CreatedAt); err != nil {
			return run, err
		}
		run.Findings = append(run.Findings, f)
	}
	return run, rows.Err()
}

type scanner interface{ Scan(...any) error }
type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *Repository) findRun(ctx context.Context, q queryRower, t, key string, id uuid.UUID) (ReconciliationRun, error) {
	var row pgx.Row
	if id != uuid.Nil {
		row = q.QueryRow(ctx, `SELECT id,org_id,product_surface,mode,trigger,status,actor_id,idempotency_key,findings_count,repaired_count,report_hash,error_code,started_at,completed_at,started_at FROM companion_fleet_reconciliation_runs WHERE org_id=$1 AND id=$2`, t, id)
	} else {
		row = q.QueryRow(ctx, `SELECT id,org_id,product_surface,mode,trigger,status,actor_id,idempotency_key,findings_count,repaired_count,report_hash,error_code,started_at,completed_at,started_at FROM companion_fleet_reconciliation_runs WHERE org_id=$1 AND idempotency_key=$2`, t, key)
	}
	var x ReconciliationRun
	if err := scanRun(row, &x); errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.NotFoundf("reconciliation", id.String())
	} else if err != nil {
		return x, err
	}
	return x, nil
}
func scanRun(row scanner, x *ReconciliationRun) error {
	return row.Scan(&x.ID, &x.OrgID, &x.ProductSurface, &x.Mode, &x.TriggerType, &x.Status, &x.ActorID, &x.IdempotencyKey, &x.FindingsCount, &x.RepairedCount, &x.ReportHash, &x.ErrorCode, &x.StartedAt, &x.CompletedAt, &x.CreatedAt)
}

const jobSelect = `SELECT 'companion',j.id,j.product_surface,j.kind,j.dedupe_key,j.status,COALESCE(d.effect_class,'internal_write'),COALESCE(d.replay_policy,'forbidden'),j.attempts,j.max_attempts,j.run_after,j.lease_until,j.deadline_at,j.last_error_code,j.last_error_code,j.evidence_json,j.created_at,j.updated_at,j.completed_at FROM companion_runtime_jobs j LEFT JOIN companion_job_definitions d ON d.product_surface=j.product_surface AND d.kind=j.kind`

func scanJob(row scanner) (OperationalJob, error) {
	var x OperationalJob
	err := row.Scan(&x.Service, &x.ID, &x.ProductSurface, &x.Kind, &x.DedupeKey, &x.Status, &x.EffectClass, &x.ReplayPolicy, &x.Attempts, &x.MaxAttempts, &x.RunAfter, &x.LeaseUntil, &x.DeadlineAt, &x.LastErrorCode, &x.CancellationCode, &x.Evidence, &x.CreatedAt, &x.UpdatedAt, &x.CompletedAt)
	x.DedupeKeyHash = hashSecret(x.DedupeKey)
	return x, err
}
func (r *Repository) ListJobs(ctx context.Context, t, status, p string, l, o int) ([]OperationalJob, bool, error) {
	l = normLimit(l)
	rows, err := r.pool.Query(ctx, jobSelect+` WHERE j.org_id=$1 AND ($2='' OR j.status=$2) AND ($3='' OR j.product_surface=$3) ORDER BY j.created_at DESC,j.id DESC LIMIT $4 OFFSET $5`, t, status, p, l+1, o)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []OperationalJob{}
	for rows.Next() {
		x, e := scanJob(rows)
		if e != nil {
			return nil, false, e
		}
		out = append(out, x)
	}
	more := len(out) > l
	if more {
		out = out[:l]
	}
	return out, more, rows.Err()
}
func (r *Repository) GetJob(ctx context.Context, t string, id uuid.UUID) (OperationalJob, error) {
	x, err := scanJob(r.pool.QueryRow(ctx, jobSelect+` WHERE j.org_id=$1 AND j.id=$2`, t, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.NotFoundf("job", id.String())
	}
	return x, err
}
func (r *Repository) CancelJob(ctx context.Context, t, actor string, id uuid.UUID, reason, key string) (OperationalJob, error) {
	tx, err := r.operationTx(ctx, t, actor, key)
	if err != nil {
		return OperationalJob{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, err := existingOperation(ctx, tx, t, actor, key, "job.cancel"); err != nil {
		return OperationalJob{}, err
	} else if found {
		x, getErr := scanJob(tx.QueryRow(ctx, jobSelect+` WHERE j.org_id=$1 AND j.id=$2`, t, existing))
		return x, commitResult(ctx, tx, getErr)
	}
	var status string
	err = tx.QueryRow(ctx, `UPDATE companion_runtime_jobs SET status=CASE WHEN status='running' THEN 'cancel_requested' ELSE 'cancelled' END,cancel_requested_at=now(),last_error_code=$3,completed_at=CASE WHEN status='queued' THEN now() ELSE completed_at END,updated_at=now() WHERE org_id=$1 AND id=$2 AND status IN('queued','running') RETURNING status`, t, id, reason).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return OperationalJob{}, domainerr.Conflict("job cannot be cancelled from its current state")
	}
	if err != nil {
		return OperationalJob{}, err
	}
	metadata, _ := json.Marshal(map[string]string{"reason_code": reason, "actor_id": actor})
	if _, err = tx.Exec(ctx, `INSERT INTO companion_runtime_job_events(job_id,event,metadata_json)VALUES($1,$2,$3)`, id, status, metadata); err != nil {
		return OperationalJob{}, err
	}
	x, err := scanJob(tx.QueryRow(ctx, jobSelect+` WHERE j.org_id=$1 AND j.id=$2`, t, id))
	if err != nil {
		return x, err
	}
	if err = storeOperation(ctx, tx, t, actor, key, "job.cancel", id, x); err != nil {
		return x, err
	}
	return x, tx.Commit(ctx)
}
func (r *Repository) ReplayJob(ctx context.Context, t, actor string, id uuid.UUID, key string) (OperationalJob, error) {
	tx, err := r.operationTx(ctx, t, actor, key)
	if err != nil {
		return OperationalJob{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, e := existingOperation(ctx, tx, t, actor, key, "job.replay"); e != nil {
		return OperationalJob{}, e
	} else if found {
		x, e := scanJob(tx.QueryRow(ctx, jobSelect+` WHERE j.org_id=$1 AND j.id=$2`, t, existing))
		if e != nil {
			return x, e
		}
		return x, tx.Commit(ctx)
	}
	var policy, effect, dedupe string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(d.replay_policy,'forbidden'),COALESCE(d.effect_class,'external_write'),j.dedupe_key FROM companion_runtime_jobs j LEFT JOIN companion_job_definitions d ON d.product_surface=j.product_surface AND d.kind=j.kind WHERE j.org_id=$1 AND j.id=$2 AND j.status='dead_letter' FOR UPDATE OF j`, t, id).Scan(&policy, &effect, &dedupe); err != nil {
		return OperationalJob{}, domainerr.Conflict("job is not replayable")
	}
	if policy == "forbidden" || (effect == "external_write" && strings.TrimSpace(dedupe) == "") {
		return OperationalJob{}, domainerr.Forbidden("job replay is not safely idempotent")
	}
	_, err = tx.Exec(ctx, `UPDATE companion_runtime_jobs SET status='queued',attempts=0,run_after=now(),lease_owner='',lease_until=NULL,heartbeat_at=NULL,last_error_code='',completed_at=NULL,updated_at=now() WHERE org_id=$1 AND id=$2`, t, id)
	if err != nil {
		return OperationalJob{}, err
	}
	x, err := scanJob(tx.QueryRow(ctx, jobSelect+` WHERE j.org_id=$1 AND j.id=$2`, t, id))
	if err != nil {
		return x, err
	}
	metadata, _ := json.Marshal(map[string]string{"actor_id": actor})
	if _, err = tx.Exec(ctx, `INSERT INTO companion_runtime_job_events(job_id,event,metadata_json)VALUES($1,'replayed',$2)`, id, metadata); err != nil {
		return x, err
	}
	if err = storeOperation(ctx, tx, t, actor, key, "job.replay", id, x); err != nil {
		return x, err
	}
	return x, tx.Commit(ctx)
}

func (r *Repository) ListOutbox(ctx context.Context, t, status string, l, o int) ([]OutboxMessage, bool, error) {
	l = normLimit(l)
	rows, err := r.pool.Query(ctx, `SELECT id,aggregate_type,aggregate_id,kind,dedupe_key,status,attempts,max_attempts,available_at,lease_until,last_error_code,created_at,updated_at,delivered_at FROM companion_nexus_outbox WHERE org_id=$1 AND ($2='' OR status=$2) ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, t, status, l+1, o)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []OutboxMessage{}
	for rows.Next() {
		var x OutboxMessage
		if err := rows.Scan(&x.ID, &x.AggregateType, &x.AggregateID, &x.Kind, &x.DedupeKey, &x.Status, &x.Attempts, &x.MaxAttempts, &x.AvailableAt, &x.LeaseUntil, &x.LastErrorCode, &x.CreatedAt, &x.UpdatedAt, &x.DeliveredAt); err != nil {
			return nil, false, err
		}
		x.DedupeKeyHash = hashSecret(x.DedupeKey)
		out = append(out, x)
	}
	more := len(out) > l
	if more {
		out = out[:l]
	}
	return out, more, rows.Err()
}
func (r *Repository) ReplayOutbox(ctx context.Context, t, actor string, id uuid.UUID, key string) (OutboxMessage, error) {
	tx, err := r.operationTx(ctx, t, actor, key)
	if err != nil {
		return OutboxMessage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, e := existingOperation(ctx, tx, t, actor, key, "outbox.replay"); e != nil {
		return OutboxMessage{}, e
	} else if found {
		id = existing
	} else {
		tag, e := tx.Exec(ctx, `UPDATE companion_nexus_outbox SET status='pending',attempts=0,available_at=now(),lease_owner='',lease_until=NULL,heartbeat_at=NULL,last_error_code='',delivered_at=NULL,updated_at=now() WHERE org_id=$1 AND id=$2 AND status='dead'`, t, id)
		if e != nil {
			return OutboxMessage{}, e
		}
		if tag.RowsAffected() == 0 {
			return OutboxMessage{}, domainerr.Conflict("outbox message is not replayable")
		}
		if err = storeOperation(ctx, tx, t, actor, key, "outbox.replay", id, map[string]any{"message_id": id}); err != nil {
			return OutboxMessage{}, err
		}
	}
	var x OutboxMessage
	err = tx.QueryRow(ctx, `SELECT id,aggregate_type,aggregate_id,kind,dedupe_key,status,attempts,max_attempts,available_at,lease_until,last_error_code,created_at,updated_at,delivered_at FROM companion_nexus_outbox WHERE org_id=$1 AND id=$2`, t, id).Scan(&x.ID, &x.AggregateType, &x.AggregateID, &x.Kind, &x.DedupeKey, &x.Status, &x.Attempts, &x.MaxAttempts, &x.AvailableAt, &x.LeaseUntil, &x.LastErrorCode, &x.CreatedAt, &x.UpdatedAt, &x.DeliveredAt)
	if err != nil {
		return x, err
	}
	x.DedupeKeyHash = hashSecret(x.DedupeKey)
	return x, tx.Commit(ctx)
}

func (r *Repository) operationTx(ctx context.Context, organization, actor, key string) (pgx.Tx, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	lockKey := strings.Join([]string{organization, actor, strings.TrimSpace(key)}, "\x1f")
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func existingOperation(ctx context.Context, tx pgx.Tx, organization, actor, key, operation string) (uuid.UUID, bool, error) {
	var existingOperation, resourceID string
	err := tx.QueryRow(ctx, `SELECT operation,resource_id FROM companion_operation_requests WHERE org_id=$1 AND actor_id=$2 AND idempotency_key=$3`, organization, actor, strings.TrimSpace(key)).Scan(&existingOperation, &resourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	if existingOperation != operation {
		return uuid.Nil, false, domainerr.Conflict("Idempotency-Key was already used for another operation")
	}
	id, err := uuid.Parse(resourceID)
	return id, true, err
}

func storeOperation(ctx context.Context, tx pgx.Tx, organization, actor, key, operation string, resourceID uuid.UUID, response any) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO companion_operation_requests(org_id,actor_id,idempotency_key,operation,resource_id,response_json)VALUES($1,$2,$3,$4,$5,$6)`, organization, actor, strings.TrimSpace(key), operation, resourceID.String(), raw)
	return err
}

func commitResult(ctx context.Context, tx pgx.Tx, err error) error {
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListWorkerControls(ctx context.Context, t, p string) ([]WorkerControl, error) {
	rows, err := r.pool.Query(ctx, `SELECT gen_random_uuid(),org_id,product_surface,kind,state,revision,failure_count,failure_window_started_at,opened_until,reason_code,changed_by,created_at,updated_at FROM companion_worker_controls WHERE org_id=$1 AND ($2='' OR product_surface=$2) ORDER BY kind`, t, p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkerControl{}
	for rows.Next() {
		var x WorkerControl
		if err := rows.Scan(&x.ID, &x.OrgID, &x.ProductSurface, &x.JobKind, &x.State, &x.Version, &x.FailureCount, &x.FailureWindowStartedAt, &x.OpenedUntil, &x.ReasonCode, &x.ChangedBy, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *Repository) PutWorkerControl(ctx context.Context, t, p, actor string, in PutWorkerControlInput) (WorkerControl, error) {
	var protected bool
	_ = r.pool.QueryRow(ctx, `SELECT protected FROM companion_job_definitions WHERE product_surface=$1 AND kind=$2`, p, in.JobKind).Scan(&protected)
	if protected && in.State == "paused" {
		return WorkerControl{}, domainerr.Conflict("protected recovery jobs cannot be paused")
	}
	var x WorkerControl
	err := r.pool.QueryRow(ctx, `INSERT INTO companion_worker_controls(org_id,product_surface,kind,state,revision,changed_by,reason_code) VALUES($1,$2,$3,$4,1,$5,$6) ON CONFLICT(org_id,product_surface,kind) DO UPDATE SET state=EXCLUDED.state,revision=companion_worker_controls.revision+1,changed_by=EXCLUDED.changed_by,reason_code=EXCLUDED.reason_code,opened_until=NULL,updated_at=now() WHERE $7=0 OR companion_worker_controls.revision=$7 RETURNING gen_random_uuid(),org_id,product_surface,kind,state,revision,failure_count,failure_window_started_at,opened_until,reason_code,changed_by,created_at,updated_at`, t, p, in.JobKind, in.State, actor, in.ReasonCode, in.ExpectedVersion).Scan(&x.ID, &x.OrgID, &x.ProductSurface, &x.JobKind, &x.State, &x.Version, &x.FailureCount, &x.FailureWindowStartedAt, &x.OpenedUntil, &x.ReasonCode, &x.ChangedBy, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.Conflict("worker control revision changed")
	}
	return x, err
}
func normLimit(n int) int {
	if n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}
func unique(err error) bool { var e *pgconn.PgError; return errors.As(err, &e) && e.Code == "23505" }
