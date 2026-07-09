package dto

import "github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"

type DecisionRequest struct {
	Note string `json:"note"`
}

func (r DecisionRequest) ToDomain() domain.DecisionInput {
	return domain.DecisionInput{Note: r.Note}
}
