package dto

import "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"

type CreateJobRoleRequest struct {
	Name             string                    `json:"name" binding:"required"`
	Slug             string                    `json:"slug"`
	Mission          string                    `json:"mission"`
	Responsibilities []ResponsibilityRequest   `json:"responsibilities"`
	SuccessCriteria  []SuccessCriterionRequest `json:"success_criteria"`
}

type ResponsibilityRequest struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	ExpectedOutcome string `json:"expected_outcome"`
	Priority        int    `json:"priority"`
}

type SuccessCriterionRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	TargetValue string `json:"target_value"`
	Priority    int    `json:"priority"`
}

func (r CreateJobRoleRequest) ToDomain() domain.CreateInput {
	return domain.CreateInput{Name: r.Name, Slug: r.Slug, Mission: r.Mission,
		Responsibilities: responsibilitiesToDomain(r.Responsibilities), SuccessCriteria: successCriteriaToDomain(r.SuccessCriteria)}
}

type UpdateJobRoleRequest struct {
	Name             string                    `json:"name" binding:"required"`
	Slug             string                    `json:"slug"`
	Mission          string                    `json:"mission"`
	Responsibilities []ResponsibilityRequest   `json:"responsibilities"`
	SuccessCriteria  []SuccessCriterionRequest `json:"success_criteria"`
}

func (r UpdateJobRoleRequest) ToDomain() domain.UpdateInput {
	return domain.UpdateInput{Name: r.Name, Slug: r.Slug, Mission: r.Mission,
		Responsibilities: responsibilitiesToDomain(r.Responsibilities), SuccessCriteria: successCriteriaToDomain(r.SuccessCriteria)}
}

func responsibilitiesToDomain(items []ResponsibilityRequest) []domain.Responsibility {
	out := make([]domain.Responsibility, 0, len(items))
	for _, item := range items {
		out = append(out, domain.Responsibility{Title: item.Title, Description: item.Description, ExpectedOutcome: item.ExpectedOutcome, Priority: item.Priority})
	}
	return out
}

func successCriteriaToDomain(items []SuccessCriterionRequest) []domain.SuccessCriterion {
	out := make([]domain.SuccessCriterion, 0, len(items))
	for _, item := range items {
		out = append(out, domain.SuccessCriterion{Title: item.Title, Description: item.Description, TargetValue: item.TargetValue, Priority: item.Priority})
	}
	return out
}

type LifecycleRequest struct {
	Reason string `json:"reason"`
}
