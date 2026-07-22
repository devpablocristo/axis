package workforcerouting

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UseCasesPort interface {
	CreateWorkSubject(context.Context, string, CreateWorkSubjectInput) (WorkSubject, error)
	ListWorkSubjects(context.Context, string, string, string) ([]WorkSubject, error)
	GetWorkSubject(context.Context, string, uuid.UUID) (WorkSubject, error)
	UpdateWorkSubject(context.Context, string, uuid.UUID, UpdateWorkSubjectInput) (WorkSubject, error)
	ArchiveWorkSubject(context.Context, string, uuid.UUID) error
	UnarchiveWorkSubject(context.Context, string, uuid.UUID) error

	CreateRoutingPool(context.Context, string, CreateRoutingPoolInput) (RoutingPool, error)
	ListRoutingPools(context.Context, string, string) ([]RoutingPool, error)
	GetRoutingPool(context.Context, string, uuid.UUID) (RoutingPool, error)
	UpdateRoutingPool(context.Context, string, uuid.UUID, UpdateRoutingPoolInput) (RoutingPool, error)
	ArchiveRoutingPool(context.Context, string, uuid.UUID) error
	UnarchiveRoutingPool(context.Context, string, uuid.UUID) error
	UpsertPoolMember(context.Context, string, uuid.UUID, uuid.UUID, UpsertPoolMemberInput) (PoolMember, error)
	ListPoolMembers(context.Context, string, uuid.UUID) ([]PoolMember, error)

	ListRelationships(context.Context, string, uuid.UUID) ([]VirployeeRelationship, error)
	ReplaceRelationships(context.Context, string, uuid.UUID, []RelationshipInput) ([]VirployeeRelationship, error)

	Resolve(context.Context, string, ResolveInput) (ResolveResult, error)
	ListAssignments(context.Context, string, string, string) ([]ContinuityAssignment, error)
	ListAssignmentsForVirployee(context.Context, string, uuid.UUID) ([]ContinuityAssignment, error)
	Reassign(context.Context, string, uuid.UUID, ReassignInput) (ContinuityAssignment, error)
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	subjects := router.Group("/work-subjects")
	subjects.POST("", h.createWorkSubject)
	subjects.GET("", h.listWorkSubjects)
	subjects.GET("/:subject_id", h.getWorkSubject)
	subjects.PUT("/:subject_id", h.updateWorkSubject)
	subjects.POST("/:subject_id/archive", h.archiveWorkSubject)
	subjects.POST("/:subject_id/unarchive", h.unarchiveWorkSubject)

	pools := router.Group("/routing-pools")
	pools.POST("", h.createRoutingPool)
	pools.GET("", h.listRoutingPools)
	pools.GET("/:pool_id", h.getRoutingPool)
	pools.PUT("/:pool_id", h.updateRoutingPool)
	pools.POST("/:pool_id/archive", h.archiveRoutingPool)
	pools.POST("/:pool_id/unarchive", h.unarchiveRoutingPool)
	pools.GET("/:pool_id/members", h.listPoolMembers)
	pools.PUT("/:pool_id/members/:virployee_id", h.upsertPoolMember)

	router.GET("/virployees/:virployee_id/relationships", h.listRelationships)
	router.PUT("/virployees/:virployee_id/relationships", h.replaceRelationships)
	router.GET("/virployees/:virployee_id/assignments", h.listVirployeeAssignments)
	router.POST("/virployee-routing:resolve", h.resolve)
	router.POST("/virployee-routing/resolve", h.resolve)
	router.GET("/virployee-routing/assignments", h.listAssignments)
	router.POST("/virployee-routing/assignments/:assignment_id/reassign", h.reassign)
}

