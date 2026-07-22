package workforcerouting

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryConcurrentStableAssignmentCapacityAndTenantIsolation(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_WORKFORCE_ROUTING_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_WORKFORCE_ROUTING_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenantID := "routing-test-" + uuid.NewString()
	otherTenantID := tenantID + "-other"
	jobRoleID := uuid.New()
	profileID := uuid.New()
	virployeeID := uuid.New()
	seedRoutingPrincipal(t, ctx, pool, tenantID, jobRoleID, profileID, virployeeID)
	defer cleanupRoutingTenant(t, pool, tenantID)
	defer cleanupRoutingTenant(t, pool, otherTenantID)

	repo := NewRepository(pool)
	routingPool, err := repo.CreateRoutingPool(ctx, tenantID, NormalizedRoutingPoolInput{JobRoleID: jobRoleID, Name: "Clinical team"})
	if err != nil {
		t.Fatalf("CreateRoutingPool: %v", err)
	}
	if _, err := repo.CreateRoutingPool(ctx, tenantID, NormalizedRoutingPoolInput{JobRoleID: jobRoleID, Name: "Parallel clinical team"}); !domainerr.IsConflict(err) {
		t.Fatalf("same Job Role must not have parallel active pools: %v", err)
	}
	if _, err := repo.UpsertPoolMember(ctx, tenantID, routingPool.ID, virployeeID, UpsertPoolMemberInput{MaxActiveSubjects: 1, Enabled: true}); err != nil {
		t.Fatalf("UpsertPoolMember: %v", err)
	}
	subject, err := repo.CreateWorkSubject(ctx, tenantID, NormalizedWorkSubjectInput{Kind: SubjectKindPatient, DisplayName: "Patient A"})
	if err != nil {
		t.Fatalf("CreateWorkSubject: %v", err)
	}

	const contenders = 12
	start := make(chan struct{})
	results := make(chan resolveAttempt, contenders)
	var wg sync.WaitGroup
	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, resolveErr := repo.Resolve(ctx, tenantID, NormalizedResolveInput{PoolID: routingPool.ID, SubjectID: subject.ID, ActorID: "test"})
			results <- resolveAttempt{result: result, err: resolveErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	created := 0
	var assignmentID uuid.UUID
	for attempt := range results {
		if attempt.err != nil {
			t.Fatalf("concurrent Resolve: %v", attempt.err)
		}
		if attempt.result.Status != ResolveStatusAssigned || attempt.result.Assignment == nil {
			t.Fatalf("unexpected resolve result: %+v", attempt.result)
		}
		if assignmentID == uuid.Nil {
			assignmentID = attempt.result.Assignment.ID
		} else if assignmentID != attempt.result.Assignment.ID {
			t.Fatalf("stable routing produced multiple assignments: %s and %s", assignmentID, attempt.result.Assignment.ID)
		}
		if attempt.result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly one created assignment, got %d", created)
	}

	secondSubject, err := repo.CreateWorkSubject(ctx, tenantID, NormalizedWorkSubjectInput{Kind: SubjectKindPatient, DisplayName: "Patient B"})
	if err != nil {
		t.Fatal(err)
	}
	full, err := repo.Resolve(ctx, tenantID, NormalizedResolveInput{PoolID: routingPool.ID, SubjectID: secondSubject.ID, ActorID: "test"})
	if err != nil {
		t.Fatalf("Resolve full pool: %v", err)
	}
	if full.Status != ResolveStatusUnavailable || full.Assignment != nil {
		t.Fatalf("expected unavailable at capacity, got %+v", full)
	}

	if _, err := repo.Resolve(ctx, otherTenantID, NormalizedResolveInput{PoolID: routingPool.ID, SubjectID: subject.ID, ActorID: "test"}); !domainerr.IsNotFound(err) {
		t.Fatalf("expected tenant-scoped not found, got %v", err)
	}
	otherAssignments, err := repo.ListAssignments(ctx, otherTenantID, uuid.Nil, uuid.Nil)
	if err != nil {
		t.Fatalf("ListAssignments other tenant: %v", err)
	}
	if len(otherAssignments) != 0 {
		t.Fatalf("tenant isolation leaked assignments: %+v", otherAssignments)
	}

	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM companion_continuity_assignment_events
		WHERE tenant_id=$1 AND assignment_id=$2`, tenantID, assignmentID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("idempotent resolve must emit one assignment event, got %d", eventCount)
	}
	version, err := repo.ValidateAssistAssignment(ctx, tenantID, assignmentID, subject.ID, virployeeID, 0)
	if err != nil || version != 1 {
		t.Fatalf("ValidateAssistAssignment: version=%d err=%v", version, err)
	}
	if _, err := repo.ValidateAssistAssignment(ctx, tenantID, assignmentID, secondSubject.ID, virployeeID, 0); !domainerr.IsConflict(err) {
		t.Fatalf("assignment must not cross subjects: %v", err)
	}
	if required, err := repo.RequiresAssistAssignment(ctx, tenantID, uuid.Nil, virployeeID); err != nil || !required {
		t.Fatalf("active pool member must require assignment for Assist: required=%v err=%v", required, err)
	}
	otherSameRoleID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO virployees(
		id,tenant_id,name,job_role_id,profile_template_id,description,supervisor_user_id,autonomy,created_at,updated_at)
		VALUES ($1,$2,'Dr Unassigned',$3,$4,'','supervisor','A2',now(),now())`, otherSameRoleID, tenantID, jobRoleID, profileID); err != nil {
		t.Fatal(err)
	}
	if required, err := repo.RequiresAssistAssignment(ctx, tenantID, subject.ID, otherSameRoleID); err != nil || !required {
		t.Fatalf("subject assignment must prevent same-profession Virployee bypass: required=%v err=%v", required, err)
	}

	replacementVirployeeID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO virployees(
		id,tenant_id,name,job_role_id,profile_template_id,description,supervisor_user_id,autonomy,created_at,updated_at)
		VALUES ($1,$2,'Dr Replacement',$3,$4,'','supervisor','A2',now(),now())`, replacementVirployeeID, tenantID, jobRoleID, profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertPoolMember(ctx, tenantID, routingPool.ID, replacementVirployeeID, UpsertPoolMemberInput{MaxActiveSubjects: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertPoolMember(ctx, tenantID, routingPool.ID, virployeeID, UpsertPoolMemberInput{MaxActiveSubjects: 1, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if required, err := repo.RequiresAssistAssignment(ctx, tenantID, uuid.Nil, virployeeID); err != nil || !required {
		t.Fatalf("disabled pool member must not bypass continuity routing: required=%v err=%v", required, err)
	}
	requiresReassignment, err := repo.Resolve(ctx, tenantID, NormalizedResolveInput{PoolID: routingPool.ID, SubjectID: subject.ID, ActorID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if requiresReassignment.Status != ResolveStatusReassignmentRequired || requiresReassignment.Created {
		t.Fatalf("disabled assignee must not rotate silently: %+v", requiresReassignment)
	}
	reassigned, err := repo.Reassign(ctx, tenantID, assignmentID, NormalizedReassignInput{
		VirployeeID: replacementVirployeeID, ExpectedVersion: 1, Reason: "clinician_unavailable", ActorID: "owner-1",
	})
	if err != nil {
		t.Fatalf("Reassign: %v", err)
	}
	if reassigned.VirployeeID != replacementVirployeeID || reassigned.Version != 2 {
		t.Fatalf("unexpected reassignment: %+v", reassigned)
	}
	if _, err := repo.ValidateAssistAssignment(ctx, tenantID, assignmentID, subject.ID, virployeeID, 1); !domainerr.IsConflict(err) {
		t.Fatalf("old assignee must be invalid after reassignment: %v", err)
	}
	if version, err := repo.ValidateAssistAssignment(ctx, tenantID, assignmentID, subject.ID, replacementVirployeeID, 2); err != nil || version != 2 {
		t.Fatalf("replacement assignment validation: version=%d err=%v", version, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM companion_continuity_assignment_events
		WHERE tenant_id=$1 AND assignment_id=$2`, tenantID, assignmentID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 {
		t.Fatalf("reassignment audit event missing, got %d events", eventCount)
	}

	testConcurrentCapacityAcrossSubjects(t, ctx, repo, tenantID, jobRoleID, virployeeID)
}

