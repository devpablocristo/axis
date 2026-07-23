package promptgovernance

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
	r.GET("/prompts", h.listPrompts)
	r.POST("/prompts", h.createPrompt)
	r.POST("/prompts/:id/versions", h.createPromptVersion)
	r.POST("/prompt-versions/:id/simulate", h.simulate)
	r.POST("/prompt-versions/:id/evaluate", h.evaluatePrompt)
	r.GET("/evaluation-suites", h.listSuites)
	r.POST("/evaluation-suites", h.createSuite)
	r.POST("/evaluation-suites/:id/versions", h.createSuiteVersion)
	r.GET("/evaluation-runs", h.listRuns)
	r.POST("/evaluation-runs", h.createRun)
	r.GET("/evaluation-runs/:id", h.getRun)
	r.POST("/prompt-bindings/:target_type/:target_id/promote", h.promote)
	r.POST("/prompt-bindings/:target_type/:target_id/rollback", h.rollback)
	r.GET("/prompt-resolution", h.resolve)
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
		ginmw.Respond(c, domainerr.Validation("request body must contain one JSON object"))
		return false
	}
	return true
}

func pathID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param(name)))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation(name+" must be a UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func write(c *gin.Context, status int, value any, err error) {
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, status, value)
}

func (h *Handler) listPrompts(c *gin.Context) {
	org, actor, _ := trusted(c)
	out, err := h.service.ListPrompts(c, org, actor)
	write(c, http.StatusOK, gin.H{"items": out}, err)
}

func (h *Handler) createPrompt(c *gin.Context) {
	var in struct{ Name, Description string }
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.CreatePrompt(c, org, actor, role, in.Name, in.Description)
	write(c, http.StatusCreated, out, err)
}

func (h *Handler) createPromptVersion(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Content string `json:"content"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.CreatePromptVersion(c, org, actor, role, id, in.Content)
	write(c, http.StatusCreated, out, err)
}

func (h *Handler) simulate(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.Simulate(c, org, actor, role, id)
	write(c, http.StatusOK, out, err)
}

func (h *Handler) evaluatePrompt(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in struct {
		SuiteVersionID uuid.UUID `json:"suite_version_id"`
		ProductID      string    `json:"product_id"`
		SnapshotHash   string    `json:"snapshot_hash"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	var contentHash string
	if err := h.service.pool.QueryRow(c, `SELECT content_hash FROM companion_prompt_versions WHERE org_id=$1 AND id=$2`, org, id).Scan(&contentHash); err != nil {
		ginmw.Respond(c, mapNotFound(err, "prompt version not found"))
		return
	}
	out, err := h.service.RunEvaluation(c, org, actor, role, in.SuiteVersionID, "prompt_version", id.String(), contentHash, in.ProductID, in.SnapshotHash)
	write(c, http.StatusCreated, out, err)
}

func (h *Handler) listSuites(c *gin.Context) {
	org, actor, _ := trusted(c)
	out, err := h.service.ListSuites(c, org, actor)
	write(c, http.StatusOK, gin.H{"items": out}, err)
}

func (h *Handler) createSuite(c *gin.Context) {
	var in struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		ArtifactType string `json:"artifact_type"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.CreateSuite(c, org, actor, role, in.Name, in.Description, in.ArtifactType)
	write(c, http.StatusCreated, out, err)
}

func (h *Handler) createSuiteVersion(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Dataset    json.RawMessage `json:"dataset"`
		Thresholds json.RawMessage `json:"thresholds"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.CreateSuiteVersion(c, org, actor, role, id, in.Dataset, in.Thresholds)
	write(c, http.StatusCreated, out, err)
}

func (h *Handler) createRun(c *gin.Context) {
	var in struct {
		SuiteVersionID uuid.UUID `json:"suite_version_id"`
		ArtifactType   string    `json:"artifact_type"`
		ArtifactRef    string    `json:"artifact_ref"`
		ArtifactHash   string    `json:"artifact_hash"`
		ProductID      string    `json:"product_id"`
		SnapshotHash   string    `json:"snapshot_hash"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.RunEvaluation(c, org, actor, role, in.SuiteVersionID, in.ArtifactType, in.ArtifactRef, in.ArtifactHash, in.ProductID, in.SnapshotHash)
	write(c, http.StatusCreated, out, err)
}

func (h *Handler) listRuns(c *gin.Context) {
	org, actor, _ := trusted(c)
	out, err := h.service.ListEvaluationRuns(c, org, actor)
	write(c, http.StatusOK, gin.H{"items": out}, err)
}

func (h *Handler) getRun(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	org, actor, _ := trusted(c)
	out, err := h.service.GetEvaluationRun(c, org, actor, id)
	write(c, http.StatusOK, out, err)
}

func (h *Handler) promote(c *gin.Context)  { h.changeBinding(c, "promote") }
func (h *Handler) rollback(c *gin.Context) { h.changeBinding(c, "rollback") }

func (h *Handler) changeBinding(c *gin.Context, action string) {
	targetID, ok := pathID(c, "target_id")
	if !ok {
		return
	}
	var in struct {
		PromptVersionID   uuid.UUID `json:"prompt_version_id"`
		EvaluationRunID   uuid.UUID `json:"evaluation_run_id"`
		ProductID         string    `json:"product_id"`
		AuthorizationHash string    `json:"authorization_hash"`
	}
	if !decode(c, &in) {
		return
	}
	org, actor, role := trusted(c)
	out, err := h.service.Promote(c, org, actor, role, c.Param("target_type"), targetID, in.PromptVersionID, in.EvaluationRunID, in.ProductID, in.AuthorizationHash, action)
	write(c, http.StatusOK, out, err)
}

func (h *Handler) resolve(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Query("virployee_id")))
	if err != nil {
		ginmw.Respond(c, domainerr.Validation("virployee_id must be a UUID"))
		return
	}
	org, actor, _ := trusted(c)
	out, err := h.service.Resolve(c, org, actor, id, c.Query("product_id"), c.Query("include_content") == "true")
	write(c, http.StatusOK, out, err)
}
