package assist

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	assistdto "github.com/devpablocristo/companion/internal/assist/handler/dto"
	domain "github.com/devpablocristo/companion/internal/assist/usecases/domain"
	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

type assistUsecase interface {
	UpsertPack(ctx context.Context, in UpsertPackInput) (domain.AssistPack, error)
	GetPack(ctx context.Context, id uuid.UUID) (domain.AssistPack, error)
	ListPacks(ctx context.Context, in ListPacksInput) ([]domain.AssistPack, error)
	UpdatePack(ctx context.Context, in UpdatePackInput) (domain.AssistPack, error)
	ArchivePack(ctx context.Context, orgID, actor string, id uuid.UUID) error
	RestorePack(ctx context.Context, orgID, actor string, id uuid.UUID) error
	HardDeletePack(ctx context.Context, orgID, actor string, id uuid.UUID) error
	RunAssist(ctx context.Context, in RunAssistInput) (domain.AssistRun, error)
	GetRun(ctx context.Context, id uuid.UUID) (domain.AssistRun, error)
	ListRuns(ctx context.Context, in ListRunsInput) ([]domain.AssistRun, error)
}

type Handler struct {
	uc assistUsecase
}

func NewHandler(uc assistUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/assist-packs", h.upsertPack)
	mux.HandleFunc("GET /v1/assist-packs", h.listPacks)
	mux.HandleFunc("GET /v1/assist-packs/archived", h.listArchivedPacks)
	mux.HandleFunc("GET /v1/assist-packs/{id}", h.getPack)
	mux.HandleFunc("PATCH /v1/assist-packs/{id}", h.updatePack)
	mux.HandleFunc("POST /v1/assist-packs/{id}/archive", h.archivePack)
	mux.HandleFunc("POST /v1/assist-packs/{id}/restore", h.restorePack)
	mux.HandleFunc("DELETE /v1/assist-packs/{id}/hard", h.hardDeletePack)
	mux.HandleFunc("POST /v1/assist-runs", h.runAssist)
	mux.HandleFunc("GET /v1/assist-runs", h.listRuns)
	mux.HandleFunc("GET /v1/assist-runs/{id}", h.getRun)
}

func (h *Handler) upsertPack(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionAssistWrite) {
		return
	}
	var body assistdto.AssistPackRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if !canWriteOwner(r, body.OwnerSystem) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "owner_system is not allowed for this principal")
		return
	}
	out, err := h.uc.UpsertPack(r.Context(), UpsertPackInput{
		OrgID:          resolveOrgID(r),
		OwnerSystem:    body.OwnerSystem,
		ProductSurface: body.ProductSurface,
		AssistType:     body.AssistType,
		Name:           body.Name,
		Description:    body.Description,
		PromptTemplate: body.PromptTemplate,
		ModelPolicy:    body.ModelPolicy,
		OutputSchema:   body.OutputSchema,
		Enabled:        body.Enabled,
	})
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, packResponse(out))
}

func (h *Handler) listPacks(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionAssistRead) {
		return
	}
	out, err := h.uc.ListPacks(r.Context(), ListPacksInput{
		OrgID:          resolveOrgID(r),
		OwnerSystem:    r.URL.Query().Get("owner_system"),
		ProductSurface: r.URL.Query().Get("product_surface"),
		AssistType:     r.URL.Query().Get("assist_type"),
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	resp := make([]assistdto.AssistPackResponse, 0, len(out))
	for _, item := range out {
		resp = append(resp, packResponse(item))
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) listArchivedPacks(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionAssistRead) {
		return
	}
	out, err := h.uc.ListPacks(r.Context(), ListPacksInput{
		OrgID:          resolveOrgID(r),
		OwnerSystem:    r.URL.Query().Get("owner_system"),
		ProductSurface: r.URL.Query().Get("product_surface"),
		AssistType:     r.URL.Query().Get("assist_type"),
		ArchivedOnly:   true,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	resp := make([]assistdto.AssistPackResponse, 0, len(out))
	for _, item := range out {
		resp = append(resp, packResponse(item))
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) getPack(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionAssistRead) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	out, err := h.uc.GetPack(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, out.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "pack org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, packResponse(out))
}

func (h *Handler) updatePack(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionAssistWrite) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	current, err := h.uc.GetPack(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, current.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "pack org is not allowed for this principal")
		return
	}
	var body assistdto.AssistPackRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	owner := body.OwnerSystem
	if strings.TrimSpace(owner) == "" {
		owner = current.OwnerSystem
	}
	if !canWriteOwner(r, owner) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "owner_system is not allowed for this principal")
		return
	}
	out, err := h.uc.UpdatePack(r.Context(), UpdatePackInput{
		ID:             id,
		OwnerSystem:    body.OwnerSystem,
		ProductSurface: body.ProductSurface,
		AssistType:     body.AssistType,
		Name:           body.Name,
		Description:    body.Description,
		PromptTemplate: body.PromptTemplate,
		ModelPolicy:    body.ModelPolicy,
		OutputSchema:   body.OutputSchema,
		Enabled:        body.Enabled,
	})
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, packResponse(out))
}

