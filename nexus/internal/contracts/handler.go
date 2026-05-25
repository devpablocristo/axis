package contracts

import (
	"context"
	"net/http"
	"strings"
	"time"

	domain "github.com/devpablocristo/nexus/internal/contracts/usecases/domain"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

type contractUsecase interface {
	Upsert(ctx context.Context, contract domain.Contract) (domain.Contract, error)
	List(ctx context.Context, orgID *string, includeGlobal bool) ([]domain.Contract, error)
	Validate(ctx context.Context, in ValidateInput) (ValidateOutput, error)
}

type Handler struct {
	uc contractUsecase
}

func NewHandler(uc contractUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/governance/contracts", h.list)
	mux.HandleFunc("POST /v1/governance/contracts", h.upsert)
	mux.HandleFunc("POST /v1/governance/contracts/validate", h.validate)
}

type contractRequest struct {
	OrgID          string         `json:"org_id,omitempty"`
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	SubjectType    string         `json:"subject_type,omitempty"`
	Schema         map[string]any `json:"schema"`
	Status         string         `json:"status,omitempty"`
	ValidationMode string         `json:"validation_mode,omitempty"`
	Compatibility  string         `json:"compatibility,omitempty"`
}

type validateRequest struct {
	OrgID       string         `json:"org_id,omitempty"`
	Name        string         `json:"name"`
	SubjectType string         `json:"subject_type,omitempty"`
	SubjectID   string         `json:"subject_id,omitempty"`
	Payload     map[string]any `json:"payload"`
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusContractsAdmin, scopeNexusPoliciesAdmin) {
		return
	}
	var body contractRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Version) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "name and version are required")
		return
	}
	orgID, ok := writableOrg(r, body.OrgID)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "contract org is not allowed for this principal")
		return
	}
	contract := domain.Contract{
		OrgID:          orgID,
		Name:           body.Name,
		Version:        body.Version,
		SubjectType:    body.SubjectType,
		Schema:         body.Schema,
		Status:         domain.ContractStatus(firstNonEmpty(body.Status, string(domain.ContractStatusDraft))),
		ValidationMode: domain.ValidationMode(firstNonEmpty(body.ValidationMode, string(domain.ValidationModeReportOnly))),
		Compatibility:  body.Compatibility,
		CreatedBy:      actorID(r),
	}
	created, err := h.uc.Upsert(r.Context(), contract)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "upsert governance contract")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusContractsAdmin, scopeNexusPoliciesAdmin) {
		return
	}
	orgID, ok := contractOrgScope(r)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	includeGlobal := orgID != nil || identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg)
	list, err := h.uc.List(r.Context(), orgID, includeGlobal)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list governance contracts")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, contract := range list {
		out = append(out, toResponse(contract))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) validate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusContractsAdmin, scopeNexusPoliciesAdmin) {
		return
	}
	var body validateRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	orgID, ok := writableOrg(r, body.OrgID)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "contract org is not allowed for this principal")
		return
	}
	out, err := h.uc.Validate(r.Context(), ValidateInput{
		OrgID:       orgID,
		Name:        body.Name,
		SubjectType: body.SubjectType,
		SubjectID:   body.SubjectID,
		Payload:     body.Payload,
	})
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "contract not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, out)
}

func writableOrg(r *http.Request, requested string) (*string, bool) {
	requested = strings.TrimSpace(requested)
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		if requested == "" {
			return nil, true
		}
		return &requested, true
	}
	orgID := strings.TrimSpace(identityhttp.PrincipalOrgID(r))
	if orgID == "" {
		return nil, false
	}
	if requested != "" && requested != orgID {
		return nil, false
	}
	return &orgID, true
}

func actorID(r *http.Request) string {
	if identityhttp.HasNoAuthContext(r) {
		return "dev"
	}
	if id := strings.TrimSpace(identityhttp.FromRequest(r).Actor); id != "" {
		return id
	}
	return "unknown"
}

func toResponse(contract domain.Contract) map[string]any {
	orgID := ""
	if contract.OrgID != nil {
		orgID = *contract.OrgID
	}
	return map[string]any{
		"id":              contract.ID.String(),
		"org_id":          orgID,
		"name":            contract.Name,
		"version":         contract.Version,
		"subject_type":    contract.SubjectType,
		"schema":          contract.Schema,
		"status":          string(contract.Status),
		"validation_mode": string(contract.ValidationMode),
		"compatibility":   contract.Compatibility,
		"created_by":      contract.CreatedBy,
		"created_at":      contract.CreatedAt.Format(time.RFC3339),
		"updated_at":      contract.UpdatedAt.Format(time.RFC3339),
	}
}
