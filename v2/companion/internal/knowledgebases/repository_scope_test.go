package knowledgebases

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestPrivateDocumentScopeHasNoLegacyWorkSubjectBypass(t *testing.T) {
	scope := ArtifactScope{
		VirployeeID: uuid.New(), ProductSurface: KnowledgeProductSurface, SubjectID: uuid.NewString(),
		RepositoryGeneration: "generation", DocumentID: "document",
	}
	missingAccess := &scriptedRowQuerier{rows: []scriptedRow{{values: []any{true}}, {values: []any{false}}}}
	err := validateDocumentScope(context.Background(), missingAccess, "tenant-a", uuid.New(), ClassificationPrivate, scope)
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected inaccessible work subject to be rejected, got %v", err)
	}
	if len(missingAccess.queries) != 2 || !strings.Contains(missingAccess.queries[1], "companion_work_subjects") ||
		!strings.Contains(missingAccess.queries[1], "companion_continuity_assignments") ||
		!strings.Contains(missingAccess.queries[1], "companion_virployee_relationships") {
		t.Fatalf("private scope did not enforce exact subject access: %#v", missingAccess.queries)
	}

	invalidSubject := scope
	invalidSubject.SubjectID = "legacy-free-form-subject"
	invalid := &scriptedRowQuerier{rows: []scriptedRow{{values: []any{true}}}}
	if err := validateDocumentScope(context.Background(), invalid, "tenant-a", uuid.New(), ClassificationPrivate, invalidSubject); !domainerr.IsValidation(err) {
		t.Fatalf("expected non-work-subject identifier to be rejected, got %v", err)
	}
}

func TestPrivateDocumentScopeMatchesBoundVirployee(t *testing.T) {
	scope := ArtifactScope{
		VirployeeID: uuid.New(), ProductSurface: KnowledgeProductSurface, SubjectID: uuid.NewString(),
		RepositoryGeneration: "generation", DocumentID: "document",
	}
	valid := &scriptedRowQuerier{rows: []scriptedRow{{values: []any{true}}, {values: []any{true}}, {values: []any{0, false}}}}
	if err := validateDocumentScope(context.Background(), valid, "tenant-a", uuid.New(), ClassificationPrivate, scope); err != nil {
		t.Fatalf("expected exact accessible scope, got %v", err)
	}
	incompatible := &scriptedRowQuerier{rows: []scriptedRow{{values: []any{true}}, {values: []any{true}}, {values: []any{1, true}}}}
	if err := validateDocumentScope(context.Background(), incompatible, "tenant-a", uuid.New(), ClassificationPrivate, scope); !domainerr.IsConflict(err) {
		t.Fatalf("expected another Virployee binding to conflict, got %v", err)
	}
	if len(incompatible.queries) != 3 || !strings.Contains(incompatible.queries[2], "virployee_id<>$4") {
		t.Fatalf("binding comparison omitted exact Virployee: %#v", incompatible.queries)
	}
}

func TestSubjectAndCaseBindingsRequireExactAccessibleResources(t *testing.T) {
	virployeeID := uuid.New()
	subjectID := uuid.NewString()
	subjectQuery := &scriptedRowQuerier{rows: []scriptedRow{{values: []any{false}}}}
	err := validateBindingReference(context.Background(), subjectQuery, "tenant-a", Binding{
		ScopeType: ScopeSubject, VirployeeID: &virployeeID, SubjectID: subjectID,
	})
	if !domainerr.IsValidation(err) || len(subjectQuery.queries) != 1 ||
		!strings.Contains(subjectQuery.queries[0], "companion_work_subjects") ||
		!strings.Contains(subjectQuery.queries[0], "a.virployee_id=v.id") {
		t.Fatalf("subject binding was not fail-closed for exact access: err=%v queries=%#v", err, subjectQuery.queries)
	}

	caseID := uuid.New()
	caseQuery := &scriptedRowQuerier{rows: []scriptedRow{{values: []any{true}}}}
	if err := validateBindingReference(context.Background(), caseQuery, "tenant-a", Binding{
		ScopeType: ScopeCase, VirployeeID: &virployeeID, SubjectID: subjectID, CaseID: &caseID,
	}); err != nil {
		t.Fatalf("expected accessible exact case, got %v", err)
	}
	if len(caseQuery.queries) != 1 || !strings.Contains(caseQuery.queries[0], "companion_work_subjects") ||
		!strings.Contains(caseQuery.queries[0], "owner_virployee_id=v.id") ||
		strings.Contains(caseQuery.queries[0], "entrypoint_virployee_id=v.id") {
		t.Fatalf("case binding omitted subject/Virployee access checks: %#v", caseQuery.queries)
	}
}

type scriptedRowQuerier struct {
	rows    []scriptedRow
	queries []string
}

func (q *scriptedRowQuerier) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	q.queries = append(q.queries, query)
	if len(q.rows) == 0 {
		return scriptedRow{err: fmt.Errorf("unexpected query: %s", query)}
	}
	row := q.rows[0]
	q.rows = q.rows[1:]
	return row
}

type scriptedRow struct {
	values []any
	err    error
}

func (r scriptedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan destinations=%d values=%d", len(dest), len(r.values))
	}
	for index, value := range r.values {
		switch target := dest[index].(type) {
		case *bool:
			*target = value.(bool)
		case *int:
			*target = value.(int)
		default:
			return fmt.Errorf("unsupported scan destination %T", dest[index])
		}
	}
	return nil
}
