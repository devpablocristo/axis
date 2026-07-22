package clinicalcapabilities

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/mcpgovernance"
	profiledomain "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/google/uuid"
)

type fakeSearch struct {
	page   knowledgebases.SearchPage
	scopes []knowledgebases.RetrievalScope
	offset int
	err    error
}

func (f *fakeSearch) Search(_ context.Context, scope knowledgebases.RetrievalScope, _ string, offset, _ int) (knowledgebases.SearchPage, error) {
	f.scopes = append(f.scopes, scope)
	f.offset = offset
	return f.page, f.err
}

type fakeRuntimeContext struct{}

func (fakeRuntimeContext) RuntimeContext(context.Context, string, uuid.UUID) (runtimecontext.Context, error) {
	return runtimecontext.Context{
		JobRole:         jobroledomain.JobRole{ID: uuid.New(), Name: "Medical Historian"},
		ProfileTemplate: profiledomain.ProfileTemplate{SystemPrompt: "Use sources only"},
	}, nil
}

type fakeAnswerer struct {
	output virployees.AnswerOutput
	calls  int
}

func (f *fakeAnswerer) Answer(context.Context, virployees.AnswerInput) (virployees.AnswerOutput, error) {
	f.calls++
	return f.output, nil
}

func testInvocation() mcpgovernance.InvocationContext {
	return mcpgovernance.InvocationContext{
		OrgID: "organization-a", VirployeeID: uuid.New(), SubjectID: uuid.New(),
		ProductSurface: "producta", RepositoryGeneration: "generation-1",
	}
}

func testCapability(key string, output map[string]any) capabilitydomain.Capability {
	return capabilitydomain.Capability{
		CapabilityKey: key, SideEffectClass: "read", ManifestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Manifest: capabilitydomain.Manifest{TimeoutMS: 30000, OutputSchema: output},
	}
}

func testMatch(score float64) knowledgebases.SearchMatch {
	return knowledgebases.SearchMatch{
		Part: artifacts.ContentPart{Kind: artifacts.PartText, Text: "clinical evidence", DocumentID: uuid.NewString()},
		Citation: knowledgebases.Citation{
			DocumentID: uuid.NewString(), SourceVersion: "source-v1",
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Locator: json.RawMessage(`{"page":1}`),
		}, Score: score,
	}
}

func TestClinicalSchemasAreClosed(t *testing.T) {
	for name, schema := range map[string]map[string]any{
		"search input": SearchInputSchema(), "search output": SearchOutputSchema(),
		"timeline input": TimelineInputSchema(), "timeline output": TimelineOutputSchema(),
	} {
		if closed, ok := schema["additionalProperties"].(bool); !ok || closed {
			t.Fatalf("%s must set additionalProperties=false", name)
		}
	}
	if err := mcpgovernance.ValidateJSONSchema(SearchInputSchema(), map[string]any{"query": "labs", "extra": true}); err == nil {
		t.Fatal("closed search schema accepted an unknown property")
	}
}

