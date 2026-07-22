package workforcerouting

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestHandlerResolveUsesOrgActorAndSafeRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assignment := ContinuityAssignment{ID: uuid.New(), OrgID: "organization-1", PoolID: uuid.New(), SubjectID: uuid.New(), VirployeeID: uuid.New(), Status: "active", Version: 1}
	stub := &handlerUseCasesStub{resolveResult: ResolveResult{Status: ResolveStatusAssigned, Created: true, Assignment: &assignment}}
	router := gin.New()
	NewHandler(stub).Routes(router.Group("/v1"))
	body, _ := json.Marshal(resolveRequest{PoolID: assignment.PoolID.String(), SubjectID: assignment.SubjectID.String()})
	req := httptest.NewRequest(http.MethodPost, "/v1/virployee-routing/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", "organization-1")
	req.Header.Set("X-Actor-ID", "owner-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if stub.orgID != "organization-1" || stub.resolveInput.ActorID != "owner-1" {
		t.Fatalf("request context was not forwarded: organization=%q input=%+v", stub.orgID, stub.resolveInput)
	}
	var response resolveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != string(ResolveStatusAssigned) || !response.Created || response.Assignment == nil || response.Assignment.ID != assignment.ID.String() {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestHandlerResolveSupportsActionStyleContractRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assignment := ContinuityAssignment{ID: uuid.New(), OrgID: "organization-1", PoolID: uuid.New(), SubjectID: uuid.New(), VirployeeID: uuid.New(), Status: "active", Version: 1}
	stub := &handlerUseCasesStub{resolveResult: ResolveResult{Status: ResolveStatusAssigned, Assignment: &assignment}}
	router := gin.New()
	NewHandler(stub).Routes(router.Group("/v1"))
	body, _ := json.Marshal(resolveRequest{PoolID: assignment.PoolID.String(), SubjectID: assignment.SubjectID.String()})
	req := httptest.NewRequest(http.MethodPost, "/v1/virployee-routing:resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", "organization-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerReplaceRelationships(t *testing.T) {
	gin.SetMode(gin.TestMode)
	virployeeID := uuid.New()
	employerID := uuid.New()
	stub := &handlerUseCasesStub{relationships: []VirployeeRelationship{{
		ID: uuid.New(), VirployeeID: virployeeID, SubjectID: employerID, RelationshipType: RelationshipWorksFor, IsPrimary: true,
	}}}
	router := gin.New()
	NewHandler(stub).Routes(router.Group("/v1"))
	body := []byte(`{"relationships":[{"subject_id":"` + employerID.String() + `","type":"works_for","is_primary":true}]}`)
	req := httptest.NewRequest(http.MethodPut, "/v1/virployees/"+virployeeID.String()+"/relationships", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", "organization-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if stub.virployeeID != virployeeID || len(stub.relationshipInputs) != 1 || !stub.relationshipInputs[0].IsPrimary {
		t.Fatalf("unexpected relationship input: virployee=%s input=%+v", stub.virployeeID, stub.relationshipInputs)
	}
}

func TestHandlerListsOnlyVirployeeAssignments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	virployeeID := uuid.New()
	assignment := ContinuityAssignment{ID: uuid.New(), OrgID: "organization-1", PoolID: uuid.New(), SubjectID: uuid.New(), VirployeeID: virployeeID, Status: "active", Version: 1}
	stub := &handlerUseCasesStub{assignments: []ContinuityAssignment{assignment}}
	router := gin.New()
	NewHandler(stub).Routes(router.Group("/v1"))
	req := httptest.NewRequest(http.MethodGet, "/v1/virployees/"+virployeeID.String()+"/assignments", nil)
	req.Header.Set("X-Org-ID", "organization-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if stub.virployeeID != virployeeID || stub.orgID != "organization-1" {
		t.Fatalf("unexpected scope: organization=%q virployee=%s", stub.orgID, stub.virployeeID)
	}
}

func TestHandlerReassignRequiresOwnerOrAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assignment := ContinuityAssignment{ID: uuid.New(), OrgID: "organization-1", PoolID: uuid.New(), SubjectID: uuid.New(), VirployeeID: uuid.New(), Status: "active", Version: 2}
	for _, tc := range []struct {
		name       string
		role       string
		wantStatus int
		wantCalls  int
	}{
		{name: "missing role", wantStatus: http.StatusForbidden},
		{name: "member", role: "member", wantStatus: http.StatusForbidden},
		{name: "owner", role: "owner", wantStatus: http.StatusOK, wantCalls: 1},
		{name: "admin", role: "admin", wantStatus: http.StatusOK, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &handlerUseCasesStub{reassignResult: assignment}
			router := gin.New()
			NewHandler(stub).Routes(router.Group("/v1"))
			body := []byte(`{"virployee_id":"` + assignment.VirployeeID.String() + `","expected_version":1,"reason":"manual_reassignment"}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/virployee-routing/assignments/"+assignment.ID.String()+"/reassign", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Org-ID", "organization-1")
			req.Header.Set("X-Actor-ID", "actor-1")
			req.Header.Set("X-Axis-Org-Role", tc.role)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if stub.reassignCalls != tc.wantCalls {
				t.Fatalf("expected %d reassign calls, got %d", tc.wantCalls, stub.reassignCalls)
			}
		})
	}
}

type handlerUseCasesStub struct {
	UseCasesPort
	orgID              string
	virployeeID        uuid.UUID
	resolveInput       ResolveInput
	resolveResult      ResolveResult
	relationshipInputs []RelationshipInput
	relationships      []VirployeeRelationship
	assignments        []ContinuityAssignment
	reassignResult     ContinuityAssignment
	reassignCalls      int
}

func (s *handlerUseCasesStub) ListAssignmentsForVirployee(_ context.Context, orgID string, virployeeID uuid.UUID) ([]ContinuityAssignment, error) {
	s.orgID = orgID
	s.virployeeID = virployeeID
	return s.assignments, nil
}

func (s *handlerUseCasesStub) Reassign(_ context.Context, orgID string, assignmentID uuid.UUID, in ReassignInput) (ContinuityAssignment, error) {
	s.orgID = orgID
	s.reassignCalls++
	return s.reassignResult, nil
}

func (s *handlerUseCasesStub) Resolve(_ context.Context, orgID string, in ResolveInput) (ResolveResult, error) {
	s.orgID = orgID
	s.resolveInput = in
	return s.resolveResult, nil
}

func (s *handlerUseCasesStub) ReplaceRelationships(_ context.Context, orgID string, virployeeID uuid.UUID, in []RelationshipInput) ([]VirployeeRelationship, error) {
	s.orgID = orgID
	s.virployeeID = virployeeID
	s.relationshipInputs = in
	return s.relationships, nil
}
