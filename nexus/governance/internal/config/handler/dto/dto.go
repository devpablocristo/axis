package dto

// --- DTOs para config (nunca exponer domain directamente) ---

// SystemConfigResponse es la respuesta completa de configuración.
// Nexus es AI-independent: no hay sección AI en la API.
type SystemConfigResponse struct {
	Risk      RiskConfigDTO      `json:"risk"`
	Approvals ApprovalsConfigDTO `json:"approvals"`
	Learning  LearningConfigDTO  `json:"learning"`
	General   GeneralConfigDTO   `json:"general"`
}

// UpdateSystemConfigRequest es el body para actualizar toda la config
type UpdateSystemConfigRequest = SystemConfigResponse

// --- Risk ---

type RiskConfigDTO struct {
	Thresholds          ThresholdsDTO          `json:"thresholds"`
	ActionTypes         ActionTypeRiskDTO      `json:"action_types"`
	BusinessHours       BusinessHoursDTO       `json:"business_hours"`
	FrequencyThresholds FrequencyThresholdsDTO `json:"frequency_thresholds"`
	ActorThresholds     ActorThresholdsDTO     `json:"actor_thresholds"`
	SuccessRateThresholds SuccessRateThresholdsDTO `json:"success_rate_thresholds"`
	Amplifications      []AmplificationDTO     `json:"amplifications"`
	SensitiveSystems    []string               `json:"sensitive_systems"`
}

type ThresholdsDTO struct {
	Allow            float64 `json:"allow"`
	EnhancedLog      float64 `json:"enhanced_log"`
	RequireApproval  float64 `json:"require_approval"`
	Deny             float64 `json:"deny"`
	MaxAmplification float64 `json:"max_amplification"`
}

type ActionTypeRiskDTO struct {
	High   []string `json:"high"`
	Medium []string `json:"medium"`
}

type BusinessHoursDTO struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type FrequencyThresholdsDTO struct {
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
}

type ActorThresholdsDTO struct {
	Unknown int `json:"unknown"`
	New     int `json:"new"`
}

type SuccessRateThresholdsDTO struct {
	Low       float64 `json:"low"`
	Moderate  float64 `json:"moderate"`
	Excellent float64 `json:"excellent"`
}

type AmplificationDTO struct {
	Factors    []string `json:"factors"`
	Multiplier float64  `json:"multiplier"`
	Reason     string   `json:"reason"`
}

// --- Approvals ---

type ApprovalsConfigDTO struct {
	DefaultTTLSeconds int `json:"default_ttl_seconds"`
}

// --- Learning ---

type LearningConfigDTO struct {
	MinSamples      int     `json:"min_samples"`
	MinApprovalRate float64 `json:"min_approval_rate"`
	MaxRequests     int     `json:"max_requests"`
}

// --- General ---

type GeneralConfigDTO struct {
	DefaultListLimit           int `json:"default_list_limit"`
	MaxListLimit               int `json:"max_list_limit"`
	MaxExpressionLength        int `json:"max_expression_length"`
	MaxIdempotencyKeyLength    int `json:"max_idempotency_key_length"`
	IdempotencyCacheTTLSeconds int `json:"idempotency_cache_ttl_seconds"`
	MaxBodySizeBytes           int `json:"max_body_size_bytes"`
}
