package virployees

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCoordinationPlanIsAtomicAndHandoffDecisionIsSingleWinner(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_COORDINATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_COORDINATION_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	repository := NewRepository(pool)
	tenantID := "coordination-test-" + uuid.NewString()
	jobRoleID, profileID := uuid.New(), uuid.New()
	ownerID, specialistID, capabilityID := uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM companion_jobs WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM companion_human_reviews WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM companion_handoffs WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `UPDATE companion_assist_runs SET orchestration_plan_id=NULL WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM companion_orchestration_plans WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM companion_assist_runs WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM companion_orchestration_policies WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM companion_specialist_routes WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM companion_assist_cases WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM virployee_capabilities WHERE virployee_id IN ($1,$2)`, ownerID, specialistID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM virployees WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM capabilities WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM job_roles WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM profile_templates WHERE tenant_id=$1`, tenantID)
	})

	now := time.Now().UTC()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO job_roles (id,tenant_id,name,slug,mission,created_at,updated_at) VALUES ($1,$2,'Coordination role',$3,'',$4,$4)`, []any{jobRoleID, tenantID, "coordination-" + jobRoleID.String(), now}},
		{`INSERT INTO profile_templates (id,tenant_id,name,description,system_prompt,max_autonomy,created_at,updated_at) VALUES ($1,$2,'Coordination profile','','test','A2',$3,$3)`, []any{profileID, tenantID, now}},
		{`INSERT INTO virployees (id,tenant_id,name,job_role_id,profile_template_id,description,supervisor_user_id,autonomy,created_at,updated_at) VALUES ($1,$2,'Owner',$3,$4,'','supervisor-a','A2',$5,$5)`, []any{ownerID, tenantID, jobRoleID, profileID, now}},
		{`INSERT INTO virployees (id,tenant_id,name,job_role_id,profile_template_id,description,supervisor_user_id,autonomy,created_at,updated_at) VALUES ($1,$2,'Specialist',$3,$4,'','supervisor-b','A2',$5,$5)`, []any{specialistID, tenantID, jobRoleID, profileID, now}},
		{`INSERT INTO capabilities (id,tenant_id,capability_key,name,description,required_autonomy,risk_class,side_effect_class,requires_nexus_approval,evidence_required,rollback_capability_key,promotion_state,manifest,created_at,updated_at) VALUES ($1,$2,'test.specialist.consult','Test consult','','A1','low','read',false,true,'','active','{}'::jsonb,$3,$3)`, []any{capabilityID, tenantID, now}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	metadata := AssistMetadata{ProductSurface: "medmory", AssistType: "clinical_diagnosis", SubjectID: "subject-" + uuid.NewString(), RepositoryGeneration: "generation-a"}
	run, created, err := repository.BeginAssistRun(ctx, tenantID, ownerID, metadata, "idem-"+uuid.NewString(), strings.Repeat("a", 64), "", json.RawMessage(`{"documents":[]}`))
	if err != nil || !created {
		t.Fatalf("begin assist run: created=%v err=%v", created, err)
	}
	policy, err := repository.UpsertOrchestrationPolicy(ctx, tenantID, OrchestrationPolicy{
		ProductSurface: "medmory", AssistType: "clinical_diagnosis", EntrypointVirployeeID: ownerID,
		Mode: OrchestrationModeActive, SelectorCapabilityID: capabilityID, SynthesisCapabilityID: capabilityID,
		OutputSchema: map[string]any{"type": "object"}, MaxSpecialists: 3, MaxDepth: 1,
		ConsultationTimeoutSeconds: 120, OrchestrationTimeoutSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	proposal := json.RawMessage(`{"decision":"consult","consultations":[{"specialty_code":"clinical.laboratory"}]}`)
	consultations := []SpecialistConsultation{{
		SpecialtyCode: "clinical.laboratory", TargetVirployeeID: specialistID, CapabilityID: capabilityID,
		Requirement: "advisory", FocusJSON: json.RawMessage(`{"focus":"laboratory results"}`), FocusHash: strings.Repeat("c", 64),
	}}
	decision := OrchestrationDecision{Decision: "consult"}
	plan, persisted, err := repository.CreateOrchestrationPlan(ctx, run, policy, decision, proposal, strings.Repeat("b", 64), "test-model", "test-prompt", consultations)
	if err != nil {
		t.Fatal(err)
	}
	again, persistedAgain, err := repository.CreateOrchestrationPlan(ctx, run, policy, decision, proposal, strings.Repeat("b", 64), "test-model", "test-prompt", consultations)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != plan.ID || len(persisted) != 1 || len(persistedAgain) != 1 || persistedAgain[0].ID != persisted[0].ID {
		t.Fatalf("plan retry was not idempotent: first=%+v second=%+v consultations=%+v/%+v", plan, again, persisted, persistedAgain)
	}
	if plan.OutputSchema["type"] != "object" || again.OutputSchema["type"] != "object" {
		t.Fatalf("orchestration plan did not snapshot its output schema: first=%+v second=%+v", plan.OutputSchema, again.OutputSchema)
	}
	var planCount, consultationCount, jobCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM companion_orchestration_plans WHERE tenant_id=$1 AND root_run_id=$2`, tenantID, run.ID).Scan(&planCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM companion_specialist_consultations WHERE tenant_id=$1 AND plan_id=$2`, tenantID, plan.ID).Scan(&consultationCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM companion_jobs WHERE tenant_id=$1 AND kind=$2`, tenantID, JobKindSpecialistConsult).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if planCount != 1 || consultationCount != 1 || jobCount != 1 {
		t.Fatalf("plan transaction duplicated durable state: plans=%d consultations=%d jobs=%d", planCount, consultationCount, jobCount)
	}
	if _, err := pool.Exec(ctx, `UPDATE companion_specialist_consultations SET status='running' WHERE tenant_id=$1 AND id=$2`, tenantID, persisted[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, reclaimed, err := repository.ClaimConsultation(ctx, tenantID, persisted[0].ID); err != nil {
		t.Fatal(err)
	} else if reclaimed {
		t.Fatal("a running specialist consultation must not be reclaimed after an ambiguous lease loss")
	}
	if _, err := pool.Exec(ctx, `UPDATE companion_specialist_consultations SET status='queued' WHERE tenant_id=$1 AND id=$2`, tenantID, persisted[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetPlanStatus(ctx, tenantID, plan.ID, "ready"); err != nil {
		t.Fatal(err)
	}
	claimCh := make(chan bool, 2)
	var claimWG sync.WaitGroup
	for range 2 {
		claimWG.Add(1)
		go func() {
			defer claimWG.Done()
			_, claimed, claimErr := repository.ClaimSynthesis(ctx, tenantID, plan.ID)
			if claimErr != nil {
				t.Errorf("claim synthesis: %v", claimErr)
			}
			claimCh <- claimed
		}()
	}
	claimWG.Wait()
	close(claimCh)
	claimWinners := 0
	for claimed := range claimCh {
		if claimed {
			claimWinners++
		}
	}
	if claimWinners != 1 {
		t.Fatalf("exactly one concurrent synthesis claim must win, got %d", claimWinners)
	}

	handoff, err := repository.CreateHandoff(ctx, tenantID, ownerID, "supervisor-a", CreateHandoffInput{CaseID: run.CaseID, SourceRunID: &run.ID, ToID: specialistID, ReasonCode: "clinical_scope"})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for _, actorID := range []string{"supervisor-b", "owner-b"} {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			_, decideErr := repository.DecideHandoff(ctx, tenantID, handoff.ID, actor, "accept", DecideHandoffInput{Version: handoff.Version})
			errCh <- decideErr
		}(actorID)
	}
	wg.Wait()
	close(errCh)
	succeeded := 0
	for decideErr := range errCh {
		if decideErr == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("exactly one concurrent handoff decision must win, got %d", succeeded)
	}
	assistCase, err := repository.GetAssistCase(ctx, tenantID, run.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	if assistCase.OwnerVirployeeID != specialistID || assistCase.Version != 2 {
		t.Fatalf("accepted handoff did not atomically transfer ownership: %+v", assistCase)
	}
	var responsibleID uuid.UUID
	var ownershipVersion int64
	if err := pool.QueryRow(ctx, `SELECT responsible_virployee_id,ownership_version FROM companion_assist_runs WHERE tenant_id=$1 AND id=$2`, tenantID, run.ID).Scan(&responsibleID, &ownershipVersion); err != nil {
		t.Fatal(err)
	}
	if responsibleID != specialistID || ownershipVersion != 2 {
		t.Fatalf("active run ownership was not transferred atomically: responsible=%s version=%d", responsibleID, ownershipVersion)
	}
}
