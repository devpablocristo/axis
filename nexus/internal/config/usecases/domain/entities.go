package domain

// SystemConfig contiene TODA la configuración del sistema.
// Nexus es AI-independent: no hay sección AI. La asistencia con LLM vive en
// Companion y se configura allá.
type SystemConfig struct {
	Risk      RiskConfig      `json:"risk"`
	Approvals ApprovalsConfig `json:"approvals"`
	Learning  LearningConfig  `json:"learning"`
	General   GeneralConfig   `json:"general"`
}

// --- Risk Cascade ---

type RiskConfig struct {
	Thresholds            Thresholds            `json:"thresholds"`
	ActionTypes           ActionTypeRisk        `json:"action_types"`
	BusinessHours         BusinessHours         `json:"business_hours"`
	FrequencyThresholds   FrequencyThresholds   `json:"frequency_thresholds"`
	ActorThresholds       ActorThresholds       `json:"actor_thresholds"`
	SuccessRateThresholds SuccessRateThresholds `json:"success_rate_thresholds"`
	Amplifications        []Amplification       `json:"amplifications"`
	SensitiveSystems      []string              `json:"sensitive_systems"`
}

type Thresholds struct {
	Allow            float64 `json:"allow"`
	EnhancedLog      float64 `json:"enhanced_log"`
	RequireApproval  float64 `json:"require_approval"`
	Deny             float64 `json:"deny"`
	MaxAmplification float64 `json:"max_amplification"`
}

type ActionTypeRisk struct {
	High   []string `json:"high"`
	Medium []string `json:"medium"`
}

type BusinessHours struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type FrequencyThresholds struct {
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
}

type ActorThresholds struct {
	Unknown int `json:"unknown"`
	New     int `json:"new"`
}

type SuccessRateThresholds struct {
	Low       float64 `json:"low"`       // debajo de esto: score 0.30 (default 0.5)
	Moderate  float64 `json:"moderate"`  // debajo de esto: score 0.10 (default 0.8)
	Excellent float64 `json:"excellent"` // arriba de esto: score -0.15 (default 0.95)
}

type Amplification struct {
	Factors    []string `json:"factors"`
	Multiplier float64  `json:"multiplier"`
	Reason     string   `json:"reason"`
}

// --- Approvals ---

type ApprovalsConfig struct {
	DefaultTTLSeconds int              `json:"default_ttl_seconds"` // default 3600 (1h)
	BreakGlassRules   []BreakGlassRule `json:"break_glass_rules"`   // reglas que activan break-glass
	BreakGlassDefault int              `json:"break_glass_default"` // aprobadores por defecto para break-glass (default 2)
}

// BreakGlassRule define cuándo se activa break-glass
type BreakGlassRule struct {
	ActionTypes       []string `json:"action_types"`       // action types que matchean (vacío = todos)
	RiskLevel         string   `json:"risk_level"`         // "critical", "high" (vacío = cualquiera)
	RequiredApprovals int      `json:"required_approvals"` // cuántos aprobadores
}

// --- Learning ---

type LearningConfig struct {
	MinSamples      int     `json:"min_samples"`       // mínimo de muestras para proponer (default 50)
	MinApprovalRate float64 `json:"min_approval_rate"` // tasa mínima de consistencia (default 0.90)
	MaxRequests     int     `json:"max_requests"`      // máximo de requests a analizar (default 10000)
}

// --- General ---

type GeneralConfig struct {
	DefaultListLimit           int `json:"default_list_limit"`            // default 50
	MaxListLimit               int `json:"max_list_limit"`                // default 1000
	MaxExpressionLength        int `json:"max_expression_length"`         // default 5000
	MaxIdempotencyKeyLength    int `json:"max_idempotency_key_length"`    // default 256
	IdempotencyCacheTTLSeconds int `json:"idempotency_cache_ttl_seconds"` // default 86400 (24h)
	MaxBodySizeBytes           int `json:"max_body_size_bytes"`           // default 1048576 (1MB)
}

// DefaultSystemConfig retorna la configuración completa por defecto
func DefaultSystemConfig() SystemConfig {
	return SystemConfig{
		Risk: RiskConfig{
			Thresholds: Thresholds{
				Allow:            0.5,
				EnhancedLog:      1.0,
				RequireApproval:  1.5,
				Deny:             2.0,
				MaxAmplification: 3.0,
			},
			ActionTypes: ActionTypeRisk{
				High:   []string{"alert.silence", "runbook.execute", "delete"},
				Medium: []string{"incident.resolve", "config.update", "deploy.trigger"},
			},
			BusinessHours:         BusinessHours{Start: 9, End: 18},
			FrequencyThresholds:   FrequencyThresholds{Warning: 10, Critical: 20},
			ActorThresholds:       ActorThresholds{Unknown: 0, New: 10},
			SuccessRateThresholds: SuccessRateThresholds{Low: 0.5, Moderate: 0.8, Excellent: 0.95},
			Amplifications: []Amplification{
				{Factors: []string{"off_hours", "actor_unknown"}, Multiplier: 1.8, Reason: "off-hours + unknown actor"},
				{Factors: []string{"action_type", "frequency_anomaly"}, Multiplier: 1.5, Reason: "risky action + frequency anomaly"},
				{Factors: []string{"actor_unknown", "target_sensitivity"}, Multiplier: 1.6, Reason: "unknown actor + sensitive target"},
				{Factors: []string{"off_hours", "actor_unknown", "frequency_anomaly"}, Multiplier: 2.5, Reason: "full cascade: off-hours + unknown + frequency"},
				{Factors: []string{"action_type", "off_hours", "target_sensitivity"}, Multiplier: 2.0, Reason: "risky action + off-hours + sensitive target"},
			},
			SensitiveSystems: []string{"production", "prod"},
		},
		Approvals: ApprovalsConfig{
			DefaultTTLSeconds: 3600,
			BreakGlassDefault: 2,
			BreakGlassRules: []BreakGlassRule{
				{ActionTypes: []string{"delete"}, RiskLevel: "critical", RequiredApprovals: 2},
				{ActionTypes: []string{"runbook.execute"}, RiskLevel: "high", RequiredApprovals: 2},
			},
		},
		Learning: LearningConfig{
			MinSamples:      50,
			MinApprovalRate: 0.90,
			MaxRequests:     10000,
		},
		General: GeneralConfig{
			DefaultListLimit:           50,
			MaxListLimit:               1000,
			MaxExpressionLength:        5000,
			MaxIdempotencyKeyLength:    256,
			IdempotencyCacheTTLSeconds: 86400,
			MaxBodySizeBytes:           1048576,
		},
	}
}
