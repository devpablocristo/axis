package embeddings

import (
	"net/http"
	"strings"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

const maxTextsPerRequest = 64

type Handler struct{ provider Provider }

func NewHandler(provider Provider) *Handler { return &Handler{provider: provider} }

func (h *Handler) Routes(router gin.IRouter) { router.POST("/embeddings", h.Embed) }

type Request struct {
	Texts    []string `json:"texts"`
	TaskType string   `json:"task_type"`
}

type Response struct {
	Model      string      `json:"model"`
	Dimensions int         `json:"dimensions"`
	Embeddings [][]float32 `json:"embeddings"`
}

func (h *Handler) Embed(c *gin.Context) {
	if h.provider == nil {
		ginmw.WriteError(c, http.StatusServiceUnavailable, "embedding_unavailable", "embedding provider is not configured")
		return
	}
	var request Request
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Texts) == 0 || len(request.Texts) > maxTextsPerRequest {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "texts must contain between 1 and 64 items")
		return
	}
	if request.TaskType != TaskDocument && request.TaskType != TaskQuery {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "unsupported embedding task type")
		return
	}
	response := Response{Model: h.provider.Model(), Dimensions: h.provider.Dimensions(), Embeddings: make([][]float32, 0, len(request.Texts))}
	for _, value := range request.Texts {
		if strings.TrimSpace(value) == "" {
			ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "embedding text cannot be empty")
			return
		}
		embedding, err := h.provider.Embed(c.Request.Context(), value, request.TaskType)
		if err != nil {
			ginmw.WriteError(c, http.StatusBadGateway, "embedding_failed", "embedding provider failed")
			return
		}
		response.Embeddings = append(response.Embeddings, embedding)
	}
	c.JSON(http.StatusOK, response)
}