func (h *Handler) listVirployeeAssignments(c *gin.Context) {
	virployeeID, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	out, err := h.ucs.ListAssignmentsForVirployee(c.Request.Context(), tenantID(c), virployeeID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": assignmentsFromDomain(out)})
}

type workSubjectRequest struct {
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	ExternalRef string `json:"external_ref"`
}

type routingPoolRequest struct {
	JobRoleID string `json:"job_role_id"`
	Name      string `json:"name"`
}

type poolMemberRequest struct {
	MaxActiveSubjects int  `json:"max_active_subjects"`
	Enabled           bool `json:"enabled"`
}

type relationshipsRequest struct {
	Relationships []relationshipRequest `json:"relationships"`
}

type relationshipRequest struct {
	SubjectID string `json:"subject_id"`
	Type      string `json:"type"`
	IsPrimary bool   `json:"is_primary"`
}

type resolveRequest struct {
	PoolID        string `json:"pool_id"`
	SubjectID     string `json:"subject_id"`
	CapabilityKey string `json:"capability_key,omitempty"`
}

type reassignRequest struct {
	VirployeeID     string `json:"virployee_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type workSubjectResponse struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	Kind        string     `json:"kind"`
	DisplayName string     `json:"display_name"`
	ExternalRef string     `json:"external_ref"`
	State       string     `json:"state"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ArchivedAt  *time.Time `json:"archived_at"`
}

type routingPoolResponse struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	JobRoleID  string     `json:"job_role_id"`
	Name       string     `json:"name"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at"`
}

type poolMemberResponse struct {
	PoolID            string    `json:"pool_id"`
	VirployeeID       string    `json:"virployee_id"`
	MaxActiveSubjects int       `json:"max_active_subjects"`
	Enabled           bool      `json:"enabled"`
	ActiveSubjects    int       `json:"active_subjects"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type relationshipResponse struct {
	ID          string    `json:"id"`
	VirployeeID string    `json:"virployee_id"`
	SubjectID   string    `json:"subject_id"`
	Type        string    `json:"type"`
	IsPrimary   bool      `json:"is_primary"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type assignmentResponse struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	PoolID      string    `json:"pool_id"`
	SubjectID   string    `json:"subject_id"`
	VirployeeID string    `json:"virployee_id"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	AssignedAt  time.Time `json:"assigned_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type resolveResponse struct {
	Status     string              `json:"status"`
	Created    bool                `json:"created"`
	Assignment *assignmentResponse `json:"assignment,omitempty"`
}

func (h *Handler) createWorkSubject(c *gin.Context) {
	var req workSubjectRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.CreateWorkSubject(c.Request.Context(), tenantID(c), CreateWorkSubjectInput(req))
	if respondError(c, err) {
		return
	}
	ginmw.WriteCreated(c, workSubjectFromDomain(out))
}

func (h *Handler) listWorkSubjects(c *gin.Context) {
	out, err := h.ucs.ListWorkSubjects(c.Request.Context(), tenantID(c), c.Query("lifecycle"), c.Query("kind"))
	if respondError(c, err) {
		return
	}
	data := make([]workSubjectResponse, 0, len(out))
	for _, item := range out {
		data = append(data, workSubjectFromDomain(item))
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": data})
}

func (h *Handler) getWorkSubject(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "subject_id")
	if !ok {
		return
	}
	out, err := h.ucs.GetWorkSubject(c.Request.Context(), tenantID(c), id)
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, workSubjectFromDomain(out))
}

func (h *Handler) updateWorkSubject(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "subject_id")
	if !ok {
		return
	}
	var req workSubjectRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.UpdateWorkSubject(c.Request.Context(), tenantID(c), id, UpdateWorkSubjectInput(req))
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, workSubjectFromDomain(out))
}

func (h *Handler) archiveWorkSubject(c *gin.Context) { h.subjectLifecycle(c, h.ucs.ArchiveWorkSubject) }
func (h *Handler) unarchiveWorkSubject(c *gin.Context) {
	h.subjectLifecycle(c, h.ucs.UnarchiveWorkSubject)
}