func TestRepositoryRelationshipsAreAtomicAndTenantScoped(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_WORKFORCE_ROUTING_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_WORKFORCE_ROUTING_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := "relationship-test-" + uuid.NewString()
	otherTenantID := tenantID + "-other"
	jobRoleID, profileID, virployeeID := uuid.New(), uuid.New(), uuid.New()
	seedRoutingPrincipal(t, ctx, pool, tenantID, jobRoleID, profileID, virployeeID)
	defer cleanupRoutingTenant(t, pool, tenantID)
	defer cleanupRoutingTenant(t, pool, otherTenantID)

	repo := NewRepository(pool)
	employer, err := repo.CreateWorkSubject(ctx, tenantID, NormalizedWorkSubjectInput{Kind: SubjectKindOrganization, DisplayName: "Clinic"})
	if err != nil {
		t.Fatal(err)
	}
	patient, err := repo.CreateWorkSubject(ctx, tenantID, NormalizedWorkSubjectInput{Kind: SubjectKindPatient, DisplayName: "Patient"})
	if err != nil {
		t.Fatal(err)
	}
	otherSubject, err := repo.CreateWorkSubject(ctx, otherTenantID, NormalizedWorkSubjectInput{Kind: SubjectKindOrganization, DisplayName: "Other clinic"})
	if err != nil {
		t.Fatal(err)
	}

	items := []NormalizedRelationshipInput{
		{SubjectID: employer.ID, RelationshipType: RelationshipWorksFor, IsPrimary: true},
		{SubjectID: patient.ID, RelationshipType: RelationshipServes},
	}
	got, err := repo.ReplaceRelationships(ctx, tenantID, virployeeID, items)
	if err != nil {
		t.Fatalf("ReplaceRelationships: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two relationships, got %+v", got)
	}
	if _, err := repo.ReplaceRelationships(ctx, tenantID, virployeeID, []NormalizedRelationshipInput{
		{SubjectID: otherSubject.ID, RelationshipType: RelationshipWorksFor, IsPrimary: true},
	}); !domainerr.IsNotFound(err) {
		t.Fatalf("expected cross-tenant subject rejection, got %v", err)
	}
	unchanged, err := repo.ListRelationships(ctx, tenantID, virployeeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged) != 2 {
		t.Fatalf("failed replacement was not atomic: %+v", unchanged)
	}
}

