package dto

import "github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"

type CheckResponse struct {
	Decision             string `json:"decision"`
	RiskLevel            string `json:"risk_level"`
	Status               string `json:"status"`
	DecisionReason       string `json:"decision_reason"`
	WouldRequireApproval bool   `json:"would_require_approval"`
	Mode                 string `json:"mode"`
}

func CheckFromDomain(result domain.CheckResult) CheckResponse {
	return CheckResponse{
		Decision:             string(result.Decision),
		RiskLevel:            result.RiskLevel,
		Status:               string(result.Status),
		DecisionReason:       result.DecisionReason,
		WouldRequireApproval: result.WouldRequireApproval,
		Mode:                 result.Mode,
	}
}
