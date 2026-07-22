package enterpriseops

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Routes(r gin.IRouter) {
	g := r.Group("/operations")
	g.GET("/overview", h.overview)
	g.GET("/reconciliations", h.listReconciliations)
	g.POST("/reconciliations", h.runReconciliation)
	g.GET("/reconciliations/:reconciliation_id", h.getReconciliation)
	g.GET("/jobs", h.listJobs)
	g.GET("/jobs/:job_id", h.getJob)
	g.POST("/jobs/:job_id/cancel", h.cancelJob)
	g.POST("/jobs/:job_id/replay", h.replayJob)
	g.GET("/incidents", h.listIncidents)
	g.POST("/incidents/:incident_id/acknowledge", h.acknowledgeIncident)
	g.POST("/incidents/:incident_id/suppress", h.suppressIncident)
	g.POST("/incidents/:incident_id/resolve", h.resolveIncident)
	g.GET("/slos", h.listSLOs)
	g.PUT("/slos", h.putSLO)
	g.GET("/worker-controls", h.workerControls)
	g.PUT("/worker-controls", h.putWorkerControl)
	g.GET("/notifications", h.getNotifications)
	g.PUT("/notifications", h.putNotifications)
	g.GET("/legal-holds", h.listLegalHolds)
	g.POST("/legal-holds", h.createLegalHold)
	g.POST("/legal-holds/:hold_id/release", h.releaseLegalHold)
	g.GET("/exports", h.listExports)
	g.POST("/exports", h.createExport)
	g.GET("/exports/:export_id", h.getExport)
	g.POST("/exports/:export_id/download-token", h.downloadToken)
	g.GET("/exports/:export_id/download", h.download)
	r.POST("/internal/operations/findings", h.ingestFinding)
}
func opContext(c *gin.Context) (string, string, string, string) {
	return strings.TrimSpace(c.GetHeader("X-Org-ID")), strings.TrimSpace(c.GetHeader("X-Actor-ID")), strings.ToLower(strings.TrimSpace(c.GetHeader("X-Axis-Org-Role"))), strings.ToLower(strings.TrimSpace(c.GetHeader("X-Product-Surface")))
}
func pagination(c *gin.Context) (int, int) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := 0
	if raw, err := base64.RawURLEncoding.DecodeString(c.Query("cursor")); err == nil {
		offset, _ = strconv.Atoi(string(raw))
		if offset < 0 {
			offset = 0
		}
	}
	return limit, offset
}
func pageCursor(offset, limit int, more bool) string {
	if !more {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
}
func respond(c *gin.Context, value any, err error) {
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, value)
}
func (h *Handler) overview(c *gin.Context) {
	t, a, r, p := opContext(c)
	v, e := h.service.Overview(c, t, a, r, p)
	respond(c, v, e)
}
func (h *Handler) ingestFinding(c *gin.Context) {
	var in FindingInput
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		ginmw.Respond(c, domainerr.Validation("operational finding payload is invalid"))
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		ginmw.Respond(c, domainerr.Validation("operational finding payload must contain one JSON object"))
		return
	}
	t, a, _, _ := opContext(c)
	v, created, e := h.service.IngestFinding(c, t, a, c.GetHeader("Idempotency-Key"), in)
	if e != nil {
		ginmw.Respond(c, e)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	ginmw.WriteJSON(c, status, v)
}
func (h *Handler) listIncidents(c *gin.Context) {
	t, a, r, p := opContext(c)
	l, o := pagination(c)
	v, m, e := h.service.ListIncidents(c, t, a, r, p, c.Query("status"), l, o)
	respond(c, Page[Incident]{Items: v, NextCursor: pageCursor(o, l, m)}, e)
}
func (h *Handler) incidentAction(c *gin.Context, action string) {
	id, ok := ginmw.ParseUUIDParam(c, "incident_id")
	if !ok {
		return
	}
	var in IncidentActionInput
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.ActOnIncident(c, t, a, r, p, c.GetHeader("Idempotency-Key"), action, id, in)
	respond(c, v, e)
}
func (h *Handler) acknowledgeIncident(c *gin.Context) { h.incidentAction(c, "acknowledge") }
func (h *Handler) suppressIncident(c *gin.Context)    { h.incidentAction(c, "suppress") }
func (h *Handler) resolveIncident(c *gin.Context)     { h.incidentAction(c, "resolve") }
func (h *Handler) listReconciliations(c *gin.Context) {
	t, a, r, p := opContext(c)
	l, o := pagination(c)
	v, m, e := h.service.ListReconciliations(c, t, a, r, p, l, o)
	respond(c, Page[ReconciliationRun]{Items: v, NextCursor: pageCursor(o, l, m)}, e)
}
func (h *Handler) runReconciliation(c *gin.Context) {
	var in ReconciliationInput
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, p := opContext(c)
	v, created, e := h.service.RunReconciliation(c, t, a, r, p, c.GetHeader("Idempotency-Key"), in, false)
	if e != nil {
		ginmw.Respond(c, e)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	ginmw.WriteJSON(c, status, v)
}
func (h *Handler) getReconciliation(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "reconciliation_id")
	if !ok {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.GetReconciliation(c, t, a, r, p, id, false)
	respond(c, v, e)
}
func (h *Handler) listJobs(c *gin.Context) {
	t, a, r, p := opContext(c)
	l, _ := pagination(c)
	v, e := h.service.ListJobs(c, t, a, r, p, c.Query("status"), l)
	respond(c, Page[JobView]{Items: v}, e)
}
func (h *Handler) getJob(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "job_id")
	if !ok {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.GetJob(c, t, a, r, p, id)
	respond(c, v, e)
}
func (h *Handler) cancelJob(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "job_id")
	if !ok {
		return
	}
	var in struct {
		ReasonCode string `json:"reason_code"`
	}
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.CancelJob(c, t, a, r, p, c.GetHeader("Idempotency-Key"), in.ReasonCode, id)
	respond(c, v, e)
}
func (h *Handler) replayJob(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "job_id")
	if !ok {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.ReplayJob(c, t, a, r, p, c.GetHeader("Idempotency-Key"), id)
	respond(c, v, e)
}
func (h *Handler) listSLOs(c *gin.Context) {
	t, a, r, p := opContext(c)
	v, e := h.service.ListSLOs(c, t, a, r, p)
	respond(c, map[string]any{"items": v}, e)
}
func (h *Handler) putSLO(c *gin.Context) {
	var in PutSLOInput
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, _ := opContext(c)
	v, e := h.service.PutSLO(c, t, a, r, in)
	respond(c, v, e)
}
func (h *Handler) workerControls(c *gin.Context) {
	t, a, r, p := opContext(c)
	v, e := h.service.ListWorkerControls(c, t, a, r, p)
	respond(c, map[string]any{"items": v}, e)
}
func (h *Handler) putWorkerControl(c *gin.Context) {
	var in PutWorkerControlInput
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.PutWorkerControl(c, t, a, r, p, in)
	respond(c, v, e)
}
func (h *Handler) getNotifications(c *gin.Context) {
	t, a, r, p := opContext(c)
	v, e := h.service.GetNotificationPolicy(c, t, a, r, p)
	respond(c, v, e)
}
func (h *Handler) putNotifications(c *gin.Context) {
	var in PutNotificationPolicyInput
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, _ := opContext(c)
	v, e := h.service.PutNotificationPolicy(c, t, a, r, in)
	respond(c, v, e)
}
func (h *Handler) listLegalHolds(c *gin.Context) {
	t, a, r, p := opContext(c)
	v, e := h.service.ListLegalHolds(c, t, a, r, p)
	respond(c, map[string]any{"items": v}, e)
}
func (h *Handler) createLegalHold(c *gin.Context) {
	var in CreateLegalHoldInput
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, p := opContext(c)
	v, created, e := h.service.CreateLegalHold(c, t, a, r, p, c.GetHeader("Idempotency-Key"), in)
	if e != nil {
		ginmw.Respond(c, e)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	ginmw.WriteJSON(c, status, v)
}
func (h *Handler) releaseLegalHold(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "hold_id")
	if !ok {
		return
	}
	var in ReleaseLegalHoldInput
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.ReleaseLegalHold(c, t, a, r, p, c.GetHeader("Idempotency-Key"), id, in)
	respond(c, v, e)
}
func (h *Handler) listExports(c *gin.Context) {
	t, a, r, p := opContext(c)
	v, e := h.service.ListExports(c, t, a, r, p)
	respond(c, map[string]any{"items": v}, e)
}
func (h *Handler) createExport(c *gin.Context) {
	var in CreateExportInput
	if ginmw.BindJSON(c, &in) != nil {
		return
	}
	t, a, r, p := opContext(c)
	v, created, e := h.service.CreateExport(c, t, a, r, p, c.GetHeader("Idempotency-Key"), in)
	if e != nil {
		ginmw.Respond(c, e)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	ginmw.WriteJSON(c, status, v)
}
func (h *Handler) getExport(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "export_id")
	if !ok {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.GetExport(c, t, a, r, p, id)
	respond(c, v, e)
}
func (h *Handler) downloadToken(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "export_id")
	if !ok {
		return
	}
	t, a, r, p := opContext(c)
	v, e := h.service.CreateDownloadToken(c, t, a, r, p, id)
	respond(c, v, e)
}
func (h *Handler) download(c *gin.Context) {
	id, ok := ginmw.ParseUUIDParam(c, "export_id")
	if !ok {
		return
	}
	t, a, _, _ := opContext(c)
	files, manifest, e := h.service.RedeemDownload(c, t, a, c.Query("token"), id)
	if e != nil {
		ginmw.Respond(c, e)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="axis-export-`+id.String()+`.zip"`)
	c.Header("X-Manifest-SHA256", manifest)
	writer := zip.NewWriter(c.Writer)
	for name, content := range files {
		part, err := writer.Create(name)
		if err != nil {
			_ = writer.Close()
			return
		}
		_, _ = part.Write(content)
	}
	manifestPart, _ := writer.Create("manifest.sha256")
	_, _ = manifestPart.Write([]byte(manifest + "\n"))
	_ = writer.Close()
}
