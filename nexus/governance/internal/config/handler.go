package config

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/devpablocristo/platform/http/go/httpjson"

	configdto "github.com/devpablocristo/nexus/governance/internal/config/handler/dto"
	configdomain "github.com/devpablocristo/nexus/governance/internal/config/usecases/domain"
)

type configUsecase interface {
	GetConfig(ctx context.Context) (*configdomain.SystemConfig, error)
	UpdateConfig(ctx context.Context, cfg configdomain.SystemConfig) (*configdomain.SystemConfig, error)
	ResetConfig(ctx context.Context) (*configdomain.SystemConfig, error)
	UpdateSection(ctx context.Context, section string, data json.RawMessage) (*configdomain.SystemConfig, error)
}

type Handler struct {
	uc configUsecase
}

func NewHandler(uc configUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/config", h.getConfig)
	mux.HandleFunc("PATCH /v1/config", h.updateConfig)
	mux.HandleFunc("PATCH /v1/config/{section}", h.updateSection)
	mux.HandleFunc("POST /v1/config/reset", h.resetConfig)
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.uc.GetConfig(r.Context())
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "get config")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toResponse(cfg))
}

func (h *Handler) updateConfig(w http.ResponseWriter, r *http.Request) {
	var body configdto.UpdateSystemConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}

	domainCfg := toDomain(body)
	updated, err := h.uc.UpdateConfig(r.Context(), domainCfg)
	if err != nil {
		// Nunca exponer err.Error() — mensaje genérico
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid configuration values")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toResponse(updated))
}

func (h *Handler) updateSection(w http.ResponseWriter, r *http.Request) {
	section := r.PathValue("section")
	var data json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}

	updated, err := h.uc.UpdateSection(r.Context(), section, data)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid configuration for section "+section)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toResponse(updated))
}

func (h *Handler) resetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.uc.ResetConfig(r.Context())
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "reset config")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toResponse(cfg))
}

// --- Mappers: domain ↔ DTO ---

func toResponse(cfg *configdomain.SystemConfig) configdto.SystemConfigResponse {
	return configdto.SystemConfigResponse{
		Risk: configdto.RiskConfigDTO{
			Thresholds: configdto.ThresholdsDTO{
				Allow: cfg.Risk.Thresholds.Allow, EnhancedLog: cfg.Risk.Thresholds.EnhancedLog,
				RequireApproval: cfg.Risk.Thresholds.RequireApproval, Deny: cfg.Risk.Thresholds.Deny,
				MaxAmplification: cfg.Risk.Thresholds.MaxAmplification,
			},
			ActionTypes: configdto.ActionTypeRiskDTO{High: cfg.Risk.ActionTypes.High, Medium: cfg.Risk.ActionTypes.Medium},
			BusinessHours: configdto.BusinessHoursDTO{Start: cfg.Risk.BusinessHours.Start, End: cfg.Risk.BusinessHours.End},
			FrequencyThresholds: configdto.FrequencyThresholdsDTO{Warning: cfg.Risk.FrequencyThresholds.Warning, Critical: cfg.Risk.FrequencyThresholds.Critical},
			ActorThresholds: configdto.ActorThresholdsDTO{Unknown: cfg.Risk.ActorThresholds.Unknown, New: cfg.Risk.ActorThresholds.New},
			SuccessRateThresholds: configdto.SuccessRateThresholdsDTO{Low: cfg.Risk.SuccessRateThresholds.Low, Moderate: cfg.Risk.SuccessRateThresholds.Moderate, Excellent: cfg.Risk.SuccessRateThresholds.Excellent},
			Amplifications: toAmplificationDTOs(cfg.Risk.Amplifications),
			SensitiveSystems: cfg.Risk.SensitiveSystems,
		},
		Approvals: configdto.ApprovalsConfigDTO{DefaultTTLSeconds: cfg.Approvals.DefaultTTLSeconds},
		Learning: configdto.LearningConfigDTO{MinSamples: cfg.Learning.MinSamples, MinApprovalRate: cfg.Learning.MinApprovalRate, MaxRequests: cfg.Learning.MaxRequests},
		General: configdto.GeneralConfigDTO{
			DefaultListLimit: cfg.General.DefaultListLimit, MaxListLimit: cfg.General.MaxListLimit,
			MaxExpressionLength: cfg.General.MaxExpressionLength, MaxIdempotencyKeyLength: cfg.General.MaxIdempotencyKeyLength,
			IdempotencyCacheTTLSeconds: cfg.General.IdempotencyCacheTTLSeconds, MaxBodySizeBytes: cfg.General.MaxBodySizeBytes,
		},
	}
}