func (h *Handler) archivePack(w http.ResponseWriter, r *http.Request) {
	h.packMutation(w, r, h.uc.ArchivePack)
}

func (h *Handler) restorePack(w http.ResponseWriter, r *http.Request) {
	h.packMutation(w, r, h.uc.RestorePack)
}

func (h *Handler) hardDeletePack(w http.ResponseWriter, r *http.Request) {
	h.packMutation(w, r, h.uc.HardDeletePack)
}

func (h *Handler) packMutation(w http.ResponseWriter, r *http.Request, fn func(context.Context, string, string, uuid.UUID) error) {
	if !requireScope(w, r, scopeCompanionAssistWrite) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	current, err := h.uc.GetPack(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, current.OrgID) || !canWriteOwner(r, current.OwnerSystem) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "pack is not allowed for this principal")
		return
	}
	if err := fn(r.Context(), current.OrgID, identityctx.FromRequest(r).EffectiveActorID(), id); err != nil {
		writeUsecaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) runAssist(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionAssistWrite) {
		return
	}
	var body assistdto.AssistRunRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if !canWriteOwner(r, body.OwnerSystem) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "owner_system is not allowed for this principal")
		return
	}
	out, err := h.uc.RunAssist(r.Context(), RunAssistInput{
		OrgID:          resolveOrgID(r),
		OwnerSystem:    body.OwnerSystem,
		ProductSurface: body.ProductSurface,
		AssistType:     body.AssistType,
		SubjectType:    body.SubjectType,
		SubjectID:      body.SubjectID,
		Input:          body.Input,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		if out.ID != uuid.Nil {
			httpjson.WriteJSON(w, http.StatusBadGateway, runResponse(out))
			return
		}
		writeUsecaseError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, runResponse(out))
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionAssistRead) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.uc.ListRuns(r.Context(), ListRunsInput{
		OrgID:          resolveOrgID(r),
		OwnerSystem:    r.URL.Query().Get("owner_system"),
		ProductSurface: r.URL.Query().Get("product_surface"),
		AssistType:     r.URL.Query().Get("assist_type"),
		SubjectID:      r.URL.Query().Get("subject_id"),
		Status:         r.URL.Query().Get("status"),
		Limit:          limit,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	resp := make([]assistdto.AssistRunResponse, 0, len(out))
	for _, item := range out {
		resp = append(resp, runResponse(item))
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionAssistRead) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	out, err := h.uc.GetRun(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, out.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "run org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, runResponse(out))
}

func resolveOrgID(r *http.Request) string {
	orgID, _ := identityctx.EffectiveOrgID(r, r.URL.Query().Get("org_id"), scopeCompanionCrossOrg)
	return orgID
}

func parsePathUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

func writeUsecaseError(w http.ResponseWriter, err error) {
	var de domainerr.Error
	if errors.As(err, &de) {
		switch de.Kind() {
		case domainerr.KindNotFound:
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", de.Error())
			return
		case domainerr.KindConflict:
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", de.Error())
			return
		case domainerr.KindValidation:
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", de.Error())
			return
		}
	}
	if errors.Is(err, ErrNotFound) {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}
	httpjson.WriteFlatInternalError(w, err, "assist request failed")
}

func packResponse(pack domain.AssistPack) assistdto.AssistPackResponse {
	resp := assistdto.AssistPackResponse{
		ID:             pack.ID.String(),
		OrgID:          pack.OrgID,
		OwnerSystem:    pack.OwnerSystem,
		ProductSurface: pack.ProductSurface,
		AssistType:     pack.AssistType,
		Name:           pack.Name,
		Description:    pack.Description,
		PromptTemplate: pack.PromptTemplate,
		ModelPolicy:    pack.ModelPolicy,
		OutputSchema:   pack.OutputSchema,
		Enabled:        pack.Enabled,
		CreatedAt:      pack.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      pack.UpdatedAt.Format(time.RFC3339),
	}
	if pack.ArchivedAt != nil {
		resp.ArchivedAt = pack.ArchivedAt.Format(time.RFC3339)
	}
	return resp
}

func runResponse(run domain.AssistRun) assistdto.AssistRunResponse {
	resp := assistdto.AssistRunResponse{
		ID:             run.ID.String(),
		OrgID:          run.OrgID,
		PackID:         run.PackID.String(),
		OwnerSystem:    run.OwnerSystem,
		ProductSurface: run.ProductSurface,
		AssistType:     run.AssistType,
		SubjectType:    run.SubjectType,
		SubjectID:      run.SubjectID,
		Input:          run.Input,
		Output:         run.Output,
		Status:         run.Status,
		ErrorMessage:   run.ErrorMessage,
		CreatedAt:      run.CreatedAt.Format(time.RFC3339),
	}
	if run.CompletedAt != nil {
		resp.CompletedAt = run.CompletedAt.Format(time.RFC3339)
	}
	return resp
}
