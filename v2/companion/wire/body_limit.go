package wire

import (
	"net/http"
	"strings"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

func routeAwareBodySizeLimit(defaultMax, knowledgeUploadMax int64) gin.HandlerFunc {
	defaultLimit := ginmw.NewBodySizeLimit(defaultMax)
	uploadLimit := ginmw.NewBodySizeLimit(knowledgeUploadMax)
	return func(c *gin.Context) {
		if isKnowledgeUploadRequest(c.Request.Method, c.Request.URL.Path) {
			uploadLimit(c)
			return
		}
		defaultLimit(c)
	}
}

func isKnowledgeUploadRequest(method, requestPath string) bool {
	if method != http.MethodPost {
		return false
	}
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	return len(parts) == 5 && parts[0] == "v1" && parts[1] == "knowledge-bases" &&
		parts[2] != "" && parts[3] == "ingestions" && parts[4] == "upload"
}