func TestSearchPreservesScoreAndBindsCursorToScope(t *testing.T) {
	match := testMatch(0.91)
	retriever := &fakeSearch{page: knowledgebases.SearchPage{Matches: []knowledgebases.SearchMatch{match}, HasMore: true}}
	executor := NewExecutor(retriever, nil, nil)
	invocation := testInvocation()
	out, err := executor.Execute(context.Background(), invocation, testCapability(RecordsSearchKey, SearchOutputSchema()), map[string]any{"query": "labs", "limit": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	matches := out["matches"].([]any)
	if matches[0].(map[string]any)["score"] != 0.91 || out["next_cursor"] == "" {
		t.Fatalf("search lost score or cursor: %#v", out)
	}
	cursor := out["next_cursor"].(string)
	invocation.SubjectID = uuid.New()
	if _, err := executor.Execute(context.Background(), invocation, testCapability(RecordsSearchKey, SearchOutputSchema()), map[string]any{"query": "labs", "cursor": cursor}); err == nil {
		t.Fatal("cursor was reusable for another subject")
	}
	if retriever.scopes[0].OrgID != "organization-a" || retriever.scopes[0].RepositoryGeneration != "generation-1" {
		t.Fatalf("retrieval scope was not exact: %+v", retriever.scopes[0])
	}
}

func TestSearchHandlesZeroResultsAndIndexFailure(t *testing.T) {
	executor := NewExecutor(&fakeSearch{}, nil, nil)
	out, err := executor.Execute(context.Background(), testInvocation(), testCapability(RecordsSearchKey, SearchOutputSchema()), map[string]any{"query": "absent"})
	if err != nil || out["status"] != "completed" || len(out["matches"].([]any)) != 0 {
		t.Fatalf("zero-result search must complete: out=%#v err=%v", out, err)
	}
	executor = NewExecutor(&fakeSearch{err: errors.New("index offline")}, nil, nil)
	if _, err := executor.Execute(context.Background(), testInvocation(), testCapability(RecordsSearchKey, SearchOutputSchema()), map[string]any{"query": "labs"}); err == nil {
		t.Fatalf("index failure was not surfaced safely: %v", err)
	}
}

func TestTimelineRepairsOnceThenAbstainsForInventedCitation(t *testing.T) {
	match := testMatch(0.8)
	retriever := &fakeSearch{page: knowledgebases.SearchPage{Matches: []knowledgebases.SearchMatch{match}}}
	invalid, _ := json.Marshal(map[string]any{
		"events": []any{map[string]any{
			"date": "2026-01-02T00:00:00Z", "date_precision": "day", "type": "visit",
			"title": "Invented", "summary": "Unsupported", "references": []any{map[string]any{
				"document_id": "invented", "source_version": "x", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "locator": map[string]any{},
			}},
		}},
	})
	answerer := &fakeAnswerer{output: virployees.AnswerOutput{OutputJSON: invalid, Answered: true}}
	executor := NewExecutor(retriever, fakeRuntimeContext{}, answerer)
	out, err := executor.Execute(context.Background(), testInvocation(), testCapability(TimelineBuildKey, TimelineOutputSchema()), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != "abstained" || answerer.calls != 2 || len(out["events"].([]any)) != 0 {
		t.Fatalf("timeline did not fail closed after one repair: %#v calls=%d", out, answerer.calls)
	}
}

func TestTimelineFiltersAndOrdersEventsAfterCitationValidation(t *testing.T) {
	match := testMatch(0.8)
	reference := referenceMap(match.Citation)
	output, _ := json.Marshal(map[string]any{"events": []any{
		map[string]any{"date": "2025-01-02", "date_precision": "day", "type": "visit", "title": "Older", "summary": "older", "references": []any{reference}},
		map[string]any{"date": "2026-03-01", "date_precision": "day", "type": "lab", "title": "Newest", "summary": "new", "references": []any{reference}},
		map[string]any{"date": "", "date_precision": "unknown", "type": "history", "title": "Undated", "summary": "unknown date", "references": []any{reference}},
	}})
	answerer := &fakeAnswerer{output: virployees.AnswerOutput{OutputJSON: output, Answered: true}}
	executor := NewExecutor(&fakeSearch{page: knowledgebases.SearchPage{Matches: []knowledgebases.SearchMatch{match}}}, fakeRuntimeContext{}, answerer)
	out, err := executor.Execute(context.Background(), testInvocation(), testCapability(TimelineBuildKey, TimelineOutputSchema()), map[string]any{
		"date_from": "2026-01-01T00:00:00Z", "order": "desc", "max_events": float64(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	events := out["events"].([]any)
	if len(events) != 2 || events[0].(map[string]any)["title"] != "Newest" || events[1].(map[string]any)["title"] != "Undated" {
		t.Fatalf("timeline was not filtered/ordered in Go: %#v", events)
	}
	coverage := out["coverage"].(map[string]any)
	if coverage["events_without_date"] != float64(1) {
		t.Fatalf("undated coverage is wrong: %#v", coverage)
	}
}
