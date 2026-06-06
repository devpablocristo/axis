package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/memory/handler/dto"
	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

const (
	defaultListLimit          = 50
	scopeCompanionMemoryRead  = "companion:memory:read"
	scopeCompanionMemoryWrite = "companion:memory:write"
	scopeCompanionMemoryAdmin = "companion:memory:admin"
)

type memoryUsecase interface {
	Upsert(ctx context.Context, in UpsertInput) (domain.MemoryEntry, error)
	Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error)
	Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error)
	Search(ctx context.Context, q SearchQuery) ([]SearchResult, error)
	Delete(ctx context.Context, id uuid.UUID) error
	ListConflicts(ctx context.Context, orgID, productSurface string, limit int) ([]domain.MemoryEntry, error)
	CreateReview(ctx context.Context, in CreateReviewInput) (MemoryReview, error)
	ListReviews(ctx context.Context, orgID, productSurface, status string, limit int) ([]MemoryReview, error)
	UpdateReviewStatus(ctx context.Context, orgID, productSurface string, reviewID uuid.UUID, status, decidedBy string) (MemoryReview, error)
	ApplyReview(ctx context.Context, orgID, productSurface string, reviewID uuid.UUID, decidedBy string) (MemoryReview, error)
	ListAudit(ctx context.Context, orgID, productSurface string, limit int) ([]MemoryAuditEntry, error)
	ListSummaries(ctx context.Context, orgID, productSurface string, limit int) ([]MemorySummary, error)
	ExportByOrg(ctx context.Context, orgID, productSurface string, limit int) ([]domain.MemoryEntry, error)
	DeleteByOrg(ctx context.Context, orgID, productSurface string) (int64, error)
}

// TaskOrgGetter resuelve el org_id de una task para que el handler pueda
// validar memorias scope=task contra el principal. ErrTaskNotFound debe
// devolverse si la task no existe.
type TaskOrgGetter interface {
	GetTaskOrg(ctx context.Context, taskID uuid.UUID) (string, error)
}

// Handler HTTP adapter para memoria operativa.
type Handler struct {
	uc       memoryUsecase
	taskOrgs TaskOrgGetter
}

// NewHandler crea un nuevo handler de memoria. taskOrgs puede ser nil; en
// ese caso las memorias scope=task quedan rechazadas por defecto cuando
// hay X-Org-ID (fail-closed).
func NewHandler(uc memoryUsecase, taskOrgs TaskOrgGetter) *Handler {
	return &Handler{uc: uc, taskOrgs: taskOrgs}
}

// Register registra las rutas de memoria en el mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/memory", h.upsert)
	mux.HandleFunc("GET /v1/memory/{id}", h.get)
	mux.HandleFunc("GET /v1/memory", h.find)
	mux.HandleFunc("GET /v1/memory/search", h.search)
	mux.HandleFunc("GET /v1/memory/conflicts", h.conflicts)
	mux.HandleFunc("GET /v1/memory/reviews", h.reviews)
	mux.HandleFunc("GET /v1/memory/audit", h.audit)
	mux.HandleFunc("GET /v1/memory/summaries", h.summaries)
	mux.HandleFunc("GET /v1/memory/export", h.exportOrg)
	mux.HandleFunc("DELETE /v1/memory/org", h.deleteOrg)
	mux.HandleFunc("POST /v1/memory/{id}/reviews", h.createReview)
	mux.HandleFunc("POST /v1/memory/reviews/{review_id}/approve", h.approveReview)
	mux.HandleFunc("POST /v1/memory/reviews/{review_id}/reject", h.rejectReview)
	mux.HandleFunc("POST /v1/memory/reviews/{review_id}/apply", h.applyReview)
	mux.HandleFunc("DELETE /v1/memory/{id}", h.delete)
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryWrite) {
		return
	}
	var body dto.UpsertMemoryRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Kind == "" || body.ScopeType == "" || body.ScopeID == "" || body.Key == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "kind, scope_type, scope_id, and key are required")
		return
	}
	if !h.authorizeMemoryScope(r, domain.ScopeType(body.ScopeType), body.ScopeID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}
	orgID, userID, productSurface := h.memoryContext(r, domain.ScopeType(body.ScopeType), body.ScopeID)

	entry, err := h.uc.Upsert(r.Context(), UpsertInput{
		OrgID:           orgID,
		UserID:          userID,
		ProductSurface:  productSurface,
		Kind:            domain.MemoryKind(body.Kind),
		MemoryType:      domain.MemoryType(body.MemoryType),
		Classification:  domain.MemoryClass(body.Classification),
		ScopeType:       domain.ScopeType(body.ScopeType),
		ScopeID:         body.ScopeID,
		Key:             body.Key,
		PayloadJSON:     body.PayloadJSON,
		ContentText:     body.ContentText,
		ProvenanceJSON:  body.ProvenanceJSON,
		Confidence:      body.Confidence,
		RetentionPolicy: body.RetentionPolicy,
		Source:          body.Source,
		Supersede:       body.Supersede,
		Version:         body.Version,
		TTLDays:         body.TTLDays,
	})
	if err != nil {
		if IsVersionConflict(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "VERSION_CONFLICT", "memory entry was modified by another process")
			return
		}
		if IsQuotaExceeded(err) {
			httpjson.WriteFlatError(w, http.StatusTooManyRequests, "QUOTA_EXCEEDED", "memory quota exceeded for scope")
			return
		}
		if IsMemoryConflict(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "MEMORY_CONFLICT", "memory conflict requires review or supersession")
			return
		}
		if IsMemoryPoisoning(err) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "MEMORY_POISONING", "memory input rejected by poisoning detector")
			return
		}
		if IsValidation(err) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		if IsForbidden(err) {
			httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		httpjson.WriteFlatInternalError(w, err, "upsert memory failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.EntryToResponse(entry))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	entry, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory entry not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get memory failed")
		return
	}
	if !h.authorizeMemoryEntry(r, entry) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.EntryToResponse(entry))
}

