package requests

import (
	requestdomain "github.com/devpablocristo/nexus/governance/internal/requests/usecases/domain"
)

// --- Niveles de decisión por score ---

const (
	// Umbrales de score → decisión
	thresholdAllow           = 0.5
	thresholdEnhancedLog     = 1.0
	thresholdRequireApproval = 1.5
	thresholdDeny            = 2.0

	// Máximo multiplicador de amplificación
	maxAmplification = 3.0
)

// --- Factores de riesgo (como factores de coagulación I-XIII) ---

// RiskFactor representa un factor evaluado con su score y si está activo
type RiskFactor struct {
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Active bool    `json:"active"`
	Reason string  `json:"reason"`
}

// RiskAssessment es el resultado completo de la evaluación de riesgo
type RiskAssessment struct {
	Factors       []RiskFactor `json:"factors"`
	RawScore      float64      `json:"raw_score"`
	Amplification float64      `json:"amplification"`
	FinalScore    float64      `json:"final_score"`
	Level         string       `json:"level"`
	Decision      string       `json:"decision"`
}

// --- Configuración ---

// RiskConfig define qué action types tienen riesgo base alto/medio
type RiskConfig struct {
	HighActionTypes   map[string]bool
	MediumActionTypes map[string]bool
}

// DefaultRiskConfig retorna la configuración default de riesgo
func DefaultRiskConfig() RiskConfig {
	return RiskConfig{
		HighActionTypes:   map[string]bool{"alert.silence": true, "runbook.execute": true, "delete": true},
		MediumActionTypes: map[string]bool{"incident.resolve": true, "config.update": true, "deploy.trigger": true},
	}
}

// --- Contexto de señales para la cascada ---

// RiskSignals son las señales dinámicas que alimentan los factores
type RiskSignals struct {
	ActionType      string
	TargetSystem    string
	RequesterID     string
	RequesterType   string
	CurrentHour     int
	ActorHistory    int     // cantidad de requests previas del actor
	RecentFrequency int     // requests del mismo action_type en la última hora
	SuccessRate     float64 // tasa de éxito histórica (0.0 - 1.0), -1 si no hay datos
}

// --- Motor de cascada (inspirado en cascada de coagulación) ---

// EvaluateCascade evalúa todos los factores de riesgo y aplica amplificaciones
func EvaluateCascade(signals RiskSignals, cfg RiskConfig, policyRiskOverride *string) RiskAssessment {
	factors := evaluateFactors(signals, cfg)
	rawScore := sumFactors(factors)
	amp := calculateAmplification(factors)
	finalScore := rawScore * amp

	// Aplicar override de policy si existe
	if policyRiskOverride != nil {
		finalScore = applyPolicyOverride(*policyRiskOverride, finalScore)
	}

	level := scoreToLevel(finalScore)
	decision := scoreToDecision(finalScore)

	return RiskAssessment{
		Factors:       factors,
		RawScore:      rawScore,
		Amplification: amp,
		FinalScore:    finalScore,
		Level:         level,
		Decision:      decision,
	}
}

