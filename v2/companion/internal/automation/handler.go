package automation

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Routes(r gin.IRouter) {
	r.GET("/watchers", h.list)
	r.POST("/watchers", h.create)
	r.POST("/watchers/:id/versions", h.createVersion)
	r.POST("/watcher-versions/:id/activate", h.activate)
	r.POST("/watchers/:id/pause", h.pause)
	r.POST("/watchers/:id/resume", h.resume)
	r.POST("/watchers/:id/run", h.run)
	r.POST("/product-events", h.productEvent)
}

func trusted(c *gin.Context) (string, string, string) {
	return strings.TrimSpace(c.GetHeader("X-Org-ID")), strings.TrimSpace(c.GetHeader("X-Actor-ID")), strings.ToLower(strings.TrimSpace(c.GetHeader("X-Axis-Org-Role")))
}

func decode(c *gin.Context, out any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		ginmw.Respond(c, domainerr.Validation("invalid request body"))
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		ginmw.Respond(c, domainerr.Validation("request body must contain one object"))
		return false
	}
	return true
}

func parse(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation("id must be a UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func respond(c *gin.Context, status int, value any, err error) {
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, status, value)
}

func (h *Handler) list(c *gin.Context) {
	org, actor, _ := trusted(c)
	out, err := h.service.List(c, org, actor)
	respond(c, http.StatusOK, gin.H{"items": out}, err)
}

func (h *Handler) create(c *gin.Context) {
	var in struct {
		ProductID string `json:"product_id"`
		Name      string `json:"name"`
		Mode      string `json:"mode"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.Create(c, org, actor, role, in.ProductID, in.Name, in.Mode)
	respond(c, http.StatusCreated, out, err)
}

func (h *Handler) createVersion(c *gin.Context) {
	id, ok := parse(c)
	if !ok {
		return
	}
	var in VersionInput
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.CreateVersion(c, org, actor, role, id, in)
	respond(c, http.StatusCreated, out, err)
}

func (h *Handler) activate(c *gin.Context) {
	id, ok := parse(c)
	if !ok {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.Activate(c, org, actor, role, id)
	respond(c, http.StatusOK, out, err)
}

func (h *Handler) pause(c *gin.Context) {
	id, ok := parse(c)
	if !ok {
		return
	}
	org, actor, role := trusted(c)
	err := h.service.Pause(c, org, actor, role, id)
	respond(c, http.StatusOK, gin.H{"status": "paused"}, err)
}

func (h *Handler) resume(c *gin.Context) {
	id, ok := parse(c)
	if !ok {
		return
	}
	org, actor, role := trusted(c)
	err := h.service.SetActive(c, org, actor, role, id)
	respond(c, http.StatusOK, gin.H{"status": "active"}, err)
}

func (h *Handler) run(c *gin.Context) {
	id, ok := parse(c)
	if !ok {
		return
	}
	var in struct {
		VirployeeID uuid.UUID       `json:"virployee_id"`
		TriggerRef  string          `json:"trigger_ref"`
		Event       json.RawMessage `json:"event"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	runID, err := h.service.Run(c, org, actor, role, id, in.VirployeeID, in.TriggerRef, in.Event)
	respond(c, http.StatusAccepted, gin.H{"run_id": runID}, err)
}

func (h *Handler) productEvent(c *gin.Context) {
	var in struct {
		EventID      string          `json:"event_id"`
		EventType    string          `json:"event_type"`
		EventVersion string          `json:"event_version"`
		Payload      json.RawMessage `json:"payload"`
		VirployeeID  uuid.UUID       `json:"virployee_id"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	productID := strings.TrimSpace(c.GetHeader("X-Axis-Product-ID"))
	rows, err := h.service.pool.Query(c, `SELECT id FROM companion_business_watchers WHERE org_id=$1 AND product_id=$2 AND lifecycle='active'`, org, productID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	defer rows.Close()
	var runIDs []uuid.UUID
	for rows.Next() {
		var watcherID uuid.UUID
		if err := rows.Scan(&watcherID); err != nil {
			ginmw.Respond(c, err)
			return
		}
		runID, runErr := h.service.Run(c, org, actor, role, watcherID, in.VirployeeID, in.EventID+":"+in.EventType+":"+in.EventVersion, in.Payload)
		if runErr == nil {
			runIDs = append(runIDs, runID)
		}
	}
	respond(c, http.StatusAccepted, gin.H{"run_ids": runIDs}, rows.Err())
}
