package operations

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

type Handler struct{ ucs *UseCases }

func NewHandler(ucs *UseCases) *Handler { return &Handler{ucs: ucs} }
func (h *Handler) Routes(r gin.IRouter) {
	g := r.Group("/operations")
	g.GET("/overview", h.overview)
	g.GET("/fleet", h.fleet)
	g.GET("/reconciliations", h.listReconciliations)
	g.POST("/reconciliations", h.reconcile)
	g.GET("/reconciliations/:reconciliation_id", h.getReconciliation)
	g.GET("/jobs", h.listJobs)
	g.GET("/jobs/:job_id", h.getJob)
	g.POST("/jobs/:job_id/cancel", h.cancelJob)
	g.POST("/jobs/:job_id/replay", h.replayJob)
	g.GET("/worker-controls", h.workerControls)
	g.PUT("/worker-controls", h.putWorkerControl)
	g.GET("/outbox", h.outbox)
	g.POST("/outbox/:message_id/replay", h.replayOutbox)
}
func rc(c *gin.Context) (string, string, string, string) {
	return strings.TrimSpace(c.GetHeader("X-Tenant-ID")), strings.ToLower(strings.TrimSpace(c.GetHeader("X-Product-Surface"))), strings.TrimSpace(c.GetHeader("X-Actor-ID")), strings.TrimSpace(c.GetHeader("X-Axis-Tenant-Role"))
}
func page(c *gin.Context) (int, int) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
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
func cursor(offset, limit int, more bool) string {
	if !more {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
}
func ok(c *gin.Context, value any, err error) {
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, value)
}
func (h *Handler) overview(c *gin.Context) {
	t, p, a, r := rc(c)
	v, e := h.ucs.Overview(c, t, p, a, r)
	ok(c, v, e)
}
func (h *Handler) fleet(c *gin.Context) {
	t, p, a, r := rc(c)
	v, e := h.ucs.Fleet(c, t, p, a, r)
	ok(c, map[string]any{"items": v}, e)
}
func (h *Handler) listReconciliations(c *gin.Context) {
	t, p, a, r := rc(c)
	l, o := page(c)
	v, m, e := h.ucs.ListReconciliations(c, t, p, a, r, l, o)
	ok(c, map[string]any{"items": v, "next_cursor": cursor(o, l, m)}, e)
}
func (h *Handler) reconcile(c *gin.Context) {
	var in CreateReconciliationInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	t, p, a, r := rc(c)
	in.IdempotencyKey = c.GetHeader("Idempotency-Key")
	in.TriggerType = "manual"
	v, created, e := h.ucs.RunReconciliation(c, t, p, a, r, in)
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
	id, valid := ginmw.ParseUUIDParam(c, "reconciliation_id")
	if !valid {
		return
	}
	t, p, a, r := rc(c)
	v, e := h.ucs.GetReconciliation(c, t, p, a, r, id)
	ok(c, v, e)
}
func (h *Handler) listJobs(c *gin.Context) {
	t, p, a, r := rc(c)
	l, o := page(c)
	v, m, e := h.ucs.ListJobs(c, t, p, a, r, c.Query("status"), l, o)
	ok(c, map[string]any{"items": v, "next_cursor": cursor(o, l, m)}, e)
}
func (h *Handler) getJob(c *gin.Context) {
	id, valid := ginmw.ParseUUIDParam(c, "job_id")
	if !valid {
		return
	}
	t, p, a, r := rc(c)
	v, e := h.ucs.GetJob(c, t, p, a, r, id)
	ok(c, v, e)
}
func (h *Handler) cancelJob(c *gin.Context) {
	id, valid := ginmw.ParseUUIDParam(c, "job_id")
	if !valid {
		return
	}
	var in struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	t, p, a, r := rc(c)
	v, e := h.ucs.CancelJob(c, t, p, a, r, c.GetHeader("Idempotency-Key"), in.ReasonCode, id)
	ok(c, v, e)
}
func (h *Handler) replayJob(c *gin.Context) {
	id, valid := ginmw.ParseUUIDParam(c, "job_id")
	if !valid {
		return
	}
	var in struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	t, p, a, r := rc(c)
	v, e := h.ucs.ReplayJob(c, t, p, a, r, c.GetHeader("Idempotency-Key"), id)
	ok(c, v, e)
}
func (h *Handler) outbox(c *gin.Context) {
	t, p, a, r := rc(c)
	l, o := page(c)
	v, m, e := h.ucs.ListOutbox(c, t, p, a, r, c.Query("status"), l, o)
	ok(c, map[string]any{"items": v, "next_cursor": cursor(o, l, m)}, e)
}
func (h *Handler) replayOutbox(c *gin.Context) {
	id, valid := ginmw.ParseUUIDParam(c, "message_id")
	if !valid {
		return
	}
	var in struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	t, p, a, r := rc(c)
	v, e := h.ucs.ReplayOutbox(c, t, p, a, r, c.GetHeader("Idempotency-Key"), id)
	ok(c, v, e)
}
func (h *Handler) workerControls(c *gin.Context) {
	t, p, a, r := rc(c)
	v, e := h.ucs.WorkerControls(c, t, p, a, r)
	ok(c, map[string]any{"items": v}, e)
}
func (h *Handler) putWorkerControl(c *gin.Context) {
	var in PutWorkerControlInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	t, p, a, r := rc(c)
	v, e := h.ucs.PutWorkerControl(c, t, p, a, r, in)
	ok(c, v, e)
}