// evaluateFactors evalúa cada factor de riesgo individualmente
func evaluateFactors(s RiskSignals, cfg RiskConfig) []RiskFactor {
	factors := make([]RiskFactor, 0, 6)

	// F1: Action type — riesgo base según la acción
	f1 := RiskFactor{Name: "action_type"}
	if cfg.HighActionTypes[s.ActionType] {
		f1.Score = 0.4
		f1.Active = true
		f1.Reason = s.ActionType + " is high-risk action"
	} else if cfg.MediumActionTypes[s.ActionType] {
		f1.Score = 0.2
		f1.Active = true
		f1.Reason = s.ActionType + " is medium-risk action"
	} else {
		f1.Score = 0.1
		f1.Reason = s.ActionType + " is low-risk action"
	}
	factors = append(factors, f1)

	// F2: Off-hours — fuera de horario laboral (9-18)
	f2 := RiskFactor{Name: "off_hours"}
	if s.CurrentHour < 9 || s.CurrentHour >= 18 {
		f2.Score = 0.2
		f2.Active = true
		f2.Reason = "request at off-hours (hour=" + itoa(s.CurrentHour) + ")"
	}
	factors = append(factors, f2)

	// F3: Actor history — actor desconocido o con poco historial
	f3 := RiskFactor{Name: "actor_unknown"}
	if s.ActorHistory == 0 {
		f3.Score = 0.3
		f3.Active = true
		f3.Reason = "unknown actor, no previous requests"
	} else if s.ActorHistory < 10 {
		f3.Score = 0.15
		f3.Active = true
		f3.Reason = "new actor, only " + itoa(s.ActorHistory) + " previous requests"
	}
	factors = append(factors, f3)

	// F4: Frequency anomaly — demasiadas requests del mismo tipo en poco tiempo
	f4 := RiskFactor{Name: "frequency_anomaly"}
	if s.RecentFrequency > 20 {
		f4.Score = 0.3
		f4.Active = true
		f4.Reason = itoa(s.RecentFrequency) + " requests of same type in last hour (>20)"
	} else if s.RecentFrequency > 10 {
		f4.Score = 0.15
		f4.Active = true
		f4.Reason = itoa(s.RecentFrequency) + " requests of same type in last hour (>10)"
	}
	factors = append(factors, f4)

	// F5: Execution history — tasa de éxito/fallo de requests similares
	f5 := RiskFactor{Name: "execution_history"}
	if s.SuccessRate >= 0 {
		if s.SuccessRate < 0.5 {
			f5.Score = 0.3
			f5.Active = true
			f5.Reason = "low success rate (" + ftoa(s.SuccessRate) + ") for similar requests"
		} else if s.SuccessRate < 0.8 {
			f5.Score = 0.1
			f5.Active = true
			f5.Reason = "moderate success rate (" + ftoa(s.SuccessRate) + ")"
		} else if s.SuccessRate >= 0.95 {
			// Factor de seguridad — reduce riesgo
			f5.Score = -0.15
			f5.Reason = "excellent success rate (" + ftoa(s.SuccessRate) + ")"
		}
	}
	factors = append(factors, f5)

	// F6: Target sensitivity — producción o sistemas críticos
	f6 := RiskFactor{Name: "target_sensitivity"}
	switch s.TargetSystem {
	case "production", "prod":
		f6.Score = 0.3
		f6.Active = true
		f6.Reason = "target is production system"
	case "staging", "stg":
		f6.Score = 0.1
		f6.Active = true
		f6.Reason = "target is staging system"
	}
	factors = append(factors, f6)

	return factors
}

// calculateAmplification detecta combinaciones de factores activos
// que se amplifican mutuamente (como la cascada de coagulación)
func calculateAmplification(factors []RiskFactor) float64 {
	active := activeSet(factors)

	amp := 1.0

	// Dos factores activos — amplificación moderada
	if active["off_hours"] && active["actor_unknown"] {
		amp = max(amp, 1.8) // fuera de horario + desconocido = sospechoso
	}
	if active["action_type"] && active["frequency_anomaly"] {
		amp = max(amp, 1.5) // acción riesgosa + frecuencia anómala
	}
	if active["actor_unknown"] && active["target_sensitivity"] {
		amp = max(amp, 1.6) // desconocido atacando prod
	}

	// Tres factores activos — amplificación alta
	if active["off_hours"] && active["actor_unknown"] && active["frequency_anomaly"] {
		amp = max(amp, 2.5) // cascada completa: todo sospechoso
	}
	if active["action_type"] && active["off_hours"] && active["target_sensitivity"] {
		amp = max(amp, 2.0) // acción peligrosa + off-hours + prod
	}

	// Cuatro o más — amplificación máxima
	activeCount := 0
	for _, f := range factors {
		if f.Active {
			activeCount++
		}
	}
	if activeCount >= 4 {
		amp = max(amp, 2.5)
	}

	// Aplicar cap
	if amp > maxAmplification {
		amp = maxAmplification
	}

	return amp
}

