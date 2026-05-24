package jobs

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeJobsAdmin         = "companion:runtime:admin"
	scopeJobsCrossOrg      = "companion:cross_org"
	defaultRecoverJobLimit = 100
)

type HTTPHandler struct {
	repo Repository
}

func NewHandler(repo Repository) *HTTPHandler {
	return &HTTPHandler{repo: repo}
}

func (h *HTTPHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/jobs/{id}", h.get)
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", h.cancel)
	mux.HandleFunc("POST /v1/jobs/recover-expired", h.recoverExpired)
}

func (h *HTTPHandler) get(w http.ResponseWriter, r *http.Request) {
	if !requireJobsAdmin(w, r) {
		return
	}
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	job, err := h.repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "job not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get job failed")
		return
	}
	if !canAccessJobOrg(r, job.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "job org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, job)
}

func (h *HTTPHandler) cancel(w http.ResponseWriter, r *http.Request) {
	if !requireJobsAdmin(w, r) {
		return
	}
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	job, err := h.repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "job not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get job failed")
		return
	}
	if !canAccessJobOrg(r, job.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "job org is not allowed for this principal")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
			return
		}
	}
	if err := h.repo.Cancel(r.Context(), id, strings.TrimSpace(req.Reason)); err != nil {
		if errors.Is(err, ErrJobNotFound) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "job not found or not cancellable")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "cancel job failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}

func (h *HTTPHandler) recoverExpired(w http.ResponseWriter, r *http.Request) {
	if !requireJobsAdmin(w, r) {
		return
	}
	limit := defaultRecoverJobLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	count, err := h.repo.RecoverExpiredLeases(r.Context(), limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "recover expired jobs failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"recovered": count})
}

func parseJobID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil || id == uuid.Nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid job id")
		return uuid.Nil, false
	}
	return id, true
}

func requireJobsAdmin(w http.ResponseWriter, r *http.Request) bool {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "jobs endpoints require authenticated admin context")
		return false
	}
	if !identityctx.HasAnyScope(r, scopeJobsAdmin, scopeJobsCrossOrg) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing jobs admin scope")
		return false
	}
	return true
}

func canAccessJobOrg(r *http.Request, orgID string) bool {
	if strings.TrimSpace(orgID) == "" {
		return false
	}
	return identityctx.CanAccessOrg(r, orgID, scopeJobsCrossOrg)
}
