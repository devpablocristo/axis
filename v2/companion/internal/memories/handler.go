package memories

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct{ u *UseCases }

func NewHandler(u *UseCases) *Handler { return &Handler{u: u} }
func (h *Handler) Routes(r gin.IRouter) {
	g := r.Group("/virployees/:virployee_id/memories")
	g.GET("", h.List)
	g.POST("", h.Create)
	g.POST("/recall", h.Recall)
	g.GET("/:memory_id", h.Get)
	g.PUT("/:memory_id", h.Update)
	g.POST("/:memory_id/review", h.Review)
	for _, a := range []string{"archive", "unarchive", "trash", "restore"} {
		action := a
		g.POST("/:memory_id/"+a, func(c *gin.Context) { h.lifecycle(c, action) })
	}
	g.DELETE("/:memory_id/purge", h.Purge)
}

type reviewRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (h *Handler) Review(c *gin.Context) {
	virployeeID, memoryID, ok := ids(c)
	if !ok {
		return
	}
	var request reviewRequest
	if err := ginmw.BindJSON(c, &request); err != nil {
		return
	}
	organization, actor, role := auth(c)
	memory, err := h.u.Review(c, organization, virployeeID, memoryID, actor, role, request.Decision, request.Note)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, memory)
}
func ids(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	v, e := uuid.Parse(c.Param("virployee_id"))
	if e != nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return uuid.Nil, uuid.Nil, false
	}
	m := uuid.Nil
	if raw := c.Param("memory_id"); raw != "" {
		m, e = uuid.Parse(raw)
		if e != nil {
			ginmw.Respond(c, ginmw.ErrBadInput)
			return uuid.Nil, uuid.Nil, false
		}
	}
	return v, m, true
}
func auth(c *gin.Context) (string, string, string) {
	return strings.TrimSpace(c.GetHeader("X-Org-ID")), strings.TrimSpace(c.GetHeader("X-Actor-ID")), strings.TrimSpace(c.GetHeader("X-Axis-Org-Role"))
}

type createRequest struct {
	Title       string `json:"title"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Sensitivity string `json:"sensitivity"`
	Scope       Scope  `json:"scope"`
}

func (h *Handler) Create(c *gin.Context) {
	v, _, ok := ids(c)
	if !ok {
		return
	}
	var q createRequest
	if err := ginmw.BindJSON(c, &q); err != nil {
		return
	}
	t, a, r := auth(c)
	m, err := h.u.Create(c, t, v, a, r, CreateInput{Title: q.Title, Type: q.Type, Content: q.Content, Sensitivity: q.Sensitivity, Scope: q.Scope})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, m)
}
func (h *Handler) Get(c *gin.Context) {
	v, m, ok := ids(c)
	if !ok {
		return
	}
	t, a, r := auth(c)
	out, err := h.u.Get(c, t, v, m, a, r)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}
func (h *Handler) List(c *gin.Context) {
	v, _, ok := ids(c)
	if !ok {
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil {
			ginmw.Respond(c, domainerr.Validation("limit must be an integer"))
			return
		}
		limit = n
	}
	t, a, r := auth(c)
	scope, err := scopeFromQuery(c)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	out, err := h.u.List(c, t, v, a, r, ListInput{State: c.Query("state"), Query: c.Query("q"), Cursor: c.Query("cursor"), Limit: limit, Scope: scope})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

type updateRequest struct {
	createRequest
	ExpectedVersion int `json:"expected_version"`
}

func (h *Handler) Update(c *gin.Context) {
	v, m, ok := ids(c)
	if !ok {
		return
	}
	var q updateRequest
	if err := ginmw.BindJSON(c, &q); err != nil {
		return
	}
	t, a, r := auth(c)
	out, err := h.u.Update(c, t, v, m, a, r, UpdateInput{Title: q.Title, Type: q.Type, Content: q.Content, Sensitivity: q.Sensitivity, ExpectedVersion: q.ExpectedVersion})
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

type recallRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	Scope Scope  `json:"scope"`
}

func (h *Handler) Recall(c *gin.Context) {
	v, _, ok := ids(c)
	if !ok {
		return
	}
	var q recallRequest
	if err := ginmw.BindJSON(c, &q); err != nil {
		return
	}
	t, a, r := auth(c)
	out, err := h.u.RecallScoped(c, t, v, a, r, q.Scope, q.Query, q.Limit)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"items": out, "memory_context_hash": ContextHash(out)})
}

func scopeFromQuery(c *gin.Context) (Scope, error) {
	scope := Scope{Type: c.Query("scope_type"), SubjectID: c.Query("subject_id")}
	if raw := strings.TrimSpace(c.Query("case_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return Scope{}, domainerr.Validation("case_id must be a valid UUID")
		}
		scope.CaseID = &id
	}
	return NormalizeScope(scope)
}
func (h *Handler) lifecycle(c *gin.Context, action string) {
	v, m, ok := ids(c)
	if !ok {
		return
	}
	t, a, r := auth(c)
	if err := h.u.Lifecycle(c, t, v, m, a, r, action); err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *Handler) Purge(c *gin.Context) {
	v, m, ok := ids(c)
	if !ok {
		return
	}
	t, a, r := auth(c)
	if err := h.u.Purge(c, t, v, m, a, r); err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
