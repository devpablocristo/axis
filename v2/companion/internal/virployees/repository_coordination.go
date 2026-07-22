package virployees

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/jobs"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) EnsureAssistCase(ctx context.Context, orgID string, entrypoint uuid.UUID, metadata AssistMetadata) (AssistCase, error) {
	if strings.TrimSpace(metadata.SubjectID) == "" || strings.TrimSpace(metadata.ProductSurface) == "" || strings.TrimSpace(metadata.AssistType) == "" {
		return AssistCase{}, domainerr.Validation("case-enabled assist requires product_surface, assist_type and subject_id")
	}
	productSurface := strings.TrimSpace(metadata.ProductSurface)
	assistType := strings.TrimSpace(metadata.AssistType)
	subjectID := strings.TrimSpace(metadata.SubjectID)
	// A handoff changes owner, not case identity. Resolve by either original
	// entrypoint or current owner before attempting to create a new live case.
	existing, err := r.scanAssistCase(r.pool.QueryRow(ctx, assistCaseSelect+`
		WHERE org_id=$1 AND product_surface=$2 AND assist_type=$3 AND subject_id=$4
		  AND status IN ('open','needs_human')
		  AND (entrypoint_virployee_id=$5 OR owner_virployee_id=$5)
		ORDER BY updated_at DESC,id LIMIT 1
	`, orgID, productSurface, assistType, subjectID, entrypoint))
	if err == nil {
		return existing, nil
	}
	if !domainerr.IsNotFound(err) {
		return AssistCase{}, err
	}

	id := uuid.New()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO companion_assist_cases (
			id,org_id,product_surface,assist_type,subject_id,entrypoint_virployee_id,owner_virployee_id
		) VALUES ($1,$2,$3,$4,$5,$6,$6)
		ON CONFLICT (org_id,product_surface,assist_type,subject_id,entrypoint_virployee_id)
		WHERE status IN ('open','needs_human') DO NOTHING
	`, id, orgID, productSurface, assistType, subjectID, entrypoint)
	if err != nil {
		return AssistCase{}, err
	}
	return r.scanAssistCase(r.pool.QueryRow(ctx, assistCaseSelect+`
		WHERE org_id=$1 AND product_surface=$2 AND assist_type=$3 AND subject_id=$4
		  AND entrypoint_virployee_id=$5 AND status IN ('open','needs_human')
	`, orgID, productSurface, assistType, subjectID, entrypoint))
}

func (r *Repository) GetAssistCase(ctx context.Context, orgID string, id uuid.UUID) (AssistCase, error) {
	return r.scanAssistCase(r.pool.QueryRow(ctx, assistCaseSelect+` WHERE org_id=$1 AND id=$2`, orgID, id))
}

func (r *Repository) ListAssistCases(ctx context.Context, orgID, status string, limit int) ([]AssistCase, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, assistCaseSelect+`
		WHERE org_id=$1 AND ($2='' OR status=$2)
		ORDER BY updated_at DESC,id LIMIT $3`, orgID, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AssistCase, 0)
	for rows.Next() {
		item, err := r.scanAssistCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

const assistCaseSelect = `SELECT id,org_id,product_surface,assist_type,subject_id,
	entrypoint_virployee_id,owner_virployee_id,status,version,created_at,updated_at,closed_at
	FROM companion_assist_cases`

func (r *Repository) scanAssistCase(row rowScanner) (AssistCase, error) {
	var out AssistCase
	if err := row.Scan(&out.ID, &out.OrgID, &out.ProductSurface, &out.AssistType, &out.SubjectID,
		&out.EntrypointVirployeeID, &out.OwnerVirployeeID, &out.Status, &out.Version, &out.CreatedAt, &out.UpdatedAt, &out.ClosedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AssistCase{}, domainerr.NotFound("assist case not found")
		}
		return AssistCase{}, err
	}
	return out, nil
}

func (r *Repository) FindOrchestrationPolicy(ctx context.Context, orgID, productSurface, assistType string, entrypoint uuid.UUID) (OrchestrationPolicy, error) {
	return r.scanOrchestrationPolicy(r.pool.QueryRow(ctx, orchestrationPolicySelect+`
		WHERE org_id=$1 AND product_surface=$2 AND assist_type=$3 AND entrypoint_virployee_id=$4
	`, orgID, productSurface, assistType, entrypoint))
}

func (r *Repository) ListOrchestrationPolicies(ctx context.Context, orgID string) ([]OrchestrationPolicy, error) {
	rows, err := r.pool.Query(ctx, orchestrationPolicySelect+` WHERE org_id=$1 ORDER BY product_surface,assist_type`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OrchestrationPolicy, 0)
	for rows.Next() {
		item, err := r.scanOrchestrationPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertOrchestrationPolicy(ctx context.Context, orgID string, in OrchestrationPolicy) (OrchestrationPolicy, error) {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if in.MaxSpecialists == 0 {
		in.MaxSpecialists = 3
	}
	if in.MaxDepth == 0 {
		in.MaxDepth = 1
	}
	if in.ConsultationTimeoutSeconds == 0 {
		in.ConsultationTimeoutSeconds = 120
	}
	if in.OrchestrationTimeoutSeconds == 0 {
		in.OrchestrationTimeoutSeconds = 300
	}
	if in.Mode == "" {
		in.Mode = OrchestrationModeDisabled
	}
	schema, err := json.Marshal(in.OutputSchema)
	if err != nil {
		return OrchestrationPolicy{}, domainerr.Validation("output_schema must be valid JSON")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO companion_orchestration_policies (
			id,org_id,product_surface,assist_type,entrypoint_virployee_id,mode,
			selector_capability_id,synthesis_capability_id,output_schema,max_specialists,max_depth,
			consultation_timeout_seconds,orchestration_timeout_seconds
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13)
		ON CONFLICT (org_id,product_surface,assist_type,entrypoint_virployee_id) DO UPDATE SET
			mode=EXCLUDED.mode,selector_capability_id=EXCLUDED.selector_capability_id,
			synthesis_capability_id=EXCLUDED.synthesis_capability_id,output_schema=EXCLUDED.output_schema,
			max_specialists=EXCLUDED.max_specialists,max_depth=EXCLUDED.max_depth,
			consultation_timeout_seconds=EXCLUDED.consultation_timeout_seconds,
			orchestration_timeout_seconds=EXCLUDED.orchestration_timeout_seconds,
			version=companion_orchestration_policies.version+1,updated_at=now()
		RETURNING id,org_id,product_surface,assist_type,entrypoint_virployee_id,mode,
			selector_capability_id,synthesis_capability_id,output_schema,max_specialists,max_depth,
			consultation_timeout_seconds,orchestration_timeout_seconds,version,created_at,updated_at
	`, in.ID, orgID, strings.TrimSpace(in.ProductSurface), strings.TrimSpace(in.AssistType), in.EntrypointVirployeeID, in.Mode,
		in.SelectorCapabilityID, in.SynthesisCapabilityID, schema, in.MaxSpecialists, in.MaxDepth, in.ConsultationTimeoutSeconds, in.OrchestrationTimeoutSeconds)
	return r.scanOrchestrationPolicy(row)
}