func (h *Handler) find(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryRead) {
		return
	}
	q := r.URL.Query()
	scopeType := q.Get("scope_type")
	scopeID := q.Get("scope_id")
	if scopeType == "" || scopeID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "scope_type and scope_id are required")
		return
	}
	if !h.authorizeMemoryScope(r, domain.ScopeType(scopeType), scopeID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}

	entries, err := h.uc.Find(r.Context(), FindQuery{
		OrgID:          principalOrgID(r),
		UserID:         principalUserID(r),
		ProductSurface: productSurface(r),
		ScopeType:      domain.ScopeType(scopeType),
		ScopeID:        scopeID,
		Kind:           domain.MemoryKind(q.Get("kind")),
		MemoryType:     domain.MemoryType(q.Get("memory_type")),
		Limit:          defaultListLimit,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "find memory failed")
		return
	}

	out := make([]dto.MemoryResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, dto.EntryToResponse(e))
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.MemoryListResponse{Entries: out})
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryRead) {
		return
	}
	q := r.URL.Query()
	scopeType := q.Get("scope_type")
	scopeID := q.Get("scope_id")
	query := strings.TrimSpace(q.Get("q"))
	if scopeType == "" || scopeID == "" || query == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "q, scope_type and scope_id are required")
		return
	}
	if !h.authorizeMemoryScope(r, domain.ScopeType(scopeType), scopeID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}
	results, err := h.uc.Search(r.Context(), SearchQuery{
		FindQuery: FindQuery{
			OrgID:          principalOrgID(r),
			UserID:         principalUserID(r),
			ProductSurface: productSurface(r),
			ScopeType:      domain.ScopeType(scopeType),
			ScopeID:        scopeID,
			Kind:           domain.MemoryKind(q.Get("kind")),
			MemoryType:     domain.MemoryType(q.Get("memory_type")),
			Limit:          defaultListLimit,
		},
		Query: query,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "search memory failed")
		return
	}
	out := make([]dto.MemorySearchResult, 0, len(results))
	for _, result := range results {
		out = append(out, dto.SearchResultToResponse(result.Entry, result.Score, result.Reasons))
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.MemorySearchResponse{Results: out})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	entry, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory entry not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get memory failed")
		return
	}
	if !h.authorizeMemoryEntry(r, entry) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}
	if err := h.uc.Delete(r.Context(), id); err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory entry not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "delete memory failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) conflicts(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	entries, err := h.uc.ListConflicts(r.Context(), principalOrgID(r), productSurface(r), memoryLimit(r, defaultListLimit))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list memory conflicts failed")
		return
	}
	out := make([]dto.MemoryResponse, 0, len(entries))
	for _, entry := range entries {
		out = append(out, dto.EntryToResponse(entry))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"conflicts": out})
}

func (h *Handler) createReview(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || id == uuid.Nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	entry, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory entry not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get memory failed")
		return
	}
	if !h.authorizeMemoryEntry(r, entry) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}
	var body struct {
		ReviewType      string          `json:"review_type"`
		Reason          string          `json:"reason"`
		ProposedContent string          `json:"proposed_content"`
		ProposedPayload json.RawMessage `json:"proposed_payload"`
	}
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	review, err := h.uc.CreateReview(r.Context(), CreateReviewInput{
		OrgID:           principalOrgID(r),
		ProductSurface:  productSurface(r),
		MemoryID:        id,
		ReviewType:      strings.TrimSpace(body.ReviewType),
		Reason:          strings.TrimSpace(body.Reason),
		ProposedContent: body.ProposedContent,
		ProposedPayload: body.ProposedPayload,
		CreatedBy:       principalUserID(r),
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, review)
}

