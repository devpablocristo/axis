package knowledgebases

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct{ u *UseCases }

func NewHandler(u *UseCases) *Handler { return &Handler{u: u} }

func (h *Handler) Routes(r gin.IRouter) {
	g := r.Group("/knowledge-bases")
	g.GET("", h.List)
	g.POST("", h.Create)
	g.GET("/:knowledge_base_id", h.Get)
	g.PUT("/:knowledge_base_id", h.Update)
	g.POST("/:knowledge_base_id/archive", func(c *gin.Context) { h.lifecycle(c, "archive") })
	g.POST("/:knowledge_base_id/activate", func(c *gin.Context) { h.lifecycle(c, "activate") })
	g.POST("/:knowledge_base_id/ingestions/connector", h.IngestConnector)
	g.POST("/:knowledge_base_id/ingestions/upload", h.IngestUpload)
	g.GET("/:knowledge_base_id/documents", h.ListDocuments)
	g.POST("/:knowledge_base_id/documents", h.RegisterDocument)
	g.POST("/:knowledge_base_id/documents/:document_id/archive", h.ArchiveDocument)
	g.GET("/:knowledge_base_id/bindings", h.ListBindings)
	g.PUT("/:knowledge_base_id/bindings", h.ReplaceBindings)
	r.GET("/virployees/:virployee_id/knowledge-bases", h.ListForVirployee)
	r.PUT("/virployees/:virployee_id/knowledge-bases", h.SetForVirployee)
}

func (h *Handler) IngestConnector(c *gin.Context) {
	baseID, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	tenant, role := auth(c)
	if _, err := authorize(tenant, role); err != nil {
		ginmw.Respond(c, err)
		return
	}
	var in ConnectorIngestionInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	out, err := h.u.IngestConnector(c, tenant, role, baseID, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, out)
}

func (h *Handler) IngestUpload(c *gin.Context) {
	baseID, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	tenant, role := auth(c)
	if _, err := authorize(tenant, role); err != nil {
		ginmw.Respond(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, artifacts.MaxArtifactBytes+(1<<20))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		writeUploadInputError(c, err)
		return
	}
	values := map[string]string{}
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			ginmw.Respond(c, ginmw.ErrBadInput)
			return
		}
		if nextErr != nil {
			writeUploadInputError(c, nextErr)
			return
		}
		name := part.FormName()
		if name == "file" {
			if part.FileName() == "" {
				_ = part.Close()
				ginmw.Respond(c, ginmw.ErrBadInput)
				return
			}
			// Metadata parts are deliberately required before the file. Opening a
			// multipart part reads only its headers; UseCases performs the tenant,
			// Virployee, subject and classification preflight before the pipeline
			// starts consuming bytes from this reader.
			out, ingestErr := h.u.IngestUpload(c, tenant, role, baseID, UploadIngestionInput{
				Title: values["title"],
				Target: IngestionTargetInput{
					VirployeeID: values["virployee_id"], SubjectID: values["subject_id"], DocumentID: values["document_id"],
				},
				Name: part.FileName(), ContentType: part.Header.Get("Content-Type"), Content: part,
			})
			if ingestErr != nil {
				// multipart.Part.Close drains the unread part. On a failed metadata/
				// authorization preflight that would unnecessarily consume a large
				// upload; close the request body instead and stop reading immediately.
				_ = c.Request.Body.Close()
				if errors.Is(ingestErr, artifacts.ErrArtifactTooLarge) {
					ginmw.WriteError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "uploaded file exceeds the allowed size")
					return
				}
				ginmw.Respond(c, ingestErr)
				return
			}
			_ = part.Close()
			ginmw.WriteCreated(c, out)
			return
		}
		if part.FileName() != "" || !allowedUploadMetadata(name) {
			_ = part.Close()
			ginmw.Respond(c, ginmw.ErrBadInput)
			return
		}
		if _, duplicate := values[name]; duplicate {
			_ = part.Close()
			ginmw.Respond(c, ginmw.ErrBadInput)
			return
		}
		value, readErr := readUploadMetadata(part)
		_ = part.Close()
		if readErr != nil {
			writeUploadInputError(c, readErr)
			return
		}
		values[name] = value
	}
}

const maxUploadMetadataBytes = 8 << 10

