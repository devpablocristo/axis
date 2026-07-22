package runtimeclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/memories"
	profiletemplatedomain "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

func TestProposeMapsResponseAndForwardsToken(t *testing.T) {
	var gotToken string
	var gotBody proposeRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Axis-Internal-Token")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(proposeResponse{
			Intent: proposedIntent{
				Matched: true, CapabilityKey: "calendar.events.create",
				Domain: "calendar", Resource: "events", Action: "create",
				RequiredAutonomy: "A2", Confidence: 0.9,
			},
			Model: "test-model",
		})
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client(), "secret-token")
	rc := runtimecontext.Context{
		ProfileTemplate: profiletemplatedomain.ProfileTemplate{SystemPrompt: "Be helpful."},
		JobRole:         jobroledomain.JobRole{Name: "Receptionist"},
		Capabilities: []capabilitydomain.Capability{
			{CapabilityKey: "calendar.events.create", Name: "Create", RequiredAutonomy: virployeedomain.AutonomyA2, RiskClass: "high"},
		},
		MemoryContext: []memories.ContextItem{{Title: "Timezone", Type: "preference", Content: "America/Argentina/Buenos_Aires"}},
	}

	proposal, err := client.Propose(context.Background(), "agendá una reunión", rc)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if gotToken != "secret-token" {
		t.Fatalf("expected internal token forwarded, got %q", gotToken)
	}
	if gotBody.SystemPrompt != "Be helpful." || len(gotBody.Capabilities) != 1 || gotBody.Capabilities[0].CapabilityKey != "calendar.events.create" {
		t.Fatalf("unexpected request body: %+v", gotBody)
	}
	if len(gotBody.Memory) != 1 || gotBody.Memory[0].Content != "America/Argentina/Buenos_Aires" {
		t.Fatalf("approved memory content was not sent to Runtime: %+v", gotBody.Memory)
	}
	if !proposal.Intent.Matched || proposal.Intent.CapabilityKey != "calendar.events.create" || proposal.Intent.Action != "create" {
		t.Fatalf("unexpected proposal intent: %+v", proposal.Intent)
	}
	if proposal.RequiredAutonomy != virployeedomain.AutonomyA2 {
		t.Fatalf("expected A2, got %q", proposal.RequiredAutonomy)
	}
}

func TestProposeErrorsOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client(), "")
	if _, err := client.Propose(context.Background(), "x", runtimecontext.Context{}); err == nil {
		t.Fatal("expected an error on a non-200 status")
	}
}

func TestEnrichMapsResponseAndForwardsToken(t *testing.T) {
	var gotToken, gotPath string
	var gotBody enrichRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Axis-Internal-Token")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(enrichResponse{
			Title: "Agendar reunión", Content: "1. Confirmar. 2. Ejecutar.",
			Enriched: true, Model: "gemini-x", PromptVersion: "enrich.v1",
		})
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client(), "secret-token")
	out, err := client.Enrich(context.Background(), EnrichRequest{
		CapabilityKey: "calendar.events.create", Title: "T", Content: "C",
	})
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if gotToken != "secret-token" || gotPath != "/v1/enrich" {
		t.Fatalf("unexpected token/path: %q %q", gotToken, gotPath)
	}
	if gotBody.CapabilityKey != "calendar.events.create" || gotBody.Title != "T" {
		t.Fatalf("unexpected request body: %+v", gotBody)
	}
	if !out.Enriched || out.Title != "Agendar reunión" || out.ModelID != "gemini-x" || out.PromptVersion != "enrich.v1" {
		t.Fatalf("unexpected enrich result: %+v", out)
	}
}

func TestEnrichErrorsOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client(), "")
	if _, err := client.Enrich(context.Background(), EnrichRequest{Title: "T", Content: "C"}); err == nil {
		t.Fatal("expected an error on a non-200 status")
	}
}

func TestAnswerMapsResponseAndForwardsToken(t *testing.T) {
	var gotToken, gotPath string
	var gotBody answerRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Axis-Internal-Token")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(answerResponse{
			OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true,
			Model: "gemini-x", PromptVersion: "answer.v1",
		})
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client(), "secret-token")
	out, err := client.Answer(context.Background(), AnswerRequest{
		SystemPrompt: "Sos un médico.", InputJSON: json.RawMessage(`{"labs":"x"}`),
		ResponseSchema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if gotToken != "secret-token" || gotPath != "/v1/answer" {
		t.Fatalf("unexpected token/path: %q %q", gotToken, gotPath)
	}
	if string(gotBody.InputJSON) != `{"labs":"x"}` || len(gotBody.ResponseSchema) == 0 {
		t.Fatalf("unexpected request body: %+v", gotBody)
	}
	if !out.Answered || string(out.OutputJSON) != `{"summary":"ok"}` || out.ModelID != "gemini-x" || out.PromptVersion != "answer.v1" {
		t.Fatalf("unexpected answer result: %+v", out)
	}
}

func TestAnswerErrorsOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := New(srv.URL, srv.Client(), "")
	if _, err := client.Answer(context.Background(), AnswerRequest{InputJSON: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("expected an error on a non-200 status")
	}
}

func TestEmbedMapsResponseAndUsesInternalAuth(t *testing.T) {
	var gotToken, gotTask string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Axis-Internal-Token")
		var request struct {
			Texts []string `json:"texts"`
			Task  string   `json:"task_type"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		gotTask = request.Task
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "gemini-embedding-001", "dimensions": 768,
			"embeddings": [][]float32{make([]float32, 768)},
		})
	}))
	defer srv.Close()
	client := New(srv.URL, srv.Client(), "secret-token")
	result, err := client.Embed(context.Background(), EmbedRequest{Texts: []string{"clinical"}, TaskType: EmbeddingTaskDocument})
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != "secret-token" || gotTask != EmbeddingTaskDocument || result.Model != "gemini-embedding-001" || len(result.Embeddings[0]) != 768 {
		t.Fatalf("token=%q task=%q result=%#v", gotToken, gotTask, result)
	}
}
