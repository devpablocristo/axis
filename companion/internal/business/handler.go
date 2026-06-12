package business

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeBusinessAdmin = "companion:runtime:admin"
	scopeCrossOrg      = "companion:cross_org"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/business-model", h.get)
	mux.HandleFunc("PUT /v1/business-model", h.put)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	orgID, productSurface, ok := businessRequestContext(w, r)
	if !ok {
		return
	}
	model, err := h.uc.Get(r.Context(), orgID, productSurface)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpjson.WriteJSON(w, http.StatusOK, Model{OrgID: orgID, ProductSurface: productSurface, Status: "active"})
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get business model failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, model)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	orgID, productSurface, ok := businessRequestContext(w, r)
	if !ok {
		return
	}
	var model Model
	if err := json.NewDecoder(r.Body).Decode(&model); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	model.OrgID = orgID
	model.ProductSurface = productSurface
	model.CreatedBy = identityctx.FromRequest(r).EffectiveActorID()
	saved, err := h.uc.Save(r.Context(), model)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "save business model failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, saved)
}

func businessRequestContext(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "business model endpoints require authenticated admin context")
		return "", "", false
	}
	if !identityctx.HasAnyScope(r, scopeBusinessAdmin, scopeCrossOrg) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing business model admin scope")
		return "", "", false
	}
	id := identityctx.FromRequest(r).WithProductSurface(identityctx.FromRequest(r).ProductSurface)
	orgID := strings.TrimSpace(id.CustomerOrgID)
	if orgID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context is required")
		return "", "", false
	}
	return orgID, id.ProductSurface, true
}
