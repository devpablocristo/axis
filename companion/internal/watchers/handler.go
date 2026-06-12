package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/productlimits"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/watchers/handler/dto"
	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeCompanionWatchersRead    = "companion:watchers:read"
	scopeCompanionWatchersWrite   = "companion:watchers:write"
	scopeCompanionWatchersExecute = "companion:watchers:execute"
	scopeCompanionCrossOrg        = "companion:cross_org"
)

// watcherUsecase port que el handler consume.
type watcherUsecase interface {
	Create(ctx context.Context, input CreateWatcherInput) (domain.Watcher, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Watcher, error)
	List(ctx context.Context, orgID string) ([]domain.Watcher, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateWatcherInput) (domain.Watcher, error)
	Delete(ctx context.Context, id uuid.UUID) error
	RunWatcher(ctx context.Context, watcherID uuid.UUID) (*domain.WatcherResult, error)
	ListProposals(ctx context.Context, watcherID uuid.UUID, limit int) ([]domain.Proposal, error)
}

// Handler es el handler HTTP del módulo watchers.
type Handler struct {
	uc watcherUsecase
}

// NewHandler crea un handler de watchers.
func NewHandler(uc watcherUsecase) *Handler {
	return &Handler{uc: uc}
}

// Register registra las rutas en un http.ServeMux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/watchers", h.create)
	mux.HandleFunc("GET /v1/watchers", h.list)
	mux.HandleFunc("GET /v1/watchers/{id}", h.getByID)
	mux.HandleFunc("PATCH /v1/watchers/{id}", h.update)
	mux.HandleFunc("DELETE /v1/watchers/{id}", h.remove)
	mux.HandleFunc("POST /v1/watchers/{id}/run", h.run)
	mux.HandleFunc("GET /v1/watchers/{id}/proposals", h.listProposals)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionWatchersWrite) {
		return
	}
	var req dto.CreateWatcherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	orgID, ok := effectiveWatcherOrgID(r, req.OrgID)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", "watcher org is not allowed for this principal")
		return
	}

	result, err := h.uc.Create(r.Context(), CreateWatcherInput{
		OrgID:       orgID,
		Name:        req.Name,
		WatcherType: domain.WatcherType(req.WatcherType),
		Config:      req.Config,
		Enabled:     req.Enabled,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not create watcher")
		return
	}

	httpjson.WriteJSON(w, http.StatusCreated, dto.WatcherToResponse(result))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionWatchersRead) {
		return
	}
	orgID, ok := effectiveWatcherOrgID(r, r.URL.Query().Get("org_id"))
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", "watcher org is not allowed for this principal")
		return
	}
	if orgID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "missing_org_id", "org_id query parameter required")
		return
	}

	watchers, err := h.uc.List(r.Context(), orgID)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not list watchers")
		return
	}

	items := make([]dto.WatcherResponse, 0, len(watchers))
	for _, w := range watchers {
		items = append(items, dto.WatcherToResponse(w))
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.WatcherListResponse{Watchers: items})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionWatchersRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "invalid_id", "invalid watcher id")
		return
	}

	watcher, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "not_found", "watcher not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not get watcher")
		return
	}
	if !canAccessWatcherOrg(r, watcher) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", "watcher org is not allowed for this principal")
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, dto.WatcherToResponse(watcher))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionWatchersWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "invalid_id", "invalid watcher id")
		return
	}

	var req dto.UpdateWatcherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	watcher, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "not_found", "watcher not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not get watcher")
		return
	}
	if !canAccessWatcherOrg(r, watcher) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", "watcher org is not allowed for this principal")
		return
	}

	input := UpdateWatcherInput{
		Name:    req.Name,
		Enabled: req.Enabled,
	}
	if req.Config != nil {
		input.Config = req.Config
	}

	result, err := h.uc.Update(r.Context(), id, input)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "not_found", "watcher not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not update watcher")
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, dto.WatcherToResponse(result))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionWatchersWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "invalid_id", "invalid watcher id")
		return
	}
	watcher, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "not_found", "watcher not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not get watcher")
		return
	}
	if !canAccessWatcherOrg(r, watcher) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", "watcher org is not allowed for this principal")
		return
	}

	if err := h.uc.Delete(r.Context(), id); err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "not_found", "watcher not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not delete watcher")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionWatchersExecute) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "invalid_id", "invalid watcher id")
		return
	}
	watcher, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "not_found", "watcher not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not get watcher")
		return
	}
	if !canAccessWatcherOrg(r, watcher) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", "watcher org is not allowed for this principal")
		return
	}

	result, err := h.uc.RunWatcher(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "not_found", "watcher not found")
			return
		}
		if errors.Is(err, ErrWatcherDisabled) {
			httpjson.WriteFlatError(w, http.StatusConflict, "watcher_disabled", "watcher is disabled")
			return
		}
		if domainerr.IsValidation(err) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "validation", err.Error())
			return
		}
		if domainerr.IsForbidden(err) {
			httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		if productlimits.IsRateLimited(err) {
			httpjson.WriteFlatError(w, http.StatusTooManyRequests, "rate_limited", err.Error())
			return
		}
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not run watcher")
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, dto.RunResultResponse{
		Found:    result.Found,
		Proposed: result.Proposed,
		Executed: result.Executed,
	})
}

func (h *Handler) listProposals(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionWatchersRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "invalid_id", "invalid watcher id")
		return
	}
	watcher, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "not_found", "watcher not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not get watcher")
		return
	}
	if !canAccessWatcherOrg(r, watcher) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", "watcher org is not allowed for this principal")
		return
	}

	proposals, err := h.uc.ListProposals(r.Context(), id, 50)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusInternalServerError, "internal_error", "could not list proposals")
		return
	}

	items := make([]dto.ProposalResponse, 0, len(proposals))
	for _, p := range proposals {
		items = append(items, dto.ProposalToResponse(p))
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.ProposalListResponse{Proposals: items})
}

func effectiveWatcherOrgID(r *http.Request, requested string) (string, bool) {
	return identityctx.EffectiveOrgID(r, requested, scopeCompanionCrossOrg)
}

func canAccessWatcherOrg(r *http.Request, watcher domain.Watcher) bool {
	if identityctx.HasNoAuthContext(r) {
		return true
	}
	return identityctx.CanAccessOrg(r, watcher.OrgID, scopeCompanionCrossOrg)
}

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "forbidden", "missing required scope")
	return false
}