func allowedUploadMetadata(name string) bool {
	switch name {
	case "title", "virployee_id", "subject_id", "document_id":
		return true
	default:
		return false
	}
}

func readUploadMetadata(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxUploadMetadataBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxUploadMetadataBytes {
		return "", artifacts.ErrArtifactTooLarge
	}
	return string(data), nil
}

func writeUploadInputError(c *gin.Context, err error) {
	if ginmw.IsBodyTooLarge(err) || errors.Is(err, artifacts.ErrArtifactTooLarge) {
		ginmw.WriteError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "upload request exceeds the allowed size")
		return
	}
	ginmw.Respond(c, ginmw.ErrBadInput)
}

func (h *Handler) ListForVirployee(c *gin.Context) {
	virployeeID, ok := pathID(c, "virployee_id")
	if !ok {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.ListForVirployee(c, tenant, role, virployeeID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	// Preview is intentionally resolved server-side. The console never receives
	// private bindings for sibling subjects and then relies on React to hide them.
	if c.Query("context_preview") == "1" {
		subjectID := strings.TrimSpace(c.Query("subject_id"))
		caseID := strings.TrimSpace(c.Query("case_id"))
		filtered := make([]VirployeeKnowledgeBase, 0, len(out))
		for _, entry := range out {
			if entry.KnowledgeBase.Classification == ClassificationProfessional {
				filtered = append(filtered, entry)
				continue
			}
			if subjectID == "" {
				continue
			}
			bindings := make([]Binding, 0, len(entry.Bindings))
			for _, binding := range entry.Bindings {
				if binding.SubjectID != subjectID {
					continue
				}
				if binding.ScopeType == ScopeSubject || (binding.ScopeType == ScopeCase && caseID != "" && binding.CaseID != nil && binding.CaseID.String() == caseID) {
					bindings = append(bindings, binding)
				}
			}
			if len(bindings) > 0 {
				entry.Bindings = bindings
				filtered = append(filtered, entry)
			}
		}
		out = filtered
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": out})
}

func (h *Handler) SetForVirployee(c *gin.Context) {
	virployeeID, ok := pathID(c, "virployee_id")
	if !ok {
		return
	}
	var in SetVirployeeKnowledgeBaseInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.SetForVirployee(c, tenant, role, virployeeID, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": out})
}

func auth(c *gin.Context) (string, string) {
	return strings.TrimSpace(c.GetHeader("X-Tenant-ID")), strings.TrimSpace(c.GetHeader("X-Axis-Tenant-Role"))
}

func pathID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil || id == uuid.Nil {
		ginmw.Respond(c, ginmw.ErrBadInput)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) Create(c *gin.Context) {
	var in CreateInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.Create(c, tenant, role, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, out)
}

func (h *Handler) List(c *gin.Context) {
	tenant, role := auth(c)
	out, err := h.u.List(c, tenant, role, c.Query("state"))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": out})
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.Get(c, tenant, role, id)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	var in UpdateInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.Update(c, tenant, role, id, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

type versionRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

func (h *Handler) lifecycle(c *gin.Context, action string) {
	id, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	var in versionRequest
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.Lifecycle(c, tenant, role, id, action, in.ExpectedVersion)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) RegisterDocument(c *gin.Context) {
	baseID, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	var in RegisterDocumentInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.RegisterDocument(c, tenant, role, baseID, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteCreated(c, out)
}

func (h *Handler) ListDocuments(c *gin.Context) {
	baseID, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.ListDocuments(c, tenant, role, baseID, c.Query("state"))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": out})
}

func (h *Handler) ArchiveDocument(c *gin.Context) {
	baseID, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	documentID, ok := pathID(c, "document_id")
	if !ok {
		return
	}
	var in versionRequest
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.ArchiveDocument(c, tenant, role, baseID, documentID, in.ExpectedVersion)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) ListBindings(c *gin.Context) {
	baseID, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.ListBindings(c, tenant, role, baseID)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": out})
}

func (h *Handler) ReplaceBindings(c *gin.Context) {
	baseID, ok := pathID(c, "knowledge_base_id")
	if !ok {
		return
	}
	var in ReplaceBindingsInput
	if err := ginmw.BindJSON(c, &in); err != nil {
		return
	}
	tenant, role := auth(c)
	out, err := h.u.ReplaceBindings(c, tenant, role, baseID, in)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, gin.H{"data": out})
}
