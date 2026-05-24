package memory

import (
	"context"
	"net/http"
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
)

type memoryUsecase interface {
	Upsert(ctx context.Context, in UpsertInput) (domain.MemoryEntry, error)
	Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error)
	Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error)
	Search(ctx context.Context, q SearchQuery) ([]SearchResult, error)
	Delete(ctx context.Context, id uuid.UUID) error
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
