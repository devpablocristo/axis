package enterpriseops

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/authorization"
	"github.com/devpablocristo/nexus-v2/internal/jobs"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Authorizer interface {
	Check(context.Context, authorization.CheckInput) (authorization.CheckResult, error)
}

type Service struct {
	pool                 *pgxpool.Pool
	jobs                 jobs.Repository
	authorizer           Authorizer
	notificationResolver NotificationDestinationResolver
	notificationSender   NotificationSender
	now                  func() time.Time
}

func NewService(pool *pgxpool.Pool, jobsRepository jobs.Repository, authorizer Authorizer) *Service {
	return &Service{pool: pool, jobs: jobsRepository, authorizer: authorizer, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) authorize(ctx context.Context, organization, actor, role, permission, product, action, resourceType, resourceID string) error {
	if s.authorizer == nil {
		return domainerr.Forbidden("operations authorization is unavailable")
	}
	result, err := s.authorizer.Check(ctx, authorization.CheckInput{OrgID: organization, ActorID: actor, ActorRole: role, Permission: permission, ProductSurface: product, ActionType: action, ResourceType: resourceType, ResourceID: resourceID, RiskClass: "low"})
	if err != nil {
		return domainerr.Forbidden("operations authorization is unavailable")
	}
	if !result.Allowed {
		return domainerr.Forbidden(result.Reason)
	}
	return nil
}

func (s *Service) Overview(ctx context.Context, organization, actor, role, product string) (Overview, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.overview.read", "operations", "overview"); err != nil {
		return Overview{}, err
	}
	out := Overview{
		Service: "nexus", Status: "healthy", Incidents: map[string]int{}, Jobs: map[string]int{},
		Exports: map[string]int{}, ServedProducts: map[string]int{}, GeneratedAt: s.now(),
	}
	if err := groupCounts(ctx, s.pool, `SELECT status,count(*)::int FROM operational_incidents WHERE org_id=$1 GROUP BY status`, []any{organization}, out.Incidents); err != nil {
		return out, err
	}
	if err := groupCounts(ctx, s.pool, `SELECT status,count(*)::int FROM nexus_jobs WHERE org_id=$1 AND ($2='' OR product_surface=$2) GROUP BY status`, []any{organization, product}, out.Jobs); err != nil {
		return out, err
	}
	if err := groupCounts(ctx, s.pool, `SELECT status,count(*)::int FROM enterprise_exports WHERE org_id=$1 GROUP BY status`, []any{organization}, out.Exports); err != nil {
		return out, err
	}
	if err := groupCounts(ctx, s.pool, `
		SELECT lifecycle,count(*)::int
		FROM nexus_product_integrations
		WHERE org_id=$1 AND ($2='' OR product_surface=$2)
		GROUP BY lifecycle
	`, []any{organization, product}, out.ServedProducts); err != nil {
		return out, err
	}
	_ = s.pool.QueryRow(ctx, `SELECT count(*)::int FROM legal_holds WHERE org_id=$1 AND status='active'`, organization).Scan(&out.ActiveHolds)
	if out.Incidents["open"] > 0 || out.Jobs["dead_letter"] > 0 || out.Exports["failed"] > 0 {
		out.Status = "degraded"
	}
	return out, nil
}

func groupCounts(ctx context.Context, q pgxQuerier, query string, args []any, target map[string]int) error {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return err
		}
		target[key] = count
	}
	return rows.Err()
}

func (s *Service) IngestFinding(ctx context.Context, organization, actor, key string, input FindingInput) (Incident, bool, error) {
	input, err := normalizeFinding(input)
	if err != nil {
		return Incident{}, false, err
	}
	organization, actor, key = strings.TrimSpace(organization), strings.TrimSpace(actor), strings.TrimSpace(key)
	if organization == "" || actor == "" {
		return Incident{}, false, domainerr.Validation("trusted organization and actor headers are required")
	}
	if _, parseErr := uuid.Parse(key); parseErr != nil {
		return Incident{}, false, domainerr.Validation("Idempotency-Key must be a UUID")
	}
	tx, err := beginOperation(ctx, s.pool, organization, actor, key)
	if err != nil {
		return Incident{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var operation, existingID string
	err = tx.QueryRow(ctx, `SELECT operation,resource_id FROM nexus_operation_requests WHERE org_id=$1 AND actor_id=$2 AND idempotency_key=$3`, organization, actor, key).Scan(&operation, &existingID)
	if err == nil {
		if operation != "incident.ingest" {
			return Incident{}, false, domainerr.Conflict("Idempotency-Key was already used for another operation")
		}
		id, parseErr := uuid.Parse(existingID)
		if parseErr != nil {
			return Incident{}, false, parseErr
		}
		incident, getErr := getIncident(ctx, tx, organization, id)
		return incident, false, commitResult(ctx, tx, getErr)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Incident{}, false, err
	}
	incident, event, err := upsertIncident(ctx, tx, organization, actor, "companion", input)
	if err != nil {
		return Incident{}, false, err
	}
	response, _ := json.Marshal(map[string]any{"incident_id": incident.ID, "revision": incident.Revision})
	_, err = tx.Exec(ctx, `INSERT INTO nexus_operation_requests(org_id,actor_id,idempotency_key,operation,resource_id,response_json) VALUES($1,$2,$3,'incident.ingest',$4,$5)`, organization, actor, key, incident.ID.String(), response)
	if err != nil {
		return Incident{}, false, err
	}
	if err = enqueueNotification(ctx, tx, organization, incident, event); err != nil {
		return Incident{}, false, err
	}
	return incident, event == "opened", tx.Commit(ctx)
}

func upsertIncident(ctx context.Context, tx pgx.Tx, organization, actor, source string, input FindingInput) (Incident, string, error) {
	id := uuid.New()
	previousStatus := ""
	if err := tx.QueryRow(ctx, `SELECT status FROM operational_incidents WHERE org_id=$1 AND fingerprint=$2 FOR UPDATE`, organization, input.Fingerprint).Scan(&previousStatus); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Incident{}, "", err
	}
	var incident Incident
	var inserted bool
	err := tx.QueryRow(ctx, `INSERT INTO operational_incidents(id,org_id,fingerprint,source,incident_type,resource_type,resource_id,severity,status,state_based,metadata_json)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8,'open',$9,$10)
	ON CONFLICT(org_id,fingerprint) DO UPDATE SET last_seen=now(),occurrence_count=operational_incidents.occurrence_count+1,
	 severity=EXCLUDED.severity,metadata_json=EXCLUDED.metadata_json,consecutive_absent_runs=0,
	 status=CASE WHEN operational_incidents.status='resolved' THEN 'open' ELSE operational_incidents.status END,revision=operational_incidents.revision+1
	RETURNING id,fingerprint,source,incident_type,resource_type,resource_id,severity,status,occurrence_count,state_based,first_seen,last_seen,suppress_until,revision,metadata_json,(xmax=0)`, id, organization, input.Fingerprint, source, input.FindingType, input.ResourceType, input.ResourceID, input.Severity, input.StateBased, input.Metadata).Scan(
		&incident.ID, &incident.Fingerprint, &incident.Source, &incident.IncidentType, &incident.ResourceType, &incident.ResourceID, &incident.Severity, &incident.Status, &incident.OccurrenceCount, &incident.StateBased, &incident.FirstSeen, &incident.LastSeen, &incident.SuppressUntil, &incident.Revision, &incident.Metadata, &inserted)
	if err != nil {
		return incident, "", err
	}
	event := "observed"
	if inserted {
		event = "opened"
	} else if previousStatus == "resolved" {
		event = "reopened"
	}
	_, err = tx.Exec(ctx, `INSERT INTO operational_incident_events(org_id,incident_id,event_type,actor_id,reason_code,revision) VALUES($1,$2,$3,$4,$5,$6)`, organization, incident.ID, event, actor, "reconciliation_finding", incident.Revision)
	return incident, event, err
}

func enqueueNotification(ctx context.Context, tx pgx.Tx, organization string, incident Incident, event string) error {
	payload, _ := json.Marshal(map[string]any{"incident_id": incident.ID, "event_type": event, "severity": incident.Severity, "incident_type": incident.IncidentType, "resource_type": incident.ResourceType, "resource_id": incident.ResourceID, "revision": incident.Revision})
	_, err := tx.Exec(ctx, `INSERT INTO operational_notification_outbox(org_id,incident_id,event_type,dedupe_key,payload_json)
	SELECT $1,$2,$3,$4,$5 FROM operational_notification_policy WHERE org_id=$1 AND enabled=true
	ON CONFLICT(org_id,dedupe_key) DO NOTHING`, organization, incident.ID, event, fmt.Sprintf("%s:%d:%s", incident.ID, incident.Revision, event), payload)
	return err
}

func (s *Service) ListIncidents(ctx context.Context, organization, actor, role, product, status string, limit, offset int) ([]Incident, bool, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.incident.read", "incident", "*"); err != nil {
		return nil, false, err
	}
	limit = normalizeLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT id,fingerprint,source,incident_type,resource_type,resource_id,severity,status,occurrence_count,state_based,first_seen,last_seen,suppress_until,revision,metadata_json FROM operational_incidents WHERE org_id=$1 AND ($2='' OR status=$2) ORDER BY last_seen DESC,id DESC LIMIT $3 OFFSET $4`, organization, status, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		item, scanErr := scanIncident(rows)
		if scanErr != nil {
			return nil, false, scanErr
		}
		out = append(out, item)
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, rows.Err()
}

func (s *Service) ActOnIncident(ctx context.Context, organization, actor, role, product, key, action string, id uuid.UUID, input IncidentActionInput) (Incident, error) {
	permission := "incident." + action
	if err := s.authorize(ctx, organization, actor, role, permission, product, "ops.incident."+action, "incident", id.String()); err != nil {
		return Incident{}, err
	}
	input.ReasonCode = strings.ToLower(strings.TrimSpace(input.ReasonCode))
	key = strings.TrimSpace(key)
	if !codePattern.MatchString(input.ReasonCode) || input.ExpectedRevision < 1 || key == "" {
		return Incident{}, domainerr.Validation("reason_code, expected_revision and Idempotency-Key are required")
	}
	if !oneOf(action, "acknowledge", "suppress", "resolve") {
		return Incident{}, domainerr.Validation("incident action is invalid")
	}
	if action == "suppress" && (input.SuppressUntil == nil || !input.SuppressUntil.After(s.now())) {
		return Incident{}, domainerr.Validation("suppress_until must be in the future")
	}
	tx, err := beginOperation(ctx, s.pool, organization, actor, key)
	if err != nil {
		return Incident{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, e := getOperation(ctx, tx, organization, actor, key, "incident."+action); e != nil {
		return Incident{}, e
	} else if found {
		if existing != id {
			return Incident{}, domainerr.Conflict("Idempotency-Key was already used for another resource")
		}
		x, e := getIncident(ctx, tx, organization, existing)
		return x, commitResult(ctx, tx, e)
	}
	status, event := map[string]string{"acknowledge": "acknowledged", "suppress": "suppressed", "resolve": "resolved"}[action], map[string]string{"acknowledge": "acknowledged", "suppress": "suppressed", "resolve": "resolved"}[action]
	var x Incident
	err = tx.QueryRow(ctx, `UPDATE operational_incidents SET status=$4,suppress_until=$5,revision=revision+1 WHERE org_id=$1 AND id=$2 AND revision=$3 AND status<>'resolved' RETURNING id,fingerprint,source,incident_type,resource_type,resource_id,severity,status,occurrence_count,state_based,first_seen,last_seen,suppress_until,revision,metadata_json`, organization, id, input.ExpectedRevision, status, input.SuppressUntil).Scan(&x.ID, &x.Fingerprint, &x.Source, &x.IncidentType, &x.ResourceType, &x.ResourceID, &x.Severity, &x.Status, &x.OccurrenceCount, &x.StateBased, &x.FirstSeen, &x.LastSeen, &x.SuppressUntil, &x.Revision, &x.Metadata)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.Conflict("incident revision or state changed")
	}
	if err != nil {
		return x, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO operational_incident_events(org_id,incident_id,event_type,actor_id,reason_code,revision)VALUES($1,$2,$3,$4,$5,$6)`, organization, id, event, actor, input.ReasonCode, x.Revision)
	if err != nil {
		return x, err
	}
	if err = putOperation(ctx, tx, organization, actor, key, "incident."+action, id, map[string]any{"incident_id": id, "revision": x.Revision}); err != nil {
		return x, err
	}
	if err = enqueueNotification(ctx, tx, organization, x, event); err != nil {
		return x, err
	}
	return x, tx.Commit(ctx)
}