const orchestrationPolicySelect = `SELECT id,org_id,product_surface,assist_type,entrypoint_virployee_id,mode,
	selector_capability_id,synthesis_capability_id,output_schema,max_specialists,max_depth,
	consultation_timeout_seconds,orchestration_timeout_seconds,version,created_at,updated_at
	FROM companion_orchestration_policies`

func (r *Repository) scanOrchestrationPolicy(row rowScanner) (OrchestrationPolicy, error) {
	var out OrchestrationPolicy
	var schema []byte
	if err := row.Scan(&out.ID, &out.OrgID, &out.ProductSurface, &out.AssistType, &out.EntrypointVirployeeID, &out.Mode,
		&out.SelectorCapabilityID, &out.SynthesisCapabilityID, &schema, &out.MaxSpecialists, &out.MaxDepth,
		&out.ConsultationTimeoutSeconds, &out.OrchestrationTimeoutSeconds, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OrchestrationPolicy{}, domainerr.NotFound("orchestration policy not found")
		}
		return OrchestrationPolicy{}, err
	}
	if len(schema) > 0 {
		_ = json.Unmarshal(schema, &out.OutputSchema)
	}
	return out, nil
}

func (r *Repository) ListSpecialistRoutes(ctx context.Context, orgID, productSurface, assistType string, entrypoint uuid.UUID, enabledOnly bool) ([]SpecialistRoute, error) {
	rows, err := r.pool.Query(ctx, specialistRouteSelect+`
		WHERE org_id=$1 AND ($2='' OR product_surface=$2) AND ($3='' OR assist_type=$3)
		  AND ($4::uuid IS NULL OR entrypoint_virployee_id=$4) AND (NOT $5 OR enabled)
		ORDER BY specialty_code`, orgID, strings.TrimSpace(productSurface), strings.TrimSpace(assistType), coordinationNullableUUID(entrypoint), enabledOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SpecialistRoute, 0)
	for rows.Next() {
		item, err := r.scanSpecialistRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertSpecialistRoute(ctx context.Context, orgID string, in SpecialistRoute) (SpecialistRoute, error) {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if in.RequirementMode == "" {
		in.RequirementMode = "selector_allowed"
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO companion_specialist_routes (
			id,org_id,product_surface,assist_type,entrypoint_virployee_id,specialty_code,
			target_virployee_id,capability_id,requirement_mode,enabled
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (org_id,product_surface,assist_type,entrypoint_virployee_id,specialty_code) DO UPDATE SET
			target_virployee_id=EXCLUDED.target_virployee_id,capability_id=EXCLUDED.capability_id,
			requirement_mode=EXCLUDED.requirement_mode,enabled=EXCLUDED.enabled,
			version=companion_specialist_routes.version+1,updated_at=now()
		RETURNING id,org_id,product_surface,assist_type,entrypoint_virployee_id,specialty_code,
			target_virployee_id,capability_id,requirement_mode,enabled,version,created_at,updated_at
	`, in.ID, orgID, strings.TrimSpace(in.ProductSurface), strings.TrimSpace(in.AssistType), in.EntrypointVirployeeID,
		strings.ToLower(strings.TrimSpace(in.SpecialtyCode)), in.TargetVirployeeID, in.CapabilityID, in.RequirementMode, in.Enabled)
	return r.scanSpecialistRoute(row)
}

const specialistRouteSelect = `SELECT id,org_id,product_surface,assist_type,entrypoint_virployee_id,specialty_code,
	target_virployee_id,capability_id,requirement_mode,enabled,version,created_at,updated_at
	FROM companion_specialist_routes`

func (r *Repository) scanSpecialistRoute(row rowScanner) (SpecialistRoute, error) {
	var out SpecialistRoute
	if err := row.Scan(&out.ID, &out.OrgID, &out.ProductSurface, &out.AssistType, &out.EntrypointVirployeeID, &out.SpecialtyCode,
		&out.TargetVirployeeID, &out.CapabilityID, &out.RequirementMode, &out.Enabled, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SpecialistRoute{}, domainerr.NotFound("specialist route not found")
		}
		return SpecialistRoute{}, err
	}
	return out, nil
}

func coordinationNullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func (r *Repository) CreateOrchestrationPlan(ctx context.Context, run AssistRun, policy OrchestrationPolicy, decision OrchestrationDecision, proposal json.RawMessage, planHash, model, promptVersion string, consultations []SpecialistConsultation) (OrchestrationPlan, []SpecialistConsultation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return OrchestrationPlan{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	planID := uuid.New()
	deadline := time.Now().UTC().Add(time.Duration(policy.OrchestrationTimeoutSeconds) * time.Second)
	status := "planned"
	if decision.Decision == "consult" {
		status = "consulting"
	}
	if decision.Decision == "needs_human" {
		status = "needs_human"
	}
	var insertedPlanID uuid.UUID
	schema, marshalErr := json.Marshal(policy.OutputSchema)
	if marshalErr != nil {
		return OrchestrationPlan{}, nil, marshalErr
	}
	err = tx.QueryRow(ctx, `INSERT INTO companion_orchestration_plans (
		id,org_id,case_id,root_run_id,policy_id,policy_version,output_schema,responsible_virployee_id,
		decision,status,proposal,plan_hash,model,prompt_version,requested_count,deadline_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11::jsonb,$12,$13,$14,$15,$16)
	ON CONFLICT (org_id,root_run_id) DO NOTHING RETURNING id`, planID, run.OrgID, run.CaseID, run.ID, policy.ID, policy.Version,
		schema, run.ResponsibleVirployeeID, decision.Decision, status, []byte(proposal), planHash, model, promptVersion, len(consultations), deadline).Scan(&insertedPlanID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := r.scanOrchestrationPlan(tx.QueryRow(ctx, orchestrationPlanSelect+` WHERE org_id=$1 AND root_run_id=$2`, run.OrgID, run.ID))
		if getErr != nil {
			return OrchestrationPlan{}, nil, getErr
		}
		if existing.PlanHash != planHash {
			return OrchestrationPlan{}, nil, domainerr.Conflict("assist run already has a different orchestration plan")
		}
		if err = tx.Commit(ctx); err != nil {
			return OrchestrationPlan{}, nil, err
		}
		persisted, listErr := r.ListConsultations(ctx, run.OrgID, existing.ID)
		return existing, persisted, listErr
	}
	if err != nil {
		return OrchestrationPlan{}, nil, err
	}
	for i := range consultations {
		c := &consultations[i]
		if c.ID == uuid.Nil {
			c.ID = uuid.New()
		}
		c.PlanID = planID
		c.RootRunID = run.ID
		c.CaseID = run.CaseID
		c.OrgID = run.OrgID
		_, err = tx.Exec(ctx, `INSERT INTO companion_specialist_consultations (
			id,org_id,plan_id,root_run_id,case_id,specialty_code,target_virployee_id,capability_id,requirement,focus_json,focus_hash
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11)
		ON CONFLICT (org_id,plan_id,specialty_code) DO NOTHING`, c.ID, c.OrgID, c.PlanID, c.RootRunID, c.CaseID, c.SpecialtyCode,
			c.TargetVirployeeID, c.CapabilityID, c.Requirement, []byte(c.FocusJSON), c.FocusHash)
		if err != nil {
			return OrchestrationPlan{}, nil, err
		}
		payload, marshalErr := json.Marshal(map[string]string{"consultation_id": c.ID.String(), "plan_id": planID.String()})
		if marshalErr != nil {
			return OrchestrationPlan{}, nil, marshalErr
		}
		consultDeadline := time.Now().UTC().Add(time.Duration(policy.ConsultationTimeoutSeconds) * time.Second)
		if consultDeadline.After(deadline) {
			consultDeadline = deadline
		}
		if _, _, err = r.jobs.EnqueueTx(ctx, tx, jobs.EnqueueInput{
			OrgID: run.OrgID, ProductSurface: "companion", Kind: JobKindSpecialistConsult,
			ShardKey: run.ID.String(), DedupeKey: c.ID.String(), Payload: payload,
			MaxAttempts: 3, Timeout: time.Duration(policy.ConsultationTimeoutSeconds) * time.Second,
			DeadlineAt: &consultDeadline,
		}); err != nil {
			return OrchestrationPlan{}, nil, err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE companion_assist_runs SET orchestration_plan_id=$3,orchestration_deadline_at=$4,
		status=CASE WHEN $5='consult' THEN 'consulting' WHEN $5='needs_human' THEN 'needs_human' ELSE status END,updated_at=now()
		WHERE org_id=$1 AND id=$2 AND ownership_version=$6`, run.OrgID, run.ID, planID, deadline, decision.Decision, run.OwnershipVersion)
	if err != nil {
		return OrchestrationPlan{}, nil, err
	}
	if tag.RowsAffected() != 1 {
		return OrchestrationPlan{}, nil, domainerr.Conflict("assist ownership changed while the orchestration plan was being produced")
	}
	if err = tx.Commit(ctx); err != nil {
		return OrchestrationPlan{}, nil, err
	}
	plan, err := r.GetOrchestrationPlan(ctx, run.OrgID, planID)
	if err != nil {
		return OrchestrationPlan{}, nil, err
	}
	persisted, err := r.ListConsultations(ctx, run.OrgID, planID)
	return plan, persisted, err
}

func (r *Repository) GetOrchestrationPlan(ctx context.Context, orgID string, id uuid.UUID) (OrchestrationPlan, error) {
	return r.scanOrchestrationPlan(r.pool.QueryRow(ctx, orchestrationPlanSelect+` WHERE org_id=$1 AND id=$2`, orgID, id))
}
func (r *Repository) GetOrchestrationPlanByRun(ctx context.Context, orgID string, runID uuid.UUID) (OrchestrationPlan, error) {
	return r.scanOrchestrationPlan(r.pool.QueryRow(ctx, orchestrationPlanSelect+` WHERE org_id=$1 AND root_run_id=$2`, orgID, runID))
}

func (r *Repository) ListRecoverableOrchestrationPlans(ctx context.Context, limit int) ([]OrchestrationPlan, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, orchestrationPlanSelect+`
		WHERE status IN ('consulting','ready','synthesizing')
		ORDER BY updated_at,id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OrchestrationPlan, 0)
	for rows.Next() {
		item, scanErr := r.scanOrchestrationPlan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ClaimSynthesis(ctx context.Context, orgID string, planID uuid.UUID) (OrchestrationPlan, bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE companion_orchestration_plans
		SET status='synthesizing',updated_at=now()
		WHERE org_id=$1 AND id=$2 AND status='ready'`, orgID, planID)
	if err != nil {
		return OrchestrationPlan{}, false, err
	}
	plan, err := r.GetOrchestrationPlan(ctx, orgID, planID)
	return plan, tag.RowsAffected() == 1, err
}

const orchestrationPlanSelect = `SELECT id,org_id,case_id,root_run_id,policy_id,policy_version,output_schema,responsible_virployee_id,
	decision,status,proposal,plan_hash,model,prompt_version,requested_count,completed_count,failed_count,
	deadline_at,created_at,updated_at,completed_at FROM companion_orchestration_plans`

func (r *Repository) scanOrchestrationPlan(row rowScanner) (OrchestrationPlan, error) {
	var out OrchestrationPlan
	var proposal []byte
	if err := row.Scan(&out.ID, &out.OrgID, &out.CaseID, &out.RootRunID, &out.PolicyID, &out.PolicyVersion, &out.OutputSchema, &out.ResponsibleVirployeeID,
		&out.Decision, &out.Status, &proposal, &out.PlanHash, &out.Model, &out.PromptVersion, &out.RequestedCount, &out.CompletedCount, &out.FailedCount,
		&out.DeadlineAt, &out.CreatedAt, &out.UpdatedAt, &out.CompletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OrchestrationPlan{}, domainerr.NotFound("orchestration plan not found")
		}
		return OrchestrationPlan{}, err
	}
	out.Proposal = proposal
	return out, nil
}

func (r *Repository) ClaimConsultation(ctx context.Context, orgID string, id uuid.UUID) (SpecialistConsultation, bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE companion_specialist_consultations SET status='running',started_at=COALESCE(started_at,now()),updated_at=now()
		WHERE org_id=$1 AND id=$2 AND status='queued'`, orgID, id)
	if err != nil {
		return SpecialistConsultation{}, false, err
	}
	item, err := r.GetConsultation(ctx, orgID, id)
	return item, tag.RowsAffected() == 1, err
}
func (r *Repository) GetConsultation(ctx context.Context, orgID string, id uuid.UUID) (SpecialistConsultation, error) {
	return r.scanConsultation(r.pool.QueryRow(ctx, consultationSelect+` WHERE org_id=$1 AND id=$2`, orgID, id))
}
func (r *Repository) ListConsultations(ctx context.Context, orgID string, planID uuid.UUID) ([]SpecialistConsultation, error) {
	rows, err := r.pool.Query(ctx, consultationSelect+` WHERE org_id=$1 AND plan_id=$2 ORDER BY specialty_code`, orgID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SpecialistConsultation, 0)
	for rows.Next() {
		item, err := r.scanConsultation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

const consultationSelect = `SELECT id,org_id,plan_id,root_run_id,case_id,specialty_code,target_virployee_id,capability_id,
	requirement,status,focus_json,focus_hash,output,output_hash,model,prompt_version,error_code,duration_ms,
	started_at,completed_at,created_at,updated_at FROM companion_specialist_consultations`

func (r *Repository) scanConsultation(row rowScanner) (SpecialistConsultation, error) {
	var out SpecialistConsultation
	var focus, output []byte
	if err := row.Scan(&out.ID, &out.OrgID, &out.PlanID, &out.RootRunID, &out.CaseID, &out.SpecialtyCode, &out.TargetVirployeeID, &out.CapabilityID, &out.Requirement, &out.Status, &focus, &out.FocusHash, &output, &out.OutputHash, &out.Model, &out.PromptVersion, &out.ErrorCode, &out.DurationMS, &out.StartedAt, &out.CompletedAt, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SpecialistConsultation{}, domainerr.NotFound("specialist consultation not found")
		}
		return SpecialistConsultation{}, err
	}
	out.FocusJSON = focus
	out.Output = output
	return out, nil
}
func (r *Repository) ReleaseConsultation(ctx context.Context, orgID string, id uuid.UUID, errorCode string) error {
	_, err := r.pool.Exec(ctx, `UPDATE companion_specialist_consultations SET status='queued',error_code=$3,updated_at=now() WHERE org_id=$1 AND id=$2 AND status='running'`, orgID, id, errorCode)
	return err
}
func (r *Repository) CompleteConsultation(ctx context.Context, orgID string, id uuid.UUID, status string, output json.RawMessage, outputHash, model, promptVersion, errorCode string, durationMS int64) (SpecialistConsultation, error) {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx, `UPDATE companion_specialist_consultations SET status=$3,output=$4::jsonb,output_hash=$5,model=$6,prompt_version=$7,error_code=$8,duration_ms=$9,completed_at=now(),updated_at=now() WHERE org_id=$1 AND id=$2 AND status IN ('queued','running')`, orgID, id, status, []byte(output), outputHash, model, promptVersion, errorCode, durationMS)
	if err != nil {
		return SpecialistConsultation{}, err
	}
	return r.GetConsultation(ctx, orgID, id)
}

func (r *Repository) RefreshPlanCounts(ctx context.Context, orgID string, planID uuid.UUID) (OrchestrationPlan, error) {
	_, err := r.pool.Exec(ctx, `UPDATE companion_orchestration_plans p SET completed_count=s.completed,failed_count=s.failed,updated_at=now() FROM (SELECT plan_id,count(*) FILTER(WHERE status='completed')::int completed,count(*) FILTER(WHERE status IN ('failed','cancelled','timed_out'))::int failed FROM companion_specialist_consultations WHERE org_id=$1 AND plan_id=$2 GROUP BY plan_id)s WHERE p.org_id=$1 AND p.id=s.plan_id`, orgID, planID)
	if err != nil {
		return OrchestrationPlan{}, err
	}
	return r.GetOrchestrationPlan(ctx, orgID, planID)
}
func (r *Repository) SetPlanStatus(ctx context.Context, orgID string, id uuid.UUID, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE companion_orchestration_plans SET status=$3,completed_at=CASE WHEN $3 IN ('completed','failed','needs_human') THEN now() ELSE completed_at END,updated_at=now() WHERE org_id=$1 AND id=$2`, orgID, id, status)
	return err
}
func (r *Repository) TimeoutConsultations(ctx context.Context, orgID string, planID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE companion_specialist_consultations SET status='timed_out',error_code='consultation_timeout',completed_at=now(),updated_at=now() WHERE org_id=$1 AND plan_id=$2 AND status IN ('queued','running')`, orgID, planID)
	return err
}

func (r *Repository) CreateHumanReview(ctx context.Context, orgID string, caseID, runID uuid.UUID, reasonCode, urgency string) (HumanReview, error) {
	id := uuid.New()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return HumanReview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var insertedID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO companion_human_reviews(id,org_id,case_id,root_run_id,reason_code,urgency)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(org_id,root_run_id) DO NOTHING RETURNING id`, id, orgID, caseID, runID, reasonCode, urgency).Scan(&insertedID)
	inserted := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return HumanReview{}, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if inserted {
		_, err = tx.Exec(ctx, `UPDATE companion_assist_cases SET status='needs_human',version=version+1,updated_at=now() WHERE org_id=$1 AND id=$2`, orgID, caseID)
	}
	if err != nil {
		return HumanReview{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return HumanReview{}, err
	}
	return r.scanHumanReview(r.pool.QueryRow(ctx, humanReviewSelect+` WHERE org_id=$1 AND root_run_id=$2`, orgID, runID))
}
func (r *Repository) ListHumanReviews(ctx context.Context, orgID, status string) ([]HumanReview, error) {
	rows, err := r.pool.Query(ctx, humanReviewSelect+` WHERE org_id=$1 AND ($2='' OR status=$2) ORDER BY CASE urgency WHEN 'urgent' THEN 0 ELSE 1 END,created_at`, orgID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]HumanReview, 0)
	for rows.Next() {
		item, err := r.scanHumanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (r *Repository) ClaimHumanReview(ctx context.Context, orgID string, id uuid.UUID, actorID string) (HumanReview, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE companion_human_reviews SET status='claimed',reviewer_user_id=$3,claimed_at=now(),updated_at=now() WHERE org_id=$1 AND id=$2 AND status='pending'`, orgID, id, actorID)
	if err != nil {
		return HumanReview{}, err
	}
	if tag.RowsAffected() != 1 {
		return HumanReview{}, domainerr.Conflict("human review is not pending")
	}
	return r.GetHumanReview(ctx, orgID, id)
}
func (r *Repository) ResolveHumanReview(ctx context.Context, orgID string, id uuid.UUID, actorID string, in ResolveReviewInput) (HumanReview, error) {
	noteHash := hashOptional(in.Note)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return HumanReview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var caseID uuid.UUID
	tag, err := tx.Exec(ctx, `UPDATE companion_human_reviews SET status='resolved',outcome=$4,note=$5,note_hash=$6,handoff_id=$7,resolved_at=now(),updated_at=now() WHERE org_id=$1 AND id=$2 AND status IN ('pending','claimed') AND (reviewer_user_id='' OR reviewer_user_id=$3)`, orgID, id, actorID, in.Outcome, strings.TrimSpace(in.Note), noteHash, nullableUUIDPtr(in.HandoffID))
	if err != nil {
		return HumanReview{}, err
	}
	if tag.RowsAffected() != 1 {
		return HumanReview{}, domainerr.Conflict("human review cannot be resolved by this actor")
	}
	if err = tx.QueryRow(ctx, `SELECT case_id FROM companion_human_reviews WHERE org_id=$1 AND id=$2`, orgID, id).Scan(&caseID); err != nil {
		return HumanReview{}, err
	}
	if in.Outcome != "handoff_requested" {
		_, err = tx.Exec(ctx, `UPDATE companion_assist_cases SET status='open',version=version+1,updated_at=now() WHERE org_id=$1 AND id=$2`, orgID, caseID)
		if err != nil {
			return HumanReview{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return HumanReview{}, err
	}
	return r.GetHumanReview(ctx, orgID, id)
}
func (r *Repository) GetHumanReview(ctx context.Context, orgID string, id uuid.UUID) (HumanReview, error) {
	return r.scanHumanReview(r.pool.QueryRow(ctx, humanReviewSelect+` WHERE org_id=$1 AND id=$2`, orgID, id))
}

const humanReviewSelect = `SELECT id,org_id,case_id,root_run_id,handoff_id,reason_code,urgency,status,reviewer_user_id,outcome,note,note_hash,created_at,claimed_at,resolved_at,updated_at FROM companion_human_reviews`

func (r *Repository) scanHumanReview(row rowScanner) (HumanReview, error) {
	var out HumanReview
	if err := row.Scan(&out.ID, &out.OrgID, &out.CaseID, &out.RootRunID, &out.HandoffID, &out.ReasonCode, &out.Urgency, &out.Status, &out.ReviewerUserID, &out.Outcome, &out.Note, &out.NoteHash, &out.CreatedAt, &out.ClaimedAt, &out.ResolvedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return HumanReview{}, domainerr.NotFound("human review not found")
		}
		return HumanReview{}, err
	}
	return out, nil
}

func nullableUUIDPtr(id *uuid.UUID) any {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	return *id
}
func hashOptional(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return runtraces.HashString(value)
}
