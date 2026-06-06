package products

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeProductsRead       = "companion:products:read"
	scopeProductsAdmin      = "companion:products:admin"
	scopeRuntimeAdmin       = "companion:runtime:admin"
	scopeCrossOrg           = "companion:cross_org"
	defaultInstallationAuth = AuthModeNone
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/products", h.listProducts)
	mux.HandleFunc("GET /v1/products/{product_surface}", h.getProduct)
	mux.HandleFunc("PUT /v1/products/{product_surface}", h.putProduct)
	mux.HandleFunc("GET /v1/product-installations", h.listInstallations)
	mux.HandleFunc("GET /v1/product-installations/{product_surface}", h.getInstallation)
	mux.HandleFunc("PUT /v1/product-installations/{product_surface}", h.putInstallation)
	mux.HandleFunc("GET /v1/product-installations/{product_surface}/resolve", h.resolveInstallation)
}

func (h *Handler) listProducts(w http.ResponseWriter, r *http.Request) {
	if !requireProductScope(w, r, scopeProductsRead, scopeProductsAdmin, scopeRuntimeAdmin, scopeCrossOrg) {
		return
	}
	products, err := h.uc.ListProducts(r.Context())
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list products failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"products": products})
}

func (h *Handler) getProduct(w http.ResponseWriter, r *http.Request) {
	if !requireProductScope(w, r, scopeProductsRead, scopeProductsAdmin, scopeRuntimeAdmin, scopeCrossOrg) {
		return
	}
	product, err := h.uc.GetProduct(r.Context(), r.PathValue("product_surface"))
	if err != nil {
		writeProductError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, product)
}

func (h *Handler) putProduct(w http.ResponseWriter, r *http.Request) {
	if !requireProductScope(w, r, scopeProductsAdmin, scopeRuntimeAdmin, scopeCrossOrg) {
		return
	}
	var body struct {
		DisplayName string         `json:"display_name"`
		Status      string         `json:"status"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	product, err := h.uc.SaveProduct(r.Context(), Product{
		ProductSurface: r.PathValue("product_surface"),
		DisplayName:    body.DisplayName,
		Status:         body.Status,
		Metadata:       body.Metadata,
		CreatedBy:      identityctx.FromRequest(r).EffectiveActorID(),
	})
	if err != nil {
		writeProductError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, product)
}

func (h *Handler) listInstallations(w http.ResponseWriter, r *http.Request) {
	orgID, ok := productInstallationOrg(w, r, scopeProductsRead, scopeProductsAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	installations, err := h.uc.ListInstallations(r.Context(), orgID)
	if err != nil {
		writeProductError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"installations": installations})
}

func (h *Handler) getInstallation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := productInstallationOrg(w, r, scopeProductsRead, scopeProductsAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	installation, err := h.uc.GetInstallation(r.Context(), orgID, r.PathValue("product_surface"))
	if err != nil {
		writeProductError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, installation)
}

func (h *Handler) resolveInstallation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := productInstallationOrg(w, r, scopeProductsRead, scopeProductsAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	installation, err := h.uc.ResolveInstallation(r.Context(), orgID, r.PathValue("product_surface"))
	if err != nil {
		writeProductError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, installation)
}

func (h *Handler) putInstallation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := productInstallationOrg(w, r, scopeProductsAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	var body struct {
		ExternalTenantID string         `json:"external_tenant_id"`
		BaseURL          string         `json:"base_url"`
		AuthMode         string         `json:"auth_mode"`
		SecretRef        string         `json:"secret_ref"`
		Enabled          *bool          `json:"enabled"`
		Config           map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	authMode := strings.TrimSpace(body.AuthMode)
	if authMode == "" {
		authMode = defaultInstallationAuth
	}
	installation, err := h.uc.SaveInstallation(r.Context(), Installation{
		OrgID:            orgID,
		ProductSurface:   r.PathValue("product_surface"),
		ExternalTenantID: body.ExternalTenantID,
		BaseURL:          body.BaseURL,
		AuthMode:         authMode,
		SecretRef:        body.SecretRef,
		Enabled:          enabled,
		Config:           body.Config,
		CreatedBy:        identityctx.FromRequest(r).EffectiveActorID(),
	})
	if err != nil {
		writeProductError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, installation)
}

func productInstallationOrg(w http.ResponseWriter, r *http.Request, scopes ...string) (string, bool) {
	if !requireProductScope(w, r, scopes...) {
		return "", false
	}
	orgID, ok := identityctx.EffectiveOrgID(r, r.URL.Query().Get("org_id"), scopeCrossOrg)
	if !ok || strings.TrimSpace(orgID) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context is required")
		return "", false
	}
	return orgID, true
}

func requireProductScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "product registry endpoints require authenticated context")
		return false
	}
	if identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing product registry scope")
	return false
}

func writeProductError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrProductNotFound), errors.Is(err, ErrInstallationNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrInstallationDisabled), errors.Is(err, ErrProductDisabled):
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "product registry operation failed")
	}
}
