package virployees

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/handler/dto"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/devpablocristo/platform/lifecycle/go/paths"
)

type UseCasesPort interface {
	Create(context.Context, string, domain.CreateInput) (domain.Virployee, error)
	ListActive(context.Context, string) ([]domain.Virployee, error)
	ListArchived(context.Context, string) ([]domain.Virployee, error)
	ListTrash(context.Context, string) ([]domain.Virployee, error)
	Get(context.Context, string, uuid.UUID) (domain.Virployee, error)
	RuntimeContext(context.Context, string, uuid.UUID) (runtimecontext.Context, error)
	DryRun(context.Context, string, uuid.UUID, string) (dryrun.Result, error)
	ExecutionGate(context.Context, string, uuid.UUID, string, *executiongate.ConfirmedDraft) (executiongate.Result, error)
	SimulateApprovedExecution(context.Context, string, uuid.UUID, uuid.UUID) (runtraces.Trace, error)
	ExecuteApprovedAction(context.Context, string, uuid.UUID, uuid.UUID) (runtraces.Trace, error)
	Assist(context.Context, string, uuid.UUID, json.RawMessage, string, AssistMetadata) (AssistRun, error)
	SubmitAssistAsync(context.Context, string, uuid.UUID, json.RawMessage, string, AssistMetadata) (AssistRun, error)
	GetAssistRun(context.Context, string, uuid.UUID, uuid.UUID) (AssistRun, error)
	ListRuns(context.Context, string, uuid.UUID, int) ([]runtraces.Trace, error)
	Update(context.Context, string, uuid.UUID, domain.UpdateInput) (domain.Virployee, error)
	Archive(context.Context, string, uuid.UUID, string, string) error
	Unarchive(context.Context, string, uuid.UUID, string, string) error
	Trash(context.Context, string, uuid.UUID, string, string) error
	Restore(context.Context, string, uuid.UUID, string, string) error
	Purge(context.Context, string, uuid.UUID, string, string) error
}

type Handler struct {
	ucs UseCasesPort
}

func NewHandler(ucs UseCasesPort) *Handler {
	return &Handler{ucs: ucs}
}

func (h *Handler) Routes(router gin.IRouter) {
	group := router.Group("/virployees")
	{
		group.POST("", h.Create)
		group.GET("", h.ListActive)
		group.GET("/"+paths.SegmentArchived, h.ListArchived)
		group.GET("/trash", h.ListTrash)
		group.GET("/autonomy-levels", h.ListAutonomyLevels)
		group.GET("/:virployee_id/runtime-context", h.RuntimeContext)
		group.POST("/:virployee_id/dry-run", h.DryRun)
		group.POST("/:virployee_id/execution-gate", h.ExecutionGate)
		group.POST("/:virployee_id/simulated-executions", h.SimulateApprovedExecution)
		group.POST("/:virployee_id/executions", h.ExecuteApprovedAction)
		group.POST("/:virployee_id/assist", h.Assist)
		group.POST("/:virployee_id/assist-runs", h.SubmitAssistRun)
		group.GET("/:virployee_id/assist-runs/:run_id", h.GetAssistRun)
		group.GET("/:virployee_id/runs", h.ListRuns)
		group.GET("/:virployee_id", h.Get)
		group.PUT("/:virployee_id", h.Update)
		group.POST("/:virployee_id/"+paths.SegmentArchive, h.Archive)
		group.POST("/:virployee_id/unarchive", h.Unarchive)
		group.POST("/:virployee_id/trash", h.Trash)
		group.POST("/:virployee_id/"+paths.SegmentRestore, h.Restore)
		// POST alongside DELETE: browser ad blockers silently drop DELETE requests.
		group.POST("/:virployee_id/purge", h.Purge)
		group.DELETE("/:virployee_id/purge", h.Purge)
	}
}

