package nexus_assist

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devpablocristo/companion/internal/nexusclient"
	coreai "github.com/devpablocristo/platform/kernels/ai/go"
)

type stubLLM struct{}

func (stubLLM) Chat(context.Context, coreai.ChatRequest) (coreai.ChatResponse, error) {
	return coreai.ChatResponse{Text: "summary"}, nil
}

// newGuardTestContextualizer builds a Contextualizer whose Nexus client points at
// a stub server that returns a request owned by reqOrgID.
func newGuardTestContextualizer(t *testing.T, reqOrgID string) *Contextualizer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"req-1","org_id":"` + reqOrgID + `","action_type":"alert.escalate"}`))
	}))
	t.Cleanup(srv.Close)
	return NewContextualizer(nexusclient.NewClient(srv.URL, "k"), stubLLM{})
}

// TestContextualizerRejectsCrossOrgRequest is the core fail-closed assertion:
// Companion calls Nexus with a cross_org service key, so an org-scoped caller
// must NOT be able to explain another org's request.
func TestContextualizerRejectsCrossOrgRequest(t *testing.T) {
	t.Parallel()
	c := newGuardTestContextualizer(t, "org-B")

	_, _, err := c.Explain(context.Background(), "req-1", "org-A", false)
	if !errors.Is(err, ErrRequestForbidden) {
		t.Fatalf("expected ErrRequestForbidden for cross-org request, got %v", err)
	}
}

func TestContextualizerAllowsSameOrgRequest(t *testing.T) {
	t.Parallel()
	c := newGuardTestContextualizer(t, "org-A")

	summary, _, err := c.Explain(context.Background(), "req-1", "org-A", false)
	if err != nil {
		t.Fatalf("same-org request should succeed, got %v", err)
	}
	if summary == "" {
		t.Fatal("expected a summary")
	}
}

// A cross-org caller (e.g. companion:cross_org / dev) is allowed across orgs.
func TestContextualizerAllowsCrossOrgCaller(t *testing.T) {
	t.Parallel()
	c := newGuardTestContextualizer(t, "org-B")

	if _, _, err := c.Explain(context.Background(), "req-1", "org-A", true); err != nil {
		t.Fatalf("cross-org caller should be allowed, got %v", err)
	}
}