func (h *Handler) subjectLifecycle(c *gin.Context, fn func(context.Context, string, uuid.UUID) error) {
	id, ok := ginmw.ParseUUIDParam(c, "subject_id")
	if !ok {
		return
	}
	if respondError(c, fn(c.Request.Context(), tenantID(c), id)) {
		return
	}
	ginmw.WriteNoContent(c)
}

func (h *Handler) createRoutingPool(c *gin.Context) {
	var req routingPoolRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.CreateRoutingPool(c.Request.Context(), tenantID(c), CreateRoutingPoolInput(req))
	if respondError(c, err) {
		return
	}
	ginmw.WriteCreated(c, routingPoolFromDomain(out))
}

func (h *Handler) listRoutingPools(c *gin.Context) {
	out, err := h.ucs.ListRoutingPools(c.Request.Context(), tenantID(c), c.Query("lifecycle"))
	if respondError(c, err) {
		return
	}
	data := make([]routingPoolResponse, 0, len(out))
	for _, item := range out {
		data = append(data, routingPoolFromDomain(item))
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": data})
}

func (h *Handler) getRoutingPool(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "pool_id")
	if !ok {
		return
	}
	out, err := h.ucs.GetRoutingPool(c.Request.Context(), tenantID(c), id)
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, routingPoolFromDomain(out))
}

func (h *Handler) updateRoutingPool(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "pool_id")
	if !ok {
		return
	}
	var req routingPoolRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.UpdateRoutingPool(c.Request.Context(), tenantID(c), id, UpdateRoutingPoolInput(req))
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, routingPoolFromDomain(out))
}

func (h *Handler) archiveRoutingPool(c *gin.Context) { h.poolLifecycle(c, h.ucs.ArchiveRoutingPool) }
func (h *Handler) unarchiveRoutingPool(c *gin.Context) {
	h.poolLifecycle(c, h.ucs.UnarchiveRoutingPool)
}

func (h *Handler) poolLifecycle(c *gin.Context, fn func(context.Context, string, uuid.UUID) error) {
	id, ok := ginmw.ParseUUIDParam(c, "pool_id")
	if !ok {
		return
	}
	if respondError(c, fn(c.Request.Context(), tenantID(c), id)) {
		return
	}
	ginmw.WriteNoContent(c)
}

func (h *Handler) upsertPoolMember(c *gin.Context) {
	poolID, ok := ginmw.ParseUUIDParam(c, "pool_id")
	if !ok {
		return
	}
	virployeeID, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req poolMemberRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.UpsertPoolMember(c.Request.Context(), tenantID(c), poolID, virployeeID, UpsertPoolMemberInput(req))
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, poolMemberFromDomain(out))
}

