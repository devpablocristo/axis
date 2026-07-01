package handoffs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/identityctx"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
)

func TestHandlerCreatesAndUpdatesVirployeeHandoff(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	toVirployeeID := uuid.NewString()
	body := bytes.NewBufferString(`{"to_virployee_id":"` + toVirployeeID + `","reason":"Needs medical review"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/handoffs?org_id=org-a&product_surface=axis&tenant_id=11111111-1111-4111-8111-111111111111", body)
	req = withHandoffPrincipal(req, []string{"companion:virployees:admin"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", res.Code, res.Body.String())
	}
	var created Handoff
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ToVirployeeID.String() != toVirployeeID || created.Status != StatusPending {
		t.Fatalf("unexpected handoff: %+v", created)
	}

	req = httptest.NewRequest(http.MethodPatch, "/v1/handoffs/"+created.HandoffID.String()+"?org_id=org-a&product_surface=axis&tenant_id=11111111-1111-4111-8111-111111111111", bytes.NewBufferString(`{"status":"accepted"}`))
	req = withHandoffPrincipal(req, []string{"companion:virployees:admin"})
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var updated Handoff
	if err := json.Unmarshal(res.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusAccepted {
		t.Fatalf("expected accepted handoff, got %+v", updated)
	}
}

func TestHandlerRejectsMissingToVirployee(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/handoffs?org_id=org-a&product_surface=axis&tenant_id=11111111-1111-4111-8111-111111111111", bytes.NewBufferString(`{"reason":"missing target"}`))
	req = withHandoffPrincipal(req, []string{"companion:virployees:admin"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", res.Code, res.Body.String())
	}
}

type fakeRepo struct {
	items map[uuid.UUID]Handoff
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{items: map[uuid.UUID]Handoff{}}
}

func (r *fakeRepo) List(_ context.Context, tenantID, orgID, productSurface string, status Status, _ int) ([]Handoff, error) {
	out := make([]Handoff, 0, len(r.items))
	for _, item := range r.items {
		if item.TenantID.String() != tenantID || item.OrgID != orgID || item.ProductSurface != productSurface {
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *fakeRepo) Get(_ context.Context, tenantID, orgID, productSurface, handoffID string) (Handoff, error) {
	id, err := uuid.Parse(handoffID)
	if err != nil {
		return Handoff{}, ErrNotFound
	}
	item, ok := r.items[id]
	if !ok || item.TenantID.String() != tenantID || item.OrgID != orgID || item.ProductSurface != productSurface {
		return Handoff{}, ErrNotFound
	}
	return item, nil
}

func (r *fakeRepo) Create(_ context.Context, handoff Handoff) (Handoff, error) {
	if handoff.HandoffID == uuid.Nil {
		handoff.HandoffID = uuid.New()
	}
	r.items[handoff.HandoffID] = handoff
	return handoff, nil
}

func (r *fakeRepo) Update(_ context.Context, handoff Handoff) (Handoff, error) {
	if _, ok := r.items[handoff.HandoffID]; !ok {
		return Handoff{}, ErrNotFound
	}
	r.items[handoff.HandoffID] = handoff
	return handoff, nil
}

func withHandoffPrincipal(req *http.Request, scopes []string) *http.Request {
	principal := &authn.Principal{
		OrgID:      "org-a",
		Actor:      "admin",
		Scopes:     scopes,
		AuthMethod: "internal_jwt",
		Claims:     map[string]any{"product_surface": "axis"},
	}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}
