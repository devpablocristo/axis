package jobroles

import (
	"context"
	"os"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryPersistsProfessionalDefinition(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_JOB_ROLE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_JOB_ROLE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := "job-role-definition-test-" + uuid.NewString()
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM job_roles WHERE tenant_id=$1`, tenantID) }()

	repo := NewRepository(pool)
	created, err := repo.Create(ctx, tenantID, domain.NormalizedCreateInput{
		Name:    "Clinical doctor",
		Slug:    "clinical-doctor",
		Mission: "Care for patients",
		Responsibilities: []domain.Responsibility{{
			Title: "Assess", Description: "Review the case", ExpectedOutcome: "Safe assessment", Priority: 1,
		}},
		SuccessCriteria: []domain.SuccessCriterion{{
			Title: "Grounding", Description: "Use approved sources", TargetValue: "100% cited", Priority: 1,
		}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	loaded, err := repo.Get(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(loaded.Responsibilities) != 1 || loaded.Responsibilities[0].ExpectedOutcome != "Safe assessment" ||
		len(loaded.SuccessCriteria) != 1 || loaded.SuccessCriteria[0].TargetValue != "100% cited" {
		t.Fatalf("professional definition did not round-trip: %+v", loaded)
	}

	updated, err := repo.Update(ctx, tenantID, created.ID, domain.NormalizedUpdateInput{
		Name: "Clinical doctor", Slug: "clinical-doctor", Mission: "Care safely",
		Responsibilities: []domain.Responsibility{}, SuccessCriteria: []domain.SuccessCriterion{},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Responsibilities == nil || updated.SuccessCriteria == nil || len(updated.Responsibilities) != 0 || len(updated.SuccessCriteria) != 0 {
		t.Fatalf("empty definitions must round-trip as arrays: %+v", updated)
	}
}