func (h *Handler) reviews(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	reviews, err := h.uc.ListReviews(r.Context(), principalOrgID(r), productSurface(r), strings.TrimSpace(r.URL.Query().Get("status")), memoryLimit(r, defaultListLimit))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list memory reviews failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"reviews": reviews})
}

func (h *Handler) approveReview(w http.ResponseWriter, r *http.Request) {
	h.updateReview(w, r, "approved")
}

func (h *Handler) rejectReview(w http.ResponseWriter, r *http.Request) {
	h.updateReview(w, r, "rejected")
}

func (h *Handler) updateReview(w http.ResponseWriter, r *http.Request, status string) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("review_id"))
	if err != nil || id == uuid.Nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid review_id")
		return
	}
	review, err := h.uc.UpdateReviewStatus(r.Context(), principalOrgID(r), productSurface(r), id, status, principalUserID(r))
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory review not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, review)
}

func (h *Handler) applyReview(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("review_id"))
	if err != nil || id == uuid.Nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid review_id")
		return
	}
	review, err := h.uc.ApplyReview(r.Context(), principalOrgID(r), productSurface(r), id, principalUserID(r))
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory review not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, review)
}

func (h *Handler) audit(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	entries, err := h.uc.ListAudit(r.Context(), principalOrgID(r), productSurface(r), memoryLimit(r, defaultListLimit))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list memory audit failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *Handler) summaries(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	summaries, err := h.uc.ListSummaries(r.Context(), principalOrgID(r), productSurface(r), memoryLimit(r, defaultListLimit))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list memory summaries failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"summaries": summaries})
}

func (h *Handler) exportOrg(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	entries, err := h.uc.ExportByOrg(r.Context(), principalOrgID(r), productSurface(r), memoryLimit(r, 1000))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "export memory failed")
		return
	}
	out := make([]dto.MemoryResponse, 0, len(entries))
	for _, entry := range entries {
		out = append(out, dto.EntryToResponse(entry))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func (h *Handler) deleteOrg(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionMemoryAdmin) {
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("confirm")) != "true" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "confirm=true is required")
		return
	}
	deleted, err := h.uc.DeleteByOrg(r.Context(), principalOrgID(r), productSurface(r))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "delete memory by org failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func (h *Handler) authorizeMemoryScope(r *http.Request, scopeType domain.ScopeType, scopeID string) bool {
	orgID := principalOrgID(r)
	if orgID == "" {
		return false
	}

	scopeID = strings.TrimSpace(scopeID)
	switch scopeType {
	case domain.ScopeOrg:
		return scopeID == orgID
	case domain.ScopeUser:
		userID := principalUserID(r)
		return userID != "" && scopeID == tenantUserMemoryScopeID(orgID, userID)
	case domain.ScopeTask:
		// Antes esto retornaba true: cualquier principal podía leer/escribir
		// memoria de cualquier task. Ahora resolvemos el org de la task vía
		// taskOrgs y exigimos coincidencia. Sin getter inyectado o task sin
		// org → fail-closed.
		if h.taskOrgs == nil {
			return false
		}
		taskID, err := uuid.Parse(scopeID)
		if err != nil {
			return false
		}
		taskOrg, err := h.taskOrgs.GetTaskOrg(r.Context(), taskID)
		if err != nil {
			return false
		}
		return strings.TrimSpace(taskOrg) == orgID
	default:
		return false
	}
}

func (h *Handler) authorizeMemoryEntry(r *http.Request, entry domain.MemoryEntry) bool {
	if strings.TrimSpace(entry.ProductSurface) != productSurface(r) {
		return false
	}
	return h.authorizeMemoryScope(r, entry.ScopeType, entry.ScopeID)
}

func (h *Handler) memoryContext(r *http.Request, scopeType domain.ScopeType, scopeID string) (string, string, string) {
	orgID := principalOrgID(r)
	userID := principalUserID(r)
	if scopeType == domain.ScopeTask && h.taskOrgs != nil {
		if taskID, err := uuid.Parse(strings.TrimSpace(scopeID)); err == nil {
			if taskOrg, err := h.taskOrgs.GetTaskOrg(r.Context(), taskID); err == nil {
				orgID = strings.TrimSpace(taskOrg)
			}
		}
	}
	return orgID, userID, productSurface(r)
}

func principalOrgID(r *http.Request) string {
	return identityctx.PrincipalOrgID(r)
}

func principalUserID(r *http.Request) string {
	return identityctx.PrincipalUserID(r)
}

func productSurface(r *http.Request) string {
	return identityctx.ProductSurface(r)
}

func tenantUserMemoryScopeID(orgID, userID string) string {
	return strings.TrimSpace(orgID) + ":" + strings.TrimSpace(userID)
}

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}

func memoryLimit(r *http.Request, fallback int) int {
	limit := fallback
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 {
			limit = parsed
		}
	}
	return limit
}