func (h *Handler) listPoolMembers(c *gin.Context) {
	poolID, ok := ginmw.ParseUUIDParam(c, "pool_id")
	if !ok {
		return
	}
	out, err := h.ucs.ListPoolMembers(c.Request.Context(), tenantID(c), poolID)
	if respondError(c, err) {
		return
	}
	data := make([]poolMemberResponse, 0, len(out))
	for _, item := range out {
		data = append(data, poolMemberFromDomain(item))
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": data})
}

func (h *Handler) listRelationships(c *gin.Context) {
	virployeeID, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	out, err := h.ucs.ListRelationships(c.Request.Context(), tenantID(c), virployeeID)
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": relationshipsFromDomain(out)})
}

func (h *Handler) replaceRelationships(c *gin.Context) {
	virployeeID, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req relationshipsRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	items := make([]RelationshipInput, 0, len(req.Relationships))
	for _, item := range req.Relationships {
		items = append(items, RelationshipInput(item))
	}
	out, err := h.ucs.ReplaceRelationships(c.Request.Context(), tenantID(c), virployeeID, items)
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": relationshipsFromDomain(out)})
}

func (h *Handler) resolve(c *gin.Context) {
	var req resolveRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Resolve(c.Request.Context(), tenantID(c), ResolveInput{
		PoolID: req.PoolID, SubjectID: req.SubjectID, CapabilityKey: req.CapabilityKey, ActorID: actorID(c),
	})
	if respondError(c, err) {
		return
	}
	response := resolveResponse{Status: string(out.Status), Created: out.Created}
	if out.Assignment != nil {
		assignment := assignmentFromDomain(*out.Assignment)
		response.Assignment = &assignment
	}
	ginmw.WriteJSON(c, http.StatusOK, response)
}

func (h *Handler) listAssignments(c *gin.Context) {
	out, err := h.ucs.ListAssignments(c.Request.Context(), tenantID(c), c.Query("pool_id"), c.Query("subject_id"))
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": assignmentsFromDomain(out)})
}

func (h *Handler) reassign(c *gin.Context) {
	if !ownerOrAdmin(c.GetHeader("X-Axis-Tenant-Role")) {
		ginmw.Respond(c, domainerr.Forbidden("continuity reassignment requires an owner or admin"))
		return
	}
	assignmentID, ok := ginmw.ParseUUIDParam(c, "assignment_id")
	if !ok {
		return
	}
	var req reassignRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Reassign(c.Request.Context(), tenantID(c), assignmentID, ReassignInput{
		VirployeeID: req.VirployeeID, ExpectedVersion: req.ExpectedVersion, Reason: req.Reason, ActorID: actorID(c),
	})
	if respondError(c, err) {
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, assignmentFromDomain(out))
}

func ownerOrAdmin(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "owner", "admin":
		return true
	default:
		return false
	}
}

func workSubjectFromDomain(item WorkSubject) workSubjectResponse {
	return workSubjectResponse{ID: item.ID.String(), TenantID: item.TenantID, Kind: string(item.Kind), DisplayName: item.DisplayName,
		ExternalRef: item.ExternalRef, State: string(item.State()), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, ArchivedAt: item.ArchivedAt}
}

func routingPoolFromDomain(item RoutingPool) routingPoolResponse {
	return routingPoolResponse{ID: item.ID.String(), TenantID: item.TenantID, JobRoleID: item.JobRoleID.String(), Name: item.Name,
		State: string(item.State()), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, ArchivedAt: item.ArchivedAt}
}

func poolMemberFromDomain(item PoolMember) poolMemberResponse {
	return poolMemberResponse{PoolID: item.PoolID.String(), VirployeeID: item.VirployeeID.String(), MaxActiveSubjects: item.MaxActiveSubjects,
		Enabled: item.Enabled, ActiveSubjects: item.ActiveSubjects, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func relationshipsFromDomain(items []VirployeeRelationship) []relationshipResponse {
	out := make([]relationshipResponse, 0, len(items))
	for _, item := range items {
		out = append(out, relationshipResponse{ID: item.ID.String(), VirployeeID: item.VirployeeID.String(), SubjectID: item.SubjectID.String(),
			Type: string(item.RelationshipType), IsPrimary: item.IsPrimary, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	return out
}

func assignmentFromDomain(item ContinuityAssignment) assignmentResponse {
	return assignmentResponse{ID: item.ID.String(), TenantID: item.TenantID, PoolID: item.PoolID.String(), SubjectID: item.SubjectID.String(),
		VirployeeID: item.VirployeeID.String(), Status: item.Status, Version: item.Version, AssignedAt: item.AssignedAt, UpdatedAt: item.UpdatedAt}
}

func assignmentsFromDomain(items []ContinuityAssignment) []assignmentResponse {
	out := make([]assignmentResponse, 0, len(items))
	for _, item := range items {
		out = append(out, assignmentFromDomain(item))
	}
	return out
}

func respondError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	ginmw.Respond(c, err)
	return true
}

func tenantID(c *gin.Context) string {
	value := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
	if value == "" {
		return "default"
	}
	return value
}

func actorID(c *gin.Context) string {
	value := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if value == "" {
		return "system"
	}
	return value
}