func toDomain(dto configdto.SystemConfigResponse) configdomain.SystemConfig {
	return configdomain.SystemConfig{
		Risk: configdomain.RiskConfig{
			Thresholds: configdomain.Thresholds{
				Allow: dto.Risk.Thresholds.Allow, EnhancedLog: dto.Risk.Thresholds.EnhancedLog,
				RequireApproval: dto.Risk.Thresholds.RequireApproval, Deny: dto.Risk.Thresholds.Deny,
				MaxAmplification: dto.Risk.Thresholds.MaxAmplification,
			},
			ActionTypes: configdomain.ActionTypeRisk{High: dto.Risk.ActionTypes.High, Medium: dto.Risk.ActionTypes.Medium},
			BusinessHours: configdomain.BusinessHours{Start: dto.Risk.BusinessHours.Start, End: dto.Risk.BusinessHours.End},
			FrequencyThresholds: configdomain.FrequencyThresholds{Warning: dto.Risk.FrequencyThresholds.Warning, Critical: dto.Risk.FrequencyThresholds.Critical},
			ActorThresholds: configdomain.ActorThresholds{Unknown: dto.Risk.ActorThresholds.Unknown, New: dto.Risk.ActorThresholds.New},
			SuccessRateThresholds: configdomain.SuccessRateThresholds{Low: dto.Risk.SuccessRateThresholds.Low, Moderate: dto.Risk.SuccessRateThresholds.Moderate, Excellent: dto.Risk.SuccessRateThresholds.Excellent},
			Amplifications: toAmplificationDomain(dto.Risk.Amplifications),
			SensitiveSystems: dto.Risk.SensitiveSystems,
		},
		Approvals: configdomain.ApprovalsConfig{DefaultTTLSeconds: dto.Approvals.DefaultTTLSeconds},
		Learning: configdomain.LearningConfig{MinSamples: dto.Learning.MinSamples, MinApprovalRate: dto.Learning.MinApprovalRate, MaxRequests: dto.Learning.MaxRequests},
		General: configdomain.GeneralConfig{
			DefaultListLimit: dto.General.DefaultListLimit, MaxListLimit: dto.General.MaxListLimit,
			MaxExpressionLength: dto.General.MaxExpressionLength, MaxIdempotencyKeyLength: dto.General.MaxIdempotencyKeyLength,
			IdempotencyCacheTTLSeconds: dto.General.IdempotencyCacheTTLSeconds, MaxBodySizeBytes: dto.General.MaxBodySizeBytes,
		},
	}
}

func toAmplificationDTOs(amps []configdomain.Amplification) []configdto.AmplificationDTO {
	out := make([]configdto.AmplificationDTO, len(amps))
	for i, a := range amps {
		out[i] = configdto.AmplificationDTO{Factors: a.Factors, Multiplier: a.Multiplier, Reason: a.Reason}
	}
	return out
}

func toAmplificationDomain(dtos []configdto.AmplificationDTO) []configdomain.Amplification {
	out := make([]configdomain.Amplification, len(dtos))
	for i, d := range dtos {
		out[i] = configdomain.Amplification{Factors: d.Factors, Multiplier: d.Multiplier, Reason: d.Reason}
	}
	return out
}
