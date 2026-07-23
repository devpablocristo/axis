package embeddings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeProvider struct{ tasks []string }

func (p *fakeProvider) Model() string   { return "fake-embedding" }
func (p *fakeProvider) Dimensions() int { return 3 }
func (p *fakeProvider) Embed(_ context.Context, request EmbeddingRequest) ([]float32, error) {
	p.tasks = append(p.tasks, request.TaskType)
	return make([]float32, p.Dimensions()), nil
}

func TestHandlerEmbedsEachDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeProvider{}
	router := gin.New()
	NewHandler(provider).Routes(router)
	req := httptest.NewRequest(http.MethodPost, "/embeddings", strings.NewReader(`{"texts":["one","two"],"task_type":"RETRIEVAL_DOCUMENT"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || len(provider.tasks) != 2 {
		t.Fatalf("status=%d tasks=%v body=%s", recorder.Code, provider.tasks, recorder.Body.String())
	}
}

func TestHandlerFailsClosedWithoutProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil).Routes(router)
	req := httptest.NewRequest(http.MethodPost, "/embeddings", strings.NewReader(`{"texts":["one"],"task_type":"RETRIEVAL_QUERY"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