// --- Funciones de decisión (compatibles con el flujo existente) ---

// TierRisk determina el nivel de riesgo usando el mapa de action types (determinístico).
// Se usa en Submit para decisiones de negocio. La cascada completa se usa en Simulate.
func TierRisk(actionType string, policyRiskOverride *string, cfg RiskConfig) requestdomain.RiskLevel {
	if policyRiskOverride != nil {
		switch *policyRiskOverride {
		case "high":
			return requestdomain.RiskHigh
		case "medium":
			return requestdomain.RiskMedium
		case "low":
			return requestdomain.RiskLow
		}
	}
	if cfg.HighActionTypes[actionType] {
		return requestdomain.RiskHigh
	}
	if cfg.MediumActionTypes[actionType] {
		return requestdomain.RiskMedium
	}
	return requestdomain.RiskLow
}

// TierRiskFromSignals determina el nivel de riesgo con señales completas
func TierRiskFromSignals(signals RiskSignals, cfg RiskConfig, policyRiskOverride *string) (requestdomain.RiskLevel, RiskAssessment) {
	assessment := EvaluateCascade(signals, cfg, policyRiskOverride)

	var level requestdomain.RiskLevel
	switch assessment.Level {
	case "critical", "high":
		level = requestdomain.RiskHigh
	case "medium":
		level = requestdomain.RiskMedium
	default:
		level = requestdomain.RiskLow
	}

	return level, assessment
}

// DecideFromPolicy decide basándose en el efecto de la policy y el riesgo
func DecideFromPolicy(effect string, risk requestdomain.RiskLevel) (requestdomain.Decision, bool) {
	switch effect {
	case "deny":
		return requestdomain.DecisionDeny, true
	case "require_approval":
		return requestdomain.DecisionRequireApproval, true
	case "allow":
		if risk == requestdomain.RiskHigh {
			return requestdomain.DecisionRequireApproval, true
		}
		return requestdomain.DecisionAllow, true
	}
	return "", false
}

// DefaultDecision decide sin policy match, basándose en el riesgo
func DefaultDecision(risk requestdomain.RiskLevel) requestdomain.Decision {
	if risk == requestdomain.RiskHigh {
		return requestdomain.DecisionRequireApproval
	}
	return requestdomain.DecisionAllow
}

// --- Helpers internos ---

func scoreToLevel(score float64) string {
	if score >= thresholdDeny {
		return "critical"
	}
	if score >= thresholdRequireApproval {
		return "high"
	}
	if score >= thresholdEnhancedLog {
		return "medium"
	}
	return "low"
}

func scoreToDecision(score float64) string {
	if score >= thresholdDeny {
		return "deny"
	}
	if score >= thresholdRequireApproval {
		return "require_approval"
	}
	return "allow"
}

func applyPolicyOverride(override string, currentScore float64) float64 {
	switch override {
	case "high":
		if currentScore < thresholdRequireApproval {
			return thresholdRequireApproval
		}
	case "medium":
		if currentScore < thresholdEnhancedLog {
			return thresholdEnhancedLog
		}
	case "low":
		if currentScore > thresholdAllow {
			return thresholdAllow * 0.9
		}
	}
	return currentScore
}

func sumFactors(factors []RiskFactor) float64 {
	total := 0.0
	for _, f := range factors {
		total += f.Score
	}
	if total < 0 {
		return 0
	}
	return total
}

func activeSet(factors []RiskFactor) map[string]bool {
	m := make(map[string]bool, len(factors))
	for _, f := range factors {
		if f.Active {
			m[f.Name] = true
		}
	}
	return m
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

func ftoa(f float64) string {
	// Formato simple: 2 decimales
	whole := int(f)
	frac := int((f - float64(whole)) * 100)
	if frac < 0 {
		frac = -frac
	}
	s := itoa(whole) + "."
	if frac < 10 {
		s += "0"
	}
	s += itoa(frac)
	return s
}