type resolveAttempt struct {
	result ResolveResult
	err    error
}

func testConcurrentCapacityAcrossSubjects(t *testing.T, ctx context.Context, repo *Repository, tenantID string, jobRoleID, virployeeID uuid.UUID) {
	t.Helper()
	pool, err := repo.CreateRoutingPool(ctx, tenantID, NormalizedRoutingPoolInput{JobRoleID: jobRoleID, Name: "Concurrent capacity"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertPoolMember(ctx, tenantID, pool.ID, virployeeID, UpsertPoolMemberInput{MaxActiveSubjects: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	subjects := make([]WorkSubject, 2)
	for i := range subjects {
		subjects[i], err = repo.CreateWorkSubject(ctx, tenantID, NormalizedWorkSubjectInput{Kind: SubjectKindPatient, DisplayName: fmt.Sprintf("Concurrent patient %d", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan resolveAttempt, 2)
	var wg sync.WaitGroup
	for _, subject := range subjects {
		subject := subject
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, resolveErr := repo.Resolve(ctx, tenantID, NormalizedResolveInput{PoolID: pool.ID, SubjectID: subject.ID, ActorID: "test"})
			results <- resolveAttempt{result: result, err: resolveErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	assigned, unavailable := 0, 0
	for attempt := range results {
		if attempt.err != nil {
			t.Fatal(attempt.err)
		}
		switch attempt.result.Status {
		case ResolveStatusAssigned:
			assigned++
		case ResolveStatusUnavailable:
			unavailable++
		}
	}
	if assigned != 1 || unavailable != 1 {
		t.Fatalf("pool-level serialization exceeded capacity: assigned=%d unavailable=%d", assigned, unavailable)
	}
}

func seedRoutingPrincipal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, jobRoleID, profileID, virployeeID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO job_roles(
		id,tenant_id,name,slug,mission,responsibilities_json,success_criteria_json,created_at,updated_at)
		VALUES ($1,$2,'Doctor',$3,'Care for patients','[]','[]',now(),now())`, jobRoleID, tenantID, "doctor-"+jobRoleID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO profile_templates(
		id,tenant_id,name,description,system_prompt,max_autonomy,created_at,updated_at)
		VALUES ($1,$2,'Clinical','', 'Stay within scope','A2',now(),now())`, profileID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO virployees(
		id,tenant_id,name,job_role_id,profile_template_id,description,supervisor_user_id,autonomy,created_at,updated_at)
		VALUES ($1,$2,'Dr Virtual',$3,$4,'','supervisor','A2',now(),now())`, virployeeID, tenantID, jobRoleID, profileID); err != nil {
		t.Fatal(err)
	}
}

func cleanupRoutingTenant(t *testing.T, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`DELETE FROM companion_continuity_assignment_events WHERE tenant_id=$1`,
		`DELETE FROM companion_continuity_assignments WHERE tenant_id=$1`,
		`DELETE FROM companion_virployee_relationships WHERE tenant_id=$1`,
		`DELETE FROM companion_routing_pool_members WHERE tenant_id=$1`,
		`DELETE FROM companion_routing_pools WHERE tenant_id=$1`,
		`DELETE FROM companion_work_subjects WHERE tenant_id=$1`,
		`DELETE FROM virployees WHERE tenant_id=$1`,
		`DELETE FROM profile_templates WHERE tenant_id=$1`,
		`DELETE FROM job_roles WHERE tenant_id=$1`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement, tenantID); err != nil {
			t.Errorf("cleanup %q: %v", statement, err)
		}
	}
}
