package virployees

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/attestation"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCompleteExecutionAtomicallyCreatesImmutableOutboxMessage(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_OUTBOX_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_OUTBOX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewRepository(pool)
	signer, err := attestation.NewSigner(attestation.DeriveDevelopmentKey("integration"), "test-executor")
	if err != nil {
		t.Fatal(err)
	}
	repository.SetExecutionAttestor(signer)
	tenantID := "execution-outbox-test-" + uuid.NewString()
	jobRoleID, profileID, virployeeID := uuid.New(), uuid.New(), uuid.New()
	preparedID, executionID := uuid.New(), uuid.New()
	governanceCheckID, approvalID := uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM companion_nexus_outbox WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM virployees WHERE tenant_id=$1 AND id=$2`, tenantID, virployeeID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM job_roles WHERE tenant_id=$1 AND id=$2`, tenantID, jobRoleID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM profile_templates WHERE tenant_id=$1 AND id=$2`, tenantID, profileID)
	})
	now := time.Now().UTC()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO job_roles (id,tenant_id,name,slug,mission,created_at,updated_at) VALUES ($1,$2,'Test role',$3,'', $4,$4)`, []any{jobRoleID, tenantID, "test-" + jobRoleID.String(), now}},
		{`INSERT INTO profile_templates (id,tenant_id,name,description,system_prompt,max_autonomy,created_at,updated_at) VALUES ($1,$2,'Test profile','','test','A2',$3,$3)`, []any{profileID, tenantID, now}},
		{`INSERT INTO virployees (id,tenant_id,name,job_role_id,profile_template_id,description,supervisor_user_id,autonomy,created_at,updated_at) VALUES ($1,$2,'Test virployee',$3,$4,'','test-supervisor','A2',$5,$5)`, []any{virployeeID, tenantID, jobRoleID, profileID, now}},
		{`INSERT INTO companion_prepared_actions (id,tenant_id,virployee_id,governance_check_id,approval_id,capability_key,action,payload,payload_hash,binding_hash,created_at) VALUES ($1,$2,$3,$4,$5,'calendar.events.create','create','{}','sha256:payload','sha256:binding',$6)`, []any{preparedID, tenantID, virployeeID, governanceCheckID, approvalID, now}},
		{`INSERT INTO companion_execution_attempts (id,tenant_id,virployee_id,prepared_action_id,idempotency_key,status,nexus_report_status,started_at,updated_at) VALUES ($1,$2,$3,$4,$5,'running','pending',$6,$6)`, []any{executionID, tenantID, virployeeID, preparedID, "idem-" + executionID.String(), now}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	result := map[string]any{"external_effects": false, "resource_id": "resource-test"}
	attempt, err := repository.CompleteExecution(ctx, tenantID, executionID, "succeeded", "resource-test", result, "", 42)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.NexusReportStatus != "pending" {
		t.Fatalf("expected pending compatibility projection, got %+v", attempt)
	}
	var payloadRaw []byte
	var status string
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT min(payload_json::text)::text, min(status), count(*)
		FROM companion_nexus_outbox
		WHERE tenant_id=$1 AND aggregate_id=$2
	`, tenantID, executionID).Scan(&payloadRaw, &status, &count); err != nil {
		t.Fatal(err)
	}
	if count != 1 || status != "pending" {
		t.Fatalf("outbox count=%d status=%s", count, status)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["binding_hash"] != "sha256:binding" || payload["governance_check_id"] != governanceCheckID.String() {
		t.Fatalf("unexpected immutable delivery payload: %+v", payload)
	}
	if payload["attestation_version"] != attestation.Version || payload["executor_version"] != "test-executor" || payload["attestation"] == "" {
		t.Fatalf("missing signed attestation: %+v", payload)
	}

	if _, err := pool.Exec(ctx, `UPDATE companion_nexus_outbox SET status='delivered', delivered_at=now() WHERE tenant_id=$1 AND aggregate_id=$2`, tenantID, executionID); err != nil {
		t.Fatal(err)
	}
	attempt, err = repository.CompleteExecution(ctx, tenantID, executionID, "succeeded", "resource-test", result, "", 42)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.NexusReportStatus != "reported" {
		t.Fatalf("idempotent completion regressed projection: %+v", attempt)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM companion_nexus_outbox WHERE tenant_id=$1 AND aggregate_id=$2`, tenantID, executionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("idempotent completion duplicated outbox message: %d", count)
	}
}