func (s *Service) RunReconciliation(ctx context.Context, organization, actor, role, product, key string, input ReconciliationInput, scheduled bool) (ReconciliationRun, bool, error) {
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = "detect"
	}
	if !oneOf(mode, "detect", "safe_repair") {
		return ReconciliationRun{}, false, domainerr.Validation("mode must be detect or safe_repair")
	}
	if !scheduled {
		if err := s.authorize(ctx, organization, actor, role, "reconciliation.run", product, "ops.reconciliation.run", "reconciliation", product); err != nil {
			return ReconciliationRun{}, false, err
		}
	}
	if strings.TrimSpace(key) == "" {
		return ReconciliationRun{}, false, domainerr.Validation("Idempotency-Key is required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ReconciliationRun{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	trigger := "manual"
	if scheduled {
		trigger = "scheduled"
	}
	var run ReconciliationRun
	err = tx.QueryRow(ctx, `INSERT INTO nexus_governance_reconciliation_runs(org_id,product_surface,mode,trigger,status,actor_id,idempotency_key)VALUES($1,$2,$3,$4,'running',$5,$6)ON CONFLICT(org_id,product_surface,idempotency_key)DO NOTHING RETURNING id,product_surface,mode,trigger,status,findings_count,repaired_count,report_hash,error_code,started_at,completed_at`, organization, product, mode, trigger, actor, key).Scan(&run.ID, &run.ProductSurface, &run.Mode, &run.Trigger, &run.Status, &run.FindingsCount, &run.RepairedCount, &run.ReportHash, &run.ErrorCode, &run.StartedAt, &run.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, e := getReconciliationByKey(ctx, tx, organization, product, key)
		return existing, false, commitResult(ctx, tx, e)
	}
	if err != nil {
		if uniqueViolation(err) {
			return run, false, domainerr.Conflict("a reconciliation is already running for this scope")
		}
		return run, false, err
	}
	type check struct{ kind, severity, resourceType, repair, query string }
	checks := []check{
		{"approval.expired_pending", "warning", "approval", "automatic_safe", `SELECT id::text FROM approvals WHERE org_id=$1 AND status='pending' AND expires_at<=now()`},
		{"job.expired_lease", "warning", "job", "automatic_safe", `SELECT id::text FROM nexus_jobs WHERE org_id=$1 AND status='running' AND lease_until<now()`},
		{"job.dead_letter", "high", "job", "manual", `SELECT id::text FROM nexus_jobs WHERE org_id=$1 AND status='dead_letter'`},
		{"audit.chain_broken", "critical", "audit_chain", "manual", `WITH ordered AS(SELECT id,chain_scope,previous_hash,lag(event_hash)OVER(PARTITION BY org_id,chain_scope ORDER BY created_at,id) expected FROM audit_events WHERE org_id=$1)SELECT id::text FROM ordered WHERE COALESCE(previous_hash,'')<>COALESCE(expected,'')`},
	}
	fingerprints := []string{}
	repaired := 0
	for _, c := range checks {
		rows, qerr := tx.Query(ctx, c.query, organization)
		if qerr != nil {
			return run, false, qerr
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e := rows.Scan(&id); e != nil {
				rows.Close()
				return run, false, e
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			fp := hash(strings.Join([]string{organization, "nexus", c.kind, c.resourceType, id}, "|"))
			didRepair := false
			if mode == "safe_repair" && c.repair == "automatic_safe" {
				rid, _ := uuid.Parse(id)
				var tag pgconn.CommandTag
				if c.kind == "approval.expired_pending" {
					tag, err = tx.Exec(ctx, `UPDATE approvals SET status='expired',updated_at=now() WHERE org_id=$1 AND id=$2 AND status='pending' AND expires_at<=now()`, organization, rid)
				} else {
					tag, err = tx.Exec(ctx, `UPDATE nexus_jobs SET status=CASE WHEN attempts<max_attempts THEN 'queued' ELSE 'dead_letter' END,run_after=now(),lease_owner='',lease_until=NULL,heartbeat_at=NULL,last_error_code='lease_expired',updated_at=now() WHERE org_id=$1 AND id=$2 AND status='running' AND lease_until<now()`, organization, rid)
				}
				if err != nil {
					return run, false, err
				}
				didRepair = tag.RowsAffected() > 0
				if didRepair {
					repaired++
				}
			}
			_, err = tx.Exec(ctx, `INSERT INTO nexus_governance_reconciliation_findings(run_id,org_id,finding_type,severity,resource_type,resource_id,fingerprint,repair_class,repaired)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, run.ID, organization, c.kind, c.severity, c.resourceType, id, fp, c.repair, didRepair)
			if err != nil {
				return run, false, err
			}
			input := FindingInput{RunID: run.ID.String(), FindingType: c.kind, Severity: c.severity, ResourceType: c.resourceType, ResourceID: id, Fingerprint: fp, StateBased: true, Metadata: json.RawMessage(`{}`)}
			if _, _, err = upsertIncident(ctx, tx, organization, actor, "nexus", input); err != nil {
				return run, false, err
			}
			fingerprints = append(fingerprints, fp)
		}
	}
	sort.Strings(fingerprints)
	reportRaw, _ := json.Marshal(map[string]any{"fingerprints": fingerprints, "repaired": repaired})
	report := hash(string(reportRaw))
	_, err = tx.Exec(ctx, `UPDATE nexus_governance_reconciliation_runs SET status='succeeded',findings_count=$2,repaired_count=$3,report_hash=$4,completed_at=now() WHERE id=$1`, run.ID, len(fingerprints), repaired, report)
	if err != nil {
		return run, false, err
	}
	if scheduled {
		_, err = tx.Exec(ctx, `UPDATE operational_incidents SET consecutive_absent_runs=consecutive_absent_runs+1,status=CASE WHEN consecutive_absent_runs+1>=2 THEN 'resolved' ELSE status END,revision=revision+1 WHERE org_id=$1 AND source='nexus' AND state_based=true AND status IN('open','acknowledged','suppressed') AND NOT(fingerprint=ANY($2::text[]))`, organization, fingerprints)
		if err != nil {
			return run, false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return run, false, err
	}
	out, e := s.GetReconciliation(ctx, organization, actor, role, product, run.ID, scheduled)
	return out, true, e
}

func (s *Service) RunScheduled(ctx context.Context, product string) ([]ReconciliationRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT org_id FROM governance_checks UNION SELECT DISTINCT org_id FROM approvals UNION SELECT DISTINCT org_id FROM functional_role_grants`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	organizations := []string{}
	for rows.Next() {
		var t string
		if e := rows.Scan(&t); e != nil {
			return nil, e
		}
		if strings.TrimSpace(t) != "" {
			organizations = append(organizations, t)
		}
	}
	bucket := s.now().Truncate(15 * time.Minute).Format(time.RFC3339)
	out := []ReconciliationRun{}
	for _, t := range organizations {
		run, _, e := s.RunReconciliation(ctx, t, "system:reconciler", "owner", product, "scheduled:"+product+":"+bucket, ReconciliationInput{Mode: "detect"}, true)
		if e != nil {
			return out, e
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
func (s *Service) ListReconciliations(ctx context.Context, organization, actor, role, product string, limit, offset int) ([]ReconciliationRun, bool, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.reconciliation.read", "reconciliation", "*"); err != nil {
		return nil, false, err
	}
	limit = normalizeLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT id,product_surface,mode,trigger,status,findings_count,repaired_count,report_hash,error_code,started_at,completed_at FROM nexus_governance_reconciliation_runs WHERE org_id=$1 AND($2='' OR product_surface=$2)ORDER BY started_at DESC,id DESC LIMIT $3 OFFSET $4`, organization, product, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []ReconciliationRun{}
	for rows.Next() {
		var x ReconciliationRun
		if e := scanReconciliation(rows, &x); e != nil {
			return nil, false, e
		}
		out = append(out, x)
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, rows.Err()
}
func (s *Service) GetReconciliation(ctx context.Context, organization, actor, role, product string, id uuid.UUID, internal bool) (ReconciliationRun, error) {
	if !internal {
		if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.reconciliation.read", "reconciliation", id.String()); err != nil {
			return ReconciliationRun{}, err
		}
	}
	var x ReconciliationRun
	err := scanReconciliation(s.pool.QueryRow(ctx, `SELECT id,product_surface,mode,trigger,status,findings_count,repaired_count,report_hash,error_code,started_at,completed_at FROM nexus_governance_reconciliation_runs WHERE org_id=$1 AND id=$2`, organization, id), &x)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.NotFoundf("reconciliation", id.String())
	}
	return x, err
}

func (s *Service) ListJobs(ctx context.Context, organization, actor, role, product, status string, limit int) ([]JobView, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.job.read", "job", "*"); err != nil {
		return nil, err
	}
	items, err := s.jobs.List(ctx, organization, product, status, normalizeLimit(limit))
	if err != nil {
		return nil, err
	}
	out := make([]JobView, 0, len(items))
	for _, j := range items {
		out = append(out, s.jobView(ctx, j))
	}
	return out, nil
}
func (s *Service) GetJob(ctx context.Context, organization, actor, role, product string, id uuid.UUID) (JobView, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.job.read", "job", id.String()); err != nil {
		return JobView{}, err
	}
	j, err := s.jobs.Get(ctx, organization, id)
	if errors.Is(err, jobs.ErrJobNotFound) {
		return JobView{}, domainerr.NotFoundf("job", id.String())
	}
	return s.jobView(ctx, j), err
}
func (s *Service) CancelJob(ctx context.Context, organization, actor, role, product, key, reason string, id uuid.UUID) (JobView, error) {
	if err := s.authorize(ctx, organization, actor, role, "job.cancel", product, "ops.job.cancel", "job", id.String()); err != nil {
		return JobView{}, err
	}
	reason, key = strings.ToLower(strings.TrimSpace(reason)), strings.TrimSpace(key)
	if key == "" || !codePattern.MatchString(reason) {
		return JobView{}, domainerr.Validation("Idempotency-Key and reason_code are required")
	}
	tx, err := beginOperation(ctx, s.pool, organization, actor, key)
	if err != nil {
		return JobView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, err := getOperation(ctx, tx, organization, actor, key, "job.cancel"); err != nil {
		return JobView{}, err
	} else if found {
		if existing != id {
			return JobView{}, domainerr.Conflict("Idempotency-Key was already used for another resource")
		}
		if err = tx.Commit(ctx); err != nil {
			return JobView{}, err
		}
		j, getErr := s.jobs.Get(ctx, organization, id)
		return s.jobView(ctx, j), getErr
	}
	var status string
	err = tx.QueryRow(ctx, `UPDATE nexus_jobs SET status=CASE WHEN status='running' THEN 'cancel_requested' ELSE 'cancelled' END,
		cancel_requested_at=now(),last_error_code=$3,completed_at=CASE WHEN status='queued' THEN now() ELSE completed_at END,updated_at=now()
		WHERE org_id=$1 AND id=$2 AND status IN('queued','running') RETURNING status`, organization, id, reason).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return JobView{}, domainerr.Conflict("job cannot be cancelled from its current state")
	}
	if err != nil {
		return JobView{}, err
	}
	metadata, _ := json.Marshal(map[string]string{"reason_code": reason, "actor_id": actor})
	if _, err = tx.Exec(ctx, `INSERT INTO nexus_job_events(job_id,event,metadata_json)VALUES($1,$2,$3)`, id, status, metadata); err != nil {
		return JobView{}, err
	}
	if err = putOperation(ctx, tx, organization, actor, key, "job.cancel", id, map[string]any{"job_id": id, "status": status}); err != nil {
		return JobView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return JobView{}, err
	}
	j, err := s.jobs.Get(ctx, organization, id)
	return s.jobView(ctx, j), err
}
func (s *Service) ReplayJob(ctx context.Context, organization, actor, role, product, key string, id uuid.UUID) (JobView, error) {
	if err := s.authorize(ctx, organization, actor, role, "job.replay", product, "ops.job.replay", "job", id.String()); err != nil {
		return JobView{}, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return JobView{}, domainerr.Validation("Idempotency-Key is required")
	}
	tx, err := beginOperation(ctx, s.pool, organization, actor, key)
	if err != nil {
		return JobView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, err := getOperation(ctx, tx, organization, actor, key, "job.replay"); err != nil {
		return JobView{}, err
	} else if found {
		if existing != id {
			return JobView{}, domainerr.Conflict("Idempotency-Key was already used for another resource")
		}
		if err = tx.Commit(ctx); err != nil {
			return JobView{}, err
		}
		j, getErr := s.jobs.Get(ctx, organization, id)
		return s.jobView(ctx, j), getErr
	}
	var replayPolicy, effect, dedupe string
	err = tx.QueryRow(ctx, `SELECT COALESCE(definition.replay_policy,'forbidden'),COALESCE(definition.effect_class,'external_write'),job.dedupe_key
		FROM nexus_jobs AS job LEFT JOIN nexus_job_definitions AS definition
		ON definition.product_surface=job.product_surface AND definition.kind=job.kind
		WHERE job.org_id=$1 AND job.id=$2 AND job.status='dead_letter' FOR UPDATE OF job`, organization, id).Scan(&replayPolicy, &effect, &dedupe)
	if errors.Is(err, pgx.ErrNoRows) {
		return JobView{}, domainerr.Conflict("job is not in the DLQ")
	}
	if err != nil {
		return JobView{}, err
	}
	if replayPolicy == "forbidden" || (effect == "external_write" && strings.TrimSpace(dedupe) == "") {
		return JobView{}, domainerr.Forbidden("job replay is not safely authorized")
	}
	if _, err = tx.Exec(ctx, `UPDATE nexus_jobs SET status='queued',attempts=0,run_after=$3,lease_owner='',lease_until=NULL,
		locked_at=NULL,heartbeat_at=NULL,last_error_code='',completed_at=NULL,updated_at=now() WHERE org_id=$1 AND id=$2`, organization, id, s.now()); err != nil {
		return JobView{}, err
	}
	metadata, _ := json.Marshal(map[string]string{"actor_id": actor})
	if _, err = tx.Exec(ctx, `INSERT INTO nexus_job_events(job_id,event,metadata_json)VALUES($1,'replayed',$2)`, id, metadata); err != nil {
		return JobView{}, err
	}
	if err = putOperation(ctx, tx, organization, actor, key, "job.replay", id, map[string]any{"job_id": id, "status": "queued"}); err != nil {
		return JobView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return JobView{}, err
	}
	j, err := s.jobs.Get(ctx, organization, id)
	return s.jobView(ctx, j), err
}

func (s *Service) ListSLOs(ctx context.Context, organization, actor, role, product string) ([]SLO, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.slo.read", "slo", "*"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT product_surface,metric_key,comparator,target,window_seconds,minimum_samples,severity,enabled,revision,updated_at FROM operational_slos WHERE org_id=$1 AND($2='' OR product_surface=$2)ORDER BY metric_key`, organization, product)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SLO{}
	for rows.Next() {
		var x SLO
		if e := rows.Scan(&x.ProductSurface, &x.MetricKey, &x.Comparator, &x.Target, &x.WindowSeconds, &x.MinimumSamples, &x.Severity, &x.Enabled, &x.Revision, &x.UpdatedAt); e != nil {
			return nil, e
		}
		x.Status = "informational"
		if x.Enabled {
			value, samples, supported, evaluateErr := s.evaluateSLO(ctx, organization, x)
			if evaluateErr != nil {
				return nil, evaluateErr
			}
			x.SampleCount = samples
			if supported && samples >= x.MinimumSamples {
				x.Value = &value
				x.Status = "breached"
				switch x.Comparator {
				case "gte":
					if value >= x.Target {
						x.Status = "met"
					}
				case "lte":
					if value <= x.Target {
						x.Status = "met"
					}
				case "eq":
					if value == x.Target {
						x.Status = "met"
					}
				}
			} else {
				x.Status = "unknown"
			}
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) evaluateSLO(ctx context.Context, organization string, slo SLO) (float64, int, bool, error) {
	switch slo.MetricKey {
	case "job_success_rate":
		var succeeded, samples int
		err := s.pool.QueryRow(ctx, `SELECT count(*)FILTER(WHERE status='succeeded')::int,count(*)::int FROM nexus_jobs
			WHERE org_id=$1 AND product_surface=$2 AND status IN('succeeded','dead_letter','cancelled') AND completed_at>=now()-make_interval(secs=>$3)`, organization, slo.ProductSurface, slo.WindowSeconds).Scan(&succeeded, &samples)
		if err != nil || samples == 0 {
			return 0, samples, true, err
		}
		return float64(succeeded) / float64(samples), samples, true, nil
	case "oldest_queue_age":
		var value float64
		err := s.pool.QueryRow(ctx, `SELECT COALESCE(EXTRACT(epoch FROM now()-min(run_after)),0) FROM nexus_jobs WHERE org_id=$1 AND product_surface=$2 AND status='queued'`, organization, slo.ProductSurface).Scan(&value)
		return value, 1, true, err
	case "dlq_count":
		var value int
		err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM nexus_jobs WHERE org_id=$1 AND product_surface=$2 AND status='dead_letter'`, organization, slo.ProductSurface).Scan(&value)
		return float64(value), 1, true, err
	case "audit_integrity":
		var samples, broken int
		err := s.pool.QueryRow(ctx, `WITH ordered AS(
			SELECT previous_hash,lag(event_hash)OVER(PARTITION BY org_id,chain_scope ORDER BY created_at,id) expected
			FROM audit_events WHERE org_id=$1 AND created_at>=now()-make_interval(secs=>$2))
			SELECT count(*)::int,count(*)FILTER(WHERE COALESCE(previous_hash,'')<>COALESCE(expected,''))::int FROM ordered`, organization, slo.WindowSeconds).Scan(&samples, &broken)
		if err != nil || samples == 0 {
			return 0, samples, true, err
		}
		if broken == 0 {
			return 1, samples, true, nil
		}
		return 0, samples, true, nil
	default:
		return 0, 0, false, nil
	}
}

func (s *Service) ListWorkerControls(ctx context.Context, organization, actor, role, product string) ([]WorkerControl, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.worker_controls.read", "worker_control", "*"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT product_surface,kind,state,failure_count,opened_until,revision,reason_code,changed_by,updated_at FROM nexus_worker_controls WHERE org_id=$1 AND($2='' OR product_surface=$2)ORDER BY kind`, organization, product)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkerControl{}
	for rows.Next() {
		x := WorkerControl{Service: "nexus"}
		if e := rows.Scan(&x.ProductSurface, &x.JobKind, &x.State, &x.FailureCount, &x.OpenedUntil, &x.Revision, &x.ReasonCode, &x.ChangedBy, &x.UpdatedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) PutWorkerControl(ctx context.Context, organization, actor, role, product string, in PutWorkerControlInput) (WorkerControl, error) {
	in.JobKind = strings.TrimSpace(in.JobKind)
	in.State = strings.ToLower(strings.TrimSpace(in.State))
	in.ReasonCode = strings.ToLower(strings.TrimSpace(in.ReasonCode))
	permission := "worker.resume"
	if in.State == "paused" {
		permission = "worker.pause"
	}
	if err := s.authorize(ctx, organization, actor, role, permission, product, "ops.worker."+in.State, "job_kind", in.JobKind); err != nil {
		return WorkerControl{}, err
	}
	if in.JobKind == "" || !oneOf(in.State, "paused", "closed") || !codePattern.MatchString(in.ReasonCode) {
		return WorkerControl{}, domainerr.Validation("worker control is invalid")
	}
	var protected bool
	_ = s.pool.QueryRow(ctx, `SELECT protected FROM nexus_job_definitions WHERE product_surface=$1 AND kind=$2`, product, in.JobKind).Scan(&protected)
	if protected && in.State == "paused" {
		return WorkerControl{}, domainerr.Conflict("protected recovery jobs cannot be paused")
	}
	x := WorkerControl{Service: "nexus"}
	err := s.pool.QueryRow(ctx, `INSERT INTO nexus_worker_controls(org_id,product_surface,kind,state,revision,changed_by,reason_code)VALUES($1,$2,$3,$4,1,$5,$6)ON CONFLICT(org_id,product_surface,kind)DO UPDATE SET state=EXCLUDED.state,revision=nexus_worker_controls.revision+1,changed_by=EXCLUDED.changed_by,reason_code=EXCLUDED.reason_code,opened_until=NULL,updated_at=now()WHERE $7=0 OR nexus_worker_controls.revision=$7 RETURNING product_surface,kind,state,failure_count,opened_until,revision,reason_code,changed_by,updated_at`, organization, product, in.JobKind, in.State, actor, in.ReasonCode, in.ExpectedVersion).Scan(&x.ProductSurface, &x.JobKind, &x.State, &x.FailureCount, &x.OpenedUntil, &x.Revision, &x.ReasonCode, &x.ChangedBy, &x.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.Conflict("worker control revision changed")
	}
	return x, err
}

func (s *Service) GetNotificationPolicy(ctx context.Context, organization, actor, role, product string) (NotificationPolicy, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.notifications.read", "notification", "policy"); err != nil {
		return NotificationPolicy{}, err
	}
	var x NotificationPolicy
	err := s.pool.QueryRow(ctx, `SELECT enabled,webhook_secret_ref,revision,changed_by,updated_at FROM operational_notification_policy WHERE org_id=$1`, organization).Scan(&x.Enabled, &x.WebhookSecretRef, &x.Revision, &x.ChangedBy, &x.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationPolicy{Revision: 0}, nil
	}
	return x, err
}
func (s *Service) PutNotificationPolicy(ctx context.Context, organization, actor, role string, in PutNotificationPolicyInput) (NotificationPolicy, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" {
		return NotificationPolicy{}, domainerr.Forbidden("notification configuration requires an owner or admin")
	}
	in.WebhookSecretRef = strings.TrimSpace(in.WebhookSecretRef)
	if in.Enabled && in.WebhookSecretRef == "" {
		return NotificationPolicy{}, domainerr.Validation("webhook_secret_ref is required when notifications are enabled")
	}
	var x NotificationPolicy
	err := s.pool.QueryRow(ctx, `INSERT INTO operational_notification_policy(org_id,enabled,webhook_secret_ref,revision,changed_by)VALUES($1,$2,$3,1,$4)ON CONFLICT(org_id)DO UPDATE SET enabled=EXCLUDED.enabled,webhook_secret_ref=EXCLUDED.webhook_secret_ref,revision=operational_notification_policy.revision+1,changed_by=EXCLUDED.changed_by,updated_at=now()WHERE $5=0 OR operational_notification_policy.revision=$5 RETURNING enabled,webhook_secret_ref,revision,changed_by,updated_at`, organization, in.Enabled, in.WebhookSecretRef, actor, in.ExpectedRevision).Scan(&x.Enabled, &x.WebhookSecretRef, &x.Revision, &x.ChangedBy, &x.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.Conflict("notification policy revision changed")
	}
	return x, err
}
func (s *Service) PutSLO(ctx context.Context, organization, actor, role string, in PutSLOInput) (SLO, error) {
	if strings.ToLower(strings.TrimSpace(role)) != "owner" && strings.ToLower(strings.TrimSpace(role)) != "admin" {
		return SLO{}, domainerr.Forbidden("SLO configuration requires an owner or admin")
	}
	in.ProductSurface = strings.ToLower(strings.TrimSpace(in.ProductSurface))
	in.MetricKey = strings.ToLower(strings.TrimSpace(in.MetricKey))
	in.Comparator = strings.ToLower(strings.TrimSpace(in.Comparator))
	in.Severity = strings.ToLower(strings.TrimSpace(in.Severity))
	if in.ProductSurface == "" || !oneOf(in.Comparator, "gte", "lte", "eq") || in.WindowSeconds < 1 || in.MinimumSamples < 1 || !oneOf(in.Severity, "info", "warning", "high", "critical") {
		return SLO{}, domainerr.Validation("SLO configuration is invalid")
	}
	var x SLO
	err := s.pool.QueryRow(ctx, `INSERT INTO operational_slos(org_id,product_surface,metric_key,comparator,target,window_seconds,minimum_samples,severity,enabled,revision,changed_by)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,$10)ON CONFLICT(org_id,product_surface,metric_key)DO UPDATE SET comparator=EXCLUDED.comparator,target=EXCLUDED.target,window_seconds=EXCLUDED.window_seconds,minimum_samples=EXCLUDED.minimum_samples,severity=EXCLUDED.severity,enabled=EXCLUDED.enabled,revision=operational_slos.revision+1,changed_by=EXCLUDED.changed_by,updated_at=now() WHERE $11=0 OR operational_slos.revision=$11 RETURNING product_surface,metric_key,comparator,target,window_seconds,minimum_samples,severity,enabled,revision,updated_at`, organization, in.ProductSurface, in.MetricKey, in.Comparator, in.Target, in.WindowSeconds, in.MinimumSamples, in.Severity, in.Enabled, actor, in.ExpectedRevision).Scan(&x.ProductSurface, &x.MetricKey, &x.Comparator, &x.Target, &x.WindowSeconds, &x.MinimumSamples, &x.Severity, &x.Enabled, &x.Revision, &x.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.Conflict("SLO revision changed")
	}
	x.Status = "unknown"
	return x, err
}

func (s *Service) ListLegalHolds(ctx context.Context, organization, actor, role, product string) ([]LegalHold, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.legal_hold.read", "legal_hold", "*"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,scope_type,scope_id,reason_code,external_reference,status,revision,created_by,created_at,released_by,released_at,release_reason FROM legal_holds WHERE org_id=$1 ORDER BY created_at DESC,id`, organization)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LegalHold{}
	for rows.Next() {
		x, e := scanLegalHold(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) CreateLegalHold(ctx context.Context, organization, actor, role, product, key string, in CreateLegalHoldInput) (LegalHold, bool, error) {
	if err := s.authorize(ctx, organization, actor, role, "legal_hold.create", product, "ops.legal_hold.create", in.ScopeType, in.ScopeID); err != nil {
		return LegalHold{}, false, err
	}
	in.ScopeType = strings.ToLower(strings.TrimSpace(in.ScopeType))
	in.ScopeID = strings.TrimSpace(in.ScopeID)
	in.ReasonCode = strings.ToLower(strings.TrimSpace(in.ReasonCode))
	if !oneOf(in.ScopeType, "organization", "virployee", "work_subject", "case", "audit_chain", "export") || in.ScopeID == "" || !codePattern.MatchString(in.ReasonCode) || key == "" {
		return LegalHold{}, false, domainerr.Validation("legal hold scope, reason and Idempotency-Key are required")
	}
	tx, err := beginOperation(ctx, s.pool, organization, actor, key)
	if err != nil {
		return LegalHold{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, e := getOperation(ctx, tx, organization, actor, key, "legal_hold.create"); e != nil {
		return LegalHold{}, false, e
	} else if found {
		x, e := getLegalHold(ctx, tx, organization, existing)
		return x, false, commitResult(ctx, tx, e)
	}
	id := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO legal_holds(id,org_id,scope_type,scope_id,reason_code,external_reference,created_by)VALUES($1,$2,$3,$4,$5,$6,$7)`, id, organization, in.ScopeType, in.ScopeID, in.ReasonCode, strings.TrimSpace(in.ExternalReference), actor)
	if uniqueViolation(err) {
		return LegalHold{}, false, domainerr.Conflict("an active legal hold already covers this scope")
	}
	if err != nil {
		return LegalHold{}, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO legal_hold_events(org_id,legal_hold_id,event_type,actor_id,reason_code,revision)VALUES($1,$2,'created',$3,$4,1)`, organization, id, actor, in.ReasonCode)
	if err != nil {
		return LegalHold{}, false, err
	}
	if err = putOperation(ctx, tx, organization, actor, key, "legal_hold.create", id, map[string]any{"legal_hold_id": id}); err != nil {
		return LegalHold{}, false, err
	}
	x, e := getLegalHold(ctx, tx, organization, id)
	return x, true, commitResult(ctx, tx, e)
}
func (s *Service) ReleaseLegalHold(ctx context.Context, organization, actor, role, product, key string, id uuid.UUID, in ReleaseLegalHoldInput) (LegalHold, error) {
	if err := s.authorize(ctx, organization, actor, role, "legal_hold.release", product, "ops.legal_hold.release", "legal_hold", id.String()); err != nil {
		return LegalHold{}, err
	}
	in.ReasonCode = strings.ToLower(strings.TrimSpace(in.ReasonCode))
	if key == "" || in.ExpectedRevision < 1 || !codePattern.MatchString(in.ReasonCode) {
		return LegalHold{}, domainerr.Validation("reason, revision and Idempotency-Key are required")
	}
	tx, err := beginOperation(ctx, s.pool, organization, actor, key)
	if err != nil {
		return LegalHold{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, e := getOperation(ctx, tx, organization, actor, key, "legal_hold.release"); e != nil {
		return LegalHold{}, e
	} else if found {
		if existing != id {
			return LegalHold{}, domainerr.Conflict("Idempotency-Key was already used for another resource")
		}
		x, getErr := getLegalHold(ctx, tx, organization, id)
		return x, commitResult(ctx, tx, getErr)
	}
	var x LegalHold
	err = tx.QueryRow(ctx, `UPDATE legal_holds SET status='released',revision=revision+1,released_by=$4,released_at=now(),release_reason=$5 WHERE org_id=$1 AND id=$2 AND revision=$3 AND status='active' RETURNING id,scope_type,scope_id,reason_code,external_reference,status,revision,created_by,created_at,released_by,released_at,release_reason`, organization, id, in.ExpectedRevision, actor, in.ReasonCode).Scan(&x.ID, &x.ScopeType, &x.ScopeID, &x.ReasonCode, &x.ExternalReference, &x.Status, &x.Revision, &x.CreatedBy, &x.CreatedAt, &x.ReleasedBy, &x.ReleasedAt, &x.ReleaseReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.Conflict("legal hold revision or state changed")
	}
	if err != nil {
		return x, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO legal_hold_events(org_id,legal_hold_id,event_type,actor_id,reason_code,revision)VALUES($1,$2,'released',$3,$4,$5)`, organization, id, actor, in.ReasonCode, x.Revision)
	if err != nil {
		return x, err
	}
	if err = putOperation(ctx, tx, organization, actor, key, "legal_hold.release", id, map[string]any{"legal_hold_id": id, "revision": x.Revision}); err != nil {
		return x, err
	}
	return x, tx.Commit(ctx)
}
func (s *Service) EnsureDeletionAllowed(ctx context.Context, organization, scopeType, scopeID string) error {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legal_holds WHERE org_id=$1 AND status='active' AND ((scope_type='organization' AND scope_id IN($1,'*'))OR(scope_type=$2 AND scope_id=$3)))`, organization, scopeType, scopeID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return domainerr.Conflict("resource is protected by an active legal hold")
	}
	return nil
}

var exportQueries = map[string]string{
	"audit":       `SELECT jsonb_build_object('id',id,'chain_scope',chain_scope,'event_type',event_type,'actor_type',actor_type,'actor_id',actor_id,'subject_type',subject_type,'subject_id',subject_id,'summary',summary,'event_hash',event_hash,'created_at',created_at)FROM audit_events WHERE org_id=$1 ORDER BY created_at,id`,
	"approvals":   `SELECT jsonb_build_object('id',id,'action_type',action_type,'target_system',target_system,'target_resource',target_resource,'risk_level',risk_level,'status',status,'requester_id',requester_id,'decided_by',decided_by,'created_at',created_at,'updated_at',updated_at)FROM approvals WHERE org_id=$1 ORDER BY created_at,id`,
	"policies":    `SELECT jsonb_build_object('id',id,'policy_id',policy_id,'version',version,'state',state,'product_surface',product_surface,'action_type_pattern',action_type_pattern,'effect',effect,'content_hash',content_hash,'created_at',created_at)FROM governance_policy_versions WHERE org_id=$1 ORDER BY created_at,id`,
	"role_grants": `SELECT jsonb_build_object('id',id,'user_id',user_id,'role_key',role_key,'product_surface',product_surface,'action_type_pattern',action_type_pattern,'resource_type',resource_type,'resource_id',resource_id,'max_risk_class',max_risk_class,'valid_from',valid_from,'valid_until',valid_until,'revision',revision,'revoked_at',revoked_at)FROM functional_role_grants WHERE org_id=$1 ORDER BY created_at,id`,
	"incidents":   `SELECT jsonb_build_object('id',id,'fingerprint',fingerprint,'source',source,'incident_type',incident_type,'resource_type',resource_type,'resource_id',resource_id,'severity',severity,'status',status,'occurrence_count',occurrence_count,'first_seen',first_seen,'last_seen',last_seen)FROM operational_incidents WHERE org_id=$1 ORDER BY first_seen,id`,
	"legal_holds": `SELECT jsonb_build_object('id',id,'scope_type',scope_type,'scope_id',scope_id,'reason_code',reason_code,'status',status,'revision',revision,'created_at',created_at,'released_at',released_at)FROM legal_holds WHERE org_id=$1 ORDER BY created_at,id`,
}

var scopedExportQueries = map[string]string{
	"audit": `SELECT jsonb_build_object('id',id,'chain_scope',chain_scope,'event_type',event_type,'actor_type',actor_type,'actor_id',actor_id,'subject_type',subject_type,'subject_id',subject_id,'summary',summary,'event_hash',event_hash,'created_at',created_at)
		FROM audit_events WHERE org_id=$1 AND (($2='audit_chain' AND chain_scope=$3) OR ($2<>'audit_chain' AND subject_type=$2 AND subject_id=$3)) ORDER BY created_at,id`,
	"approvals": `SELECT jsonb_build_object('id',id,'action_type',action_type,'target_system',target_system,'target_resource',target_resource,'risk_level',risk_level,'status',status,'requester_id',requester_id,'decided_by',decided_by,'created_at',created_at,'updated_at',updated_at)
		FROM approvals WHERE org_id=$1 AND target_resource=$3 ORDER BY created_at,id`,
	"incidents": `SELECT jsonb_build_object('id',id,'fingerprint',fingerprint,'source',source,'incident_type',incident_type,'resource_type',resource_type,'resource_id',resource_id,'severity',severity,'status',status,'occurrence_count',occurrence_count,'first_seen',first_seen,'last_seen',last_seen)
		FROM operational_incidents WHERE org_id=$1 AND resource_type=$2 AND resource_id=$3 ORDER BY first_seen,id`,
	"legal_holds": `SELECT jsonb_build_object('id',id,'scope_type',scope_type,'scope_id',scope_id,'reason_code',reason_code,'status',status,'revision',revision,'created_at',created_at,'released_at',released_at)
		FROM legal_holds WHERE org_id=$1 AND scope_type=$2 AND scope_id=$3 ORDER BY created_at,id`,
}

func (s *Service) ListExports(ctx context.Context, organization, actor, role, product string) ([]Export, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.export.read", "export", "*"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,scope_type,scope_id,categories_json,status,manifest_json,manifest_hash,error_code,requested_by,requested_at,completed_at,expires_at FROM enterprise_exports WHERE org_id=$1 ORDER BY requested_at DESC,id`, organization)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Export{}
	for rows.Next() {
		x, e := scanExport(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) CreateExport(ctx context.Context, organization, actor, role, product, key string, in CreateExportInput) (Export, bool, error) {
	if err := s.authorize(ctx, organization, actor, role, "export.create", product, "ops.export.create", in.ScopeType, in.ScopeID); err != nil {
		return Export{}, false, err
	}
	in.ScopeType = strings.ToLower(strings.TrimSpace(in.ScopeType))
	in.ScopeID = strings.TrimSpace(in.ScopeID)
	if !oneOf(in.ScopeType, "organization", "virployee", "work_subject", "case", "audit_chain") || in.ScopeID == "" || key == "" || len(in.Categories) == 0 {
		return Export{}, false, domainerr.Validation("export scope, categories and Idempotency-Key are required")
	}
	seen := map[string]bool{}
	for i, c := range in.Categories {
		c = strings.ToLower(strings.TrimSpace(c))
		if _, ok := exportQueries[c]; !ok || seen[c] {
			return Export{}, false, domainerr.Validation("export category is invalid or duplicated")
		}
		if in.ScopeType != "organization" {
			if _, ok := scopedExportQueries[c]; !ok {
				return Export{}, false, domainerr.Validation("export category is not available for the selected scope")
			}
		}
		seen[c] = true
		in.Categories[i] = c
	}
	sort.Strings(in.Categories)
	categories, _ := json.Marshal(in.Categories)
	id := uuid.New()
	var created bool
	var out Export
	err := s.pool.QueryRow(ctx, `INSERT INTO enterprise_exports(id,org_id,scope_type,scope_id,categories_json,idempotency_key,requested_by)VALUES($1,$2,$3,$4,$5,$6,$7)ON CONFLICT(org_id,idempotency_key)DO NOTHING RETURNING id,scope_type,scope_id,categories_json,status,manifest_json,manifest_hash,error_code,requested_by,requested_at,completed_at,expires_at`, id, organization, in.ScopeType, in.ScopeID, categories, key, actor).Scan(&out.ID, &out.ScopeType, &out.ScopeID, &out.Categories, &out.Status, &out.Manifest, &out.ManifestHash, &out.ErrorCode, &out.RequestedBy, &out.RequestedAt, &out.CompletedAt, &out.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		out, err = s.getExportRaw(ctx, organization, key, uuid.Nil)
		return out, false, err
	}
	if err != nil {
		return out, false, err
	}
	created = true
	payload, _ := json.Marshal(map[string]string{"export_id": out.ID.String()})
	_, _, err = s.jobs.Enqueue(ctx, jobs.EnqueueInput{OrgID: organization, ProductSurface: "nexus", Kind: "enterprise.export", ShardKey: organization, DedupeKey: key, Payload: payload, MaxAttempts: 3, Timeout: 10 * time.Minute})
	if err != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE enterprise_exports SET status='failed',error_code='enqueue_failed',completed_at=now() WHERE org_id=$1 AND id=$2`, organization, out.ID)
		return Export{}, false, err
	}
	return out, created, nil
}
func (s *Service) GetExport(ctx context.Context, organization, actor, role, product string, id uuid.UUID) (Export, error) {
	if err := s.authorize(ctx, organization, actor, role, "ops.read", product, "ops.export.read", "export", id.String()); err != nil {
		return Export{}, err
	}
	return s.getExportRaw(ctx, organization, "", id)
}
func (s *Service) ProcessExport(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload struct {
		ExportID string `json:"export_id"`
	}
	if json.Unmarshal(job.Payload, &payload) != nil {
		return nil, jobs.Permanent("invalid_export_job", nil)
	}
	id, err := uuid.Parse(payload.ExportID)
	if err != nil {
		return nil, jobs.Permanent("invalid_export_job", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, jobs.Retryable("export_storage_unavailable", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var categoriesRaw json.RawMessage
	var scopeType, scopeID string
	err = tx.QueryRow(ctx, `UPDATE enterprise_exports SET status='running' WHERE org_id=$1 AND id=$2 AND status IN('queued','running') RETURNING categories_json,scope_type,scope_id`, job.OrgID, id).Scan(&categoriesRaw, &scopeType, &scopeID)
	if err != nil {
		return nil, jobs.Permanent("export_not_queued", err)
	}
	var categories []string
	if json.Unmarshal(categoriesRaw, &categories) != nil {
		return nil, jobs.Permanent("invalid_export_categories", nil)
	}
	manifestFiles := []map[string]any{}
	for _, category := range categories {
		query, ok := exportQueries[category]
		if !ok {
			return nil, jobs.Permanent("invalid_export_category", nil)
		}
		var rows pgx.Rows
		var qerr error
		if scopeType == "organization" {
			rows, qerr = tx.Query(ctx, query, job.OrgID)
		} else if query, ok = scopedExportQueries[category]; ok {
			rows, qerr = tx.Query(ctx, query, job.OrgID, scopeType, scopeID)
		} else {
			return nil, jobs.Permanent("unsupported_export_scope", nil)
		}
		if qerr != nil {
			return nil, jobs.Retryable("export_section_failed", qerr)
		}
		var buffer bytes.Buffer
		writer := bufio.NewWriter(&buffer)
		count := 0
		for rows.Next() {
			var line json.RawMessage
			if e := rows.Scan(&line); e != nil {
				rows.Close()
				return nil, jobs.Retryable("export_section_failed", e)
			}
			_, _ = writer.Write(line)
			_ = writer.WriteByte('\n')
			count++
		}
		rows.Close()
		_ = writer.Flush()
		content := buffer.Bytes()
		digest := hash(string(content))
		name := category + ".jsonl"
		_, err = tx.Exec(ctx, `INSERT INTO enterprise_export_files(export_id,file_name,content,sha256,size_bytes)VALUES($1,$2,$3,$4,$5)ON CONFLICT(export_id,file_name)DO UPDATE SET content=EXCLUDED.content,sha256=EXCLUDED.sha256,size_bytes=EXCLUDED.size_bytes`, id, name, content, digest, len(content))
		if err != nil {
			return nil, jobs.Retryable("export_storage_failed", err)
		}
		manifestFiles = append(manifestFiles, map[string]any{"name": name, "sha256": digest, "size_bytes": len(content), "records": count})
	}
	manifest := map[string]any{"export_id": id, "org_hash": hash(job.OrgID), "scope_type": scopeType, "scope_hash": hash(scopeID), "files": manifestFiles, "generated_at": s.now().Format(time.RFC3339)}
	manifestRaw, _ := json.Marshal(manifest)
	manifestHash := hash(string(manifestRaw))
	expires := s.now().Add(24 * time.Hour)
	_, err = tx.Exec(ctx, `UPDATE enterprise_exports SET status='ready',manifest_json=$3,manifest_hash=$4,artifact_ref=$5,error_code='',completed_at=now(),expires_at=$6 WHERE org_id=$1 AND id=$2`, job.OrgID, id, manifestRaw, manifestHash, "db://enterprise_export_files/"+id.String(), expires)
	if err != nil {
		return nil, jobs.Retryable("export_finalize_failed", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, jobs.Retryable("export_commit_failed", err)
	}
	return json.Marshal(map[string]any{"export_id": id, "manifest_hash": manifestHash, "files": len(manifestFiles)})
}
func (s *Service) CreateDownloadToken(ctx context.Context, organization, actor, role, product string, id uuid.UUID) (DownloadToken, error) {
	if err := s.authorize(ctx, organization, actor, role, "export.download", product, "ops.export.download", "export", id.String()); err != nil {
		return DownloadToken{}, err
	}
	export, err := s.getExportRaw(ctx, organization, "", id)
	if err != nil {
		return DownloadToken{}, err
	}
	if export.Status != "ready" || export.ExpiresAt == nil || !export.ExpiresAt.After(s.now()) {
		return DownloadToken{}, domainerr.Conflict("export is not ready for download")
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return DownloadToken{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	expires := s.now().Add(5 * time.Minute)
	_, err = s.pool.Exec(ctx, `INSERT INTO enterprise_export_download_tokens(token_hash,org_id,export_id,actor_id,manifest_hash,expires_at)VALUES($1,$2,$3,$4,$5,$6)`, hash(token), organization, id, actor, export.ManifestHash, expires)
	if err != nil {
		return DownloadToken{}, err
	}
	return DownloadToken{Token: token, ExportID: id, ManifestHash: export.ManifestHash, ExpiresAt: expires}, nil
}
func (s *Service) RedeemDownload(ctx context.Context, organization, actor, token string, id uuid.UUID) (map[string][]byte, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var manifestHash string
	err = tx.QueryRow(ctx, `UPDATE enterprise_export_download_tokens SET used_at=now() WHERE token_hash=$1 AND org_id=$2 AND actor_id=$3 AND export_id=$4 AND used_at IS NULL AND expires_at>now() RETURNING manifest_hash`, hash(token), organization, actor, id).Scan(&manifestHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", domainerr.Forbidden("download token is invalid or expired")
	}
	if err != nil {
		return nil, "", err
	}
	var currentHash string
	if err = tx.QueryRow(ctx, `SELECT manifest_hash FROM enterprise_exports WHERE org_id=$1 AND id=$2 AND status='ready'`, organization, id).Scan(&currentHash); err != nil || currentHash != manifestHash {
		return nil, "", domainerr.Conflict("export manifest changed")
	}
	rows, err := tx.Query(ctx, `SELECT file_name,content,sha256 FROM enterprise_export_files WHERE export_id=$1 ORDER BY file_name`, id)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	files := map[string][]byte{}
	for rows.Next() {
		var name, digest string
		var content []byte
		if e := rows.Scan(&name, &content, &digest); e != nil {
			return nil, "", e
		}
		if hash(string(content)) != digest {
			return nil, "", domainerr.Conflict("export artifact integrity check failed")
		}
		files[name] = content
	}
	return files, manifestHash, commitResult(ctx, tx, rows.Err())
}

func (s *Service) getExportRaw(ctx context.Context, organization, key string, id uuid.UUID) (Export, error) {
	var row pgx.Row
	if id != uuid.Nil {
		row = s.pool.QueryRow(ctx, `SELECT id,scope_type,scope_id,categories_json,status,manifest_json,manifest_hash,error_code,requested_by,requested_at,completed_at,expires_at FROM enterprise_exports WHERE org_id=$1 AND id=$2`, organization, id)
	} else {
		row = s.pool.QueryRow(ctx, `SELECT id,scope_type,scope_id,categories_json,status,manifest_json,manifest_hash,error_code,requested_by,requested_at,completed_at,expires_at FROM enterprise_exports WHERE org_id=$1 AND idempotency_key=$2`, organization, key)
	}
	out, err := scanExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domainerr.NotFound("export not found")
	}
	return out, err
}

type scanner interface{ Scan(...any) error }
type pgxQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func scanIncident(row scanner) (Incident, error) {
	var x Incident
	err := row.Scan(&x.ID, &x.Fingerprint, &x.Source, &x.IncidentType, &x.ResourceType, &x.ResourceID, &x.Severity, &x.Status, &x.OccurrenceCount, &x.StateBased, &x.FirstSeen, &x.LastSeen, &x.SuppressUntil, &x.Revision, &x.Metadata)
	return x, err
}
func getIncident(ctx context.Context, q queryRower, organization string, id uuid.UUID) (Incident, error) {
	x, err := scanIncident(q.QueryRow(ctx, `SELECT id,fingerprint,source,incident_type,resource_type,resource_id,severity,status,occurrence_count,state_based,first_seen,last_seen,suppress_until,revision,metadata_json FROM operational_incidents WHERE org_id=$1 AND id=$2`, organization, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domainerr.NotFoundf("incident", id.String())
	}
	return x, err
}
func scanReconciliation(row scanner, x *ReconciliationRun) error {
	return row.Scan(&x.ID, &x.ProductSurface, &x.Mode, &x.Trigger, &x.Status, &x.FindingsCount, &x.RepairedCount, &x.ReportHash, &x.ErrorCode, &x.StartedAt, &x.CompletedAt)
}
func getReconciliationByKey(ctx context.Context, q queryRower, organization, product, key string) (ReconciliationRun, error) {
	var x ReconciliationRun
	err := scanReconciliation(q.QueryRow(ctx, `SELECT id,product_surface,mode,trigger,status,findings_count,repaired_count,report_hash,error_code,started_at,completed_at FROM nexus_governance_reconciliation_runs WHERE org_id=$1 AND product_surface=$2 AND idempotency_key=$3`, organization, product, key), &x)
	return x, err
}
func scanLegalHold(row scanner) (LegalHold, error) {
	var x LegalHold
	err := row.Scan(&x.ID, &x.ScopeType, &x.ScopeID, &x.ReasonCode, &x.ExternalReference, &x.Status, &x.Revision, &x.CreatedBy, &x.CreatedAt, &x.ReleasedBy, &x.ReleasedAt, &x.ReleaseReason)
	return x, err
}
func getLegalHold(ctx context.Context, q queryRower, organization string, id uuid.UUID) (LegalHold, error) {
	x, err := scanLegalHold(q.QueryRow(ctx, `SELECT id,scope_type,scope_id,reason_code,external_reference,status,revision,created_by,created_at,released_by,released_at,release_reason FROM legal_holds WHERE org_id=$1 AND id=$2`, organization, id))
	return x, err
}
func scanExport(row scanner) (Export, error) {
	var x Export
	err := row.Scan(&x.ID, &x.ScopeType, &x.ScopeID, &x.Categories, &x.Status, &x.Manifest, &x.ManifestHash, &x.ErrorCode, &x.RequestedBy, &x.RequestedAt, &x.CompletedAt, &x.ExpiresAt)
	return x, err
}
func scanJobHashes(j jobs.Job) (string, string) { return hash(j.DedupeKey), hash(string(j.Payload)) }
func jobView(j jobs.Job) JobView {
	d, p := scanJobHashes(j)
	return JobView{Service: "nexus", ID: j.ID, ProductSurface: j.ProductSurface, Kind: j.Kind, Status: string(j.Status), DedupeKeyHash: d, PayloadHash: p, Attempts: j.Attempts, MaxAttempts: j.MaxAttempts, RunAfter: j.RunAfter, LeaseUntil: j.LeaseUntil, LastErrorCode: j.LastErrorCode, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, CompletedAt: j.CompletedAt}
}

func (s *Service) jobView(ctx context.Context, job jobs.Job) JobView {
	view := jobView(job)
	view.EffectClass, view.ReplayPolicy = "external_write", "forbidden"
	_ = s.pool.QueryRow(ctx, `SELECT effect_class,replay_policy FROM nexus_job_definitions WHERE product_surface=$1 AND kind=$2`, job.ProductSurface, job.Kind).Scan(&view.EffectClass, &view.ReplayPolicy)
	return view
}
func normalizeLimit(v int) int {
	if v <= 0 {
		return 50
	}
	if v > 200 {
		return 200
	}
	return v
}
func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func beginOperation(ctx context.Context, pool *pgxpool.Pool, organization, actor, key string) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
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

func getOperation(ctx context.Context, tx pgx.Tx, organization, actor, key, operation string) (uuid.UUID, bool, error) {
	var existingOperation, resourceID string
	err := tx.QueryRow(ctx, `SELECT operation,resource_id FROM nexus_operation_requests WHERE org_id=$1 AND actor_id=$2 AND idempotency_key=$3`, organization, actor, strings.TrimSpace(key)).Scan(&existingOperation, &resourceID)
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

func putOperation(ctx context.Context, tx pgx.Tx, organization, actor, key, operation string, resourceID uuid.UUID, response any) error {
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO nexus_operation_requests(org_id,actor_id,idempotency_key,operation,resource_id,response_json)VALUES($1,$2,$3,$4,$5,$6)`, organization, actor, strings.TrimSpace(key), operation, resourceID.String(), raw)
	return err
}

func commitResult(ctx context.Context, tx pgx.Tx, err error) error {
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