func (h *Handler) SubmitAssistRun(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.AssistRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	idem := strings.TrimSpace(req.IdempotencyKey)
	if idem == "" {
		idem = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	metadata := AssistMetadata{
		AssistType: req.AssistType, ProductSurface: req.ProductSurface,
		SubjectID: req.SubjectID, RepositoryGeneration: req.RepositoryGeneration,
	}
	run, err := h.ucs.SubmitAssistAsync(c.Request.Context(), tenantID(c), id, req.InputJSON, idem, metadata)
	if err != nil {
		if respondQuotaError(c, err) {
			return
		}
		ginmw.Respond(c, err)
		return
	}
	status := http.StatusAccepted
	if run.Status == "done" || run.Status == "failed" {
		status = http.StatusOK
	}
	ginmw.WriteJSON(c, status, assistRunToDTO(run))
}

func (h *Handler) GetAssistRun(c *gin.Context) {
	virployeeID, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	runID, ok := ginmw.ParseUUIDParam(c, "run_id")
	if !ok {
		return
	}
	run, err := h.ucs.GetAssistRun(c.Request.Context(), tenantID(c), virployeeID, runID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, assistRunToDTO(run))
}

func (h *Handler) ExecuteApprovedAction(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.ExecuteApprovedActionRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	approvalID, err := uuid.Parse(strings.TrimSpace(req.ApprovalID))
	if err != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	out, err := h.ucs.ExecuteApprovedAction(c.Request.Context(), tenantID(c), id, approvalID)
	if err != nil {
		if respondQuotaError(c, err) {
			return
		}
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.RunTraceFromDomain(out))
}

func (h *Handler) Assist(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.AssistRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	idem := strings.TrimSpace(req.IdempotencyKey)
	if idem == "" {
		idem = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	metadata := AssistMetadata{
		AssistType: req.AssistType, ProductSurface: req.ProductSurface,
		SubjectID: req.SubjectID, RepositoryGeneration: req.RepositoryGeneration,
	}
	run, err := h.ucs.Assist(c.Request.Context(), tenantID(c), id, req.InputJSON, idem, metadata)
	if err != nil {
		if respondQuotaError(c, err) {
			return
		}
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, assistRunToDTO(run))
}

func respondQuotaError(c *gin.Context, err error) bool {
	retryAfter, ok := quotas.RetryAfter(err)
	if !ok {
		return false
	}
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	ginmw.WriteError(c, http.StatusTooManyRequests, "quota_exceeded", "product quota exceeded")
	return true
}

func assistRunToDTO(run AssistRun) dto.AssistRunResponse {
	return dto.AssistRunResponse{
		ID:                     run.ID.String(),
		CaseID:                 coordinationResponseUUID(run.CaseID),
		ResponsibleVirployeeID: coordinationResponseUUID(responsibleVirployeeID(run)),
		Status:                 run.Status,
		Output:                 run.Output,
		OutputText:             run.OutputText,
		Answered:               run.Answered,
		Degraded:               run.Degraded,
		Model:                  run.Model,
		PromptVersion:          run.PromptVersion,
		Error:                  run.Error,
		DurationMS:             run.DurationMS,
		Orchestration:          run.Orchestration,
	}
}

func coordinationResponseUUID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func (h *Handler) ListRuns(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	limit, ok := parseRunTraceLimit(c)
	if !ok {
		return
	}
	out, err := h.ucs.ListRuns(c.Request.Context(), tenantID(c), id, limit)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ListRunTracesFromDomain(out))
}

func (h *Handler) SimulateApprovedExecution(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.SimulateApprovedExecutionRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	approvalID, err := uuid.Parse(strings.TrimSpace(req.ApprovalID))
	if err != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return
	}
	out, err := h.ucs.SimulateApprovedExecution(c.Request.Context(), tenantID(c), id, approvalID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.RunTraceFromDomain(out))
}

func (h *Handler) ListAutonomyLevels(c *gin.Context) {
	ginmw.WriteJSON(c, http.StatusOK, dto.ListAutonomyLevelsFromDomain(domain.AutonomyDefinitions()))
}

func (h *Handler) Create(c *gin.Context) {
	var req dto.CreateVirployeeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Create(c.Request.Context(), tenantID(c), req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, dto.VirployeeFromDomain(out))
}

func (h *Handler) ListActive(c *gin.Context) {
	out, err := h.ucs.ListActive(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ListVirployeesFromDomain(out))
}

func (h *Handler) ListArchived(c *gin.Context) {
	out, err := h.ucs.ListArchived(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ListVirployeesFromDomain(out))
}

func (h *Handler) ListTrash(c *gin.Context) {
	out, err := h.ucs.ListTrash(c.Request.Context(), tenantID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ListVirployeesFromDomain(out))
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	out, err := h.ucs.Get(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.VirployeeFromDomain(out))
}

func (h *Handler) RuntimeContext(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	out, err := h.ucs.RuntimeContext(c.Request.Context(), tenantID(c), id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.RuntimeContextFromDomain(out))
}

func (h *Handler) DryRun(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.DryRunVirployeeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.DryRun(c.Request.Context(), tenantID(c), id, req.Input)
	if err != nil {
		if respondQuotaError(c, err) {
			return
		}
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.DryRunFromDomain(out))
}

func (h *Handler) ExecutionGate(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.ExecutionGateVirployeeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.ExecutionGate(c.Request.Context(), tenantID(c), id, req.Input, req.ConfirmedDraftToDomain())
	if err != nil {
		if respondQuotaError(c, err) {
			return
		}
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.ExecutionGateFromDomain(out))
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	var req dto.UpdateVirployeeRequest
	if err := ginmw.BindJSON(c, &req); err != nil {
		return
	}
	out, err := h.ucs.Update(c.Request.Context(), tenantID(c), id, req.ToDomain())
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, dto.VirployeeFromDomain(out))
}

func (h *Handler) Archive(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Archive)
}

func (h *Handler) Unarchive(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Unarchive)
}

func (h *Handler) Trash(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Trash)
}

func (h *Handler) Restore(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Restore)
}

func (h *Handler) Purge(c *gin.Context) {
	h.lifecycleAction(c, h.ucs.Purge)
}

func (h *Handler) lifecycleAction(c *gin.Context, fn func(context.Context, string, uuid.UUID, string, string) error) {
	id, ok := ginmw.ParseUUIDParam(c, "virployee_id")
	if !ok {
		return
	}
	req, ok := bindLifecycleRequest(c)
	if !ok {
		return
	}
	if err := fn(c.Request.Context(), tenantID(c), id, actorID(c), req.Reason); err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteNoContent(c)
}

func bindLifecycleRequest(c *gin.Context) (dto.LifecycleRequest, bool) {
	var req dto.LifecycleRequest
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return req, true
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, true
		}
		ginmw.Respond(c, ginmw.ErrBadInput)
		return dto.LifecycleRequest{}, false
	}
	return req, true
}

func tenantID(c *gin.Context) string {
	tenant := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
	if tenant == "" {
		return DefaultTenantID
	}
	return tenant
}

func actorID(c *gin.Context) string {
	actor := strings.TrimSpace(c.GetHeader("X-Actor-ID"))
	if actor == "" {
		return DefaultActorID
	}
	return actor
}

func parseRunTraceLimit(c *gin.Context) (int, bool) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return 20, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return 0, false
	}
	if limit > 100 {
		limit = 100
	}
	return limit, true
}
