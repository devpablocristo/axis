package securityevals

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const scopeSecurityEvalsAdmin = "companion:evals:admin"

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/security-evals/suites", h.listSuites)
	mux.HandleFunc("POST /v1/security-evals/runs", h.runSuite)
	mux.HandleFunc("GET /v1/security-evals/reports", h.listReports)
}

func (h *Handler) listSuites(w http.ResponseWriter, r *http.Request) {
	if !requireEvalScope(w, r) {
		return
	}
	suites, err := h.uc.ListSuites(r.Context())
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list security eval suites failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"suites": suites})
}

func (h *Handler) runSuite(w http.ResponseWriter, r *http.Request) {
	if !requireEvalScope(w, r) {
		return
	}
	var body struct {
		Suite string `json:"suite"`
	}
	_ = httpjson.DecodeJSON(r, &body)
	report, err := h.uc.RunSuite(r.Context(), identityctx.PrincipalOrgID(r), strings.TrimSpace(body.Suite), identityctx.PrincipalUserID(r))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "EVAL_FAILED", err.Error())
		return
	}
	status := http.StatusCreated
	if report.Status == "failed" {
		status = http.StatusUnprocessableEntity
	}
	httpjson.WriteJSON(w, status, report)
}

func (h *Handler) listReports(w http.ResponseWriter, r *http.Request) {
	if !requireEvalScope(w, r) {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	reports, err := h.uc.ListReports(r.Context(), identityctx.PrincipalOrgID(r), strings.TrimSpace(r.URL.Query().Get("suite")), limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list security eval reports failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"reports": reports})
}

func requireEvalScope(w http.ResponseWriter, r *http.Request) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopeSecurityEvalsAdmin, "companion:runtime:admin", "companion:cross_org") {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing security eval admin scope")
	return false
}
