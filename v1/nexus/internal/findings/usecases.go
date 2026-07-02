package findings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"

	domain "github.com/devpablocristo/nexus/internal/findings/usecases/domain"
)

const resourceFindingRule = "finding_rule"

type CreateRuleInput struct {
	OrgID          string
	OwnerSystem    string
	SourceSystem   string
	FactType       string
	Code           string
	Name           string
	Description    string
	Expression     string
	Severity       string
	Title          string
	Message        string
	Recommendation string
	Mode           domain.RuleMode
	Enabled        *bool
	Priority       int
}

type UpdateRuleInput struct {
	ID             uuid.UUID
	OwnerSystem    string
	SourceSystem   string
	FactType       string
	Code           string
	Name           string
	Description    string
	Expression     string
	Severity       string
	Title          string
	Message        string
	Recommendation string
	Mode           domain.RuleMode
	Enabled        *bool
	Priority       int
}

type SubmitFactsInput struct {
	OrgID         string
	OwnerSystem   string
	SourceSystem  string
	FactType      string
	SourceEventID string
	SubjectType   string
	SubjectID     string
	Facts         map[string]any
}

type SubmitFactsOutput struct {
	Evaluation domain.FactEvaluation
	Findings   []domain.Finding
}

type ListRulesInput = RuleFilter
type ListFindingsInput = FindingFilter

type Usecases struct {
	repo      Repository
	evaluator *Evaluator
	lifecycle *lifecycle.Service
	now       func() time.Time
}

func NewUsecases(repo Repository, evaluator *Evaluator) *Usecases {
	if evaluator == nil {
		evaluator = NewEvaluator()
	}
	lifecycleSvc, err := lifecycle.NewServiceWithRepos(
		map[string]lifecycle.RepositoryPort{resourceFindingRule: repo},
		noopLifecycleAudit{},
		lifecycle.NewStaticPolicyRegistry(&lifecycle.ArchivePolicy{
			ResourceType:    resourceFindingRule,
			AllowArchive:    true,
			AllowHardDelete: true,
		}),
	)
	if err != nil {
		panic(err)
	}
	return &Usecases{repo: repo, evaluator: evaluator, lifecycle: lifecycleSvc, now: func() time.Time { return time.Now().UTC() }}
}

func (uc *Usecases) UpsertRule(ctx context.Context, in CreateRuleInput) (domain.FindingRule, error) {
	rule, err := uc.ruleFromCreate(in)
	if err != nil {
		return domain.FindingRule{}, err
	}
	if err := uc.evaluator.Validate(rule.Expression); err != nil {
		return domain.FindingRule{}, fmt.Errorf("invalid finding rule expression: %w", err)
	}
	return uc.repo.UpsertRule(ctx, rule)
}

func (uc *Usecases) GetRule(ctx context.Context, id uuid.UUID) (domain.FindingRule, error) {
	return uc.repo.GetRule(ctx, id)
}

func (uc *Usecases) ListRules(ctx context.Context, in ListRulesInput) ([]domain.FindingRule, error) {
	in.OrgID = strings.TrimSpace(in.OrgID)
	if in.OrgID == "" {
		return nil, errors.New("org_id is required")
	}
	in.OwnerSystem = strings.TrimSpace(in.OwnerSystem)
	in.SourceSystem = strings.TrimSpace(in.SourceSystem)
	in.FactType = strings.TrimSpace(in.FactType)
	return uc.repo.ListRules(ctx, in)
}

func (uc *Usecases) UpdateRule(ctx context.Context, in UpdateRuleInput) (domain.FindingRule, error) {
	current, err := uc.repo.GetRule(ctx, in.ID)
	if err != nil {
		return domain.FindingRule{}, err
	}
	current.OwnerSystem = firstNonEmpty(in.OwnerSystem, current.OwnerSystem)
	current.SourceSystem = firstNonEmpty(in.SourceSystem, current.SourceSystem)
	current.FactType = firstNonEmpty(in.FactType, current.FactType)
	current.Code = firstNonEmpty(in.Code, current.Code)
	current.Name = firstNonEmpty(in.Name, current.Name)
	current.Description = in.Description
	current.Expression = firstNonEmpty(in.Expression, current.Expression)
	current.Severity = firstNonEmpty(in.Severity, current.Severity)
	current.Title = firstNonEmpty(in.Title, current.Title)
	current.Message = firstNonEmpty(in.Message, current.Message)
	current.Recommendation = in.Recommendation
	if in.Mode != "" {
		current.Mode = in.Mode
	}
	if in.Enabled != nil {
		current.Enabled = *in.Enabled
	}
	if in.Priority != 0 {
		current.Priority = in.Priority
	}
	if err := validateRule(current); err != nil {
		return domain.FindingRule{}, err
	}
	if err := uc.evaluator.Validate(current.Expression); err != nil {
		return domain.FindingRule{}, fmt.Errorf("invalid finding rule expression: %w", err)
	}
	return uc.repo.UpdateRule(ctx, current)
}

func (uc *Usecases) ArchiveRule(ctx context.Context, orgID, actor string, id uuid.UUID) error {
	return uc.lifecycle.SoftDelete(ctx, &lifecycle.ArchiveRequest{
		ResourceType: resourceFindingRule,
		ResourceID:   id,
		TenantID:     strings.TrimSpace(orgID),
		Actor:        strings.TrimSpace(actor),
	})
}

func (uc *Usecases) RestoreRule(ctx context.Context, orgID, actor string, id uuid.UUID) error {
	return uc.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: resourceFindingRule,
		ResourceID:   id,
		TenantID:     strings.TrimSpace(orgID),
		Actor:        strings.TrimSpace(actor),
	})
}

func (uc *Usecases) HardDeleteRule(ctx context.Context, orgID, actor string, id uuid.UUID) error {
	return uc.lifecycle.HardDelete(ctx, &lifecycle.HardDeleteRequest{
		ResourceType:   resourceFindingRule,
		ResourceID:     id,
		TenantID:       strings.TrimSpace(orgID),
		Actor:          strings.TrimSpace(actor),
		MustBeArchived: true,
	})
}

func (uc *Usecases) SubmitFacts(ctx context.Context, in SubmitFactsInput) (SubmitFactsOutput, error) {
	evaluation, err := uc.evaluationFromInput(in)
	if err != nil {
		return SubmitFactsOutput{}, err
	}
	rules, err := uc.repo.ListRules(ctx, RuleFilter{
		OrgID:        evaluation.OrgID,
		OwnerSystem:  evaluation.OwnerSystem,
		SourceSystem: evaluation.SourceSystem,
		FactType:     evaluation.FactType,
	})
	if err != nil {
		return SubmitFactsOutput{}, err
	}

	now := uc.now()
	source := map[string]any{
		"owner_system":    evaluation.OwnerSystem,
		"source_system":   evaluation.SourceSystem,
		"fact_type":       evaluation.FactType,
		"source_event_id": evaluation.SourceEventID,
		"subject_type":    evaluation.SubjectType,
		"subject_id":      evaluation.SubjectID,
	}
	items := make([]domain.Finding, 0)
	for _, rule := range rules {
		if !rule.Enabled || rule.ArchivedAt != nil {
			continue
		}
		matched, err := uc.evaluator.Matches(rule.Expression, evaluation.Facts, source, now)
		if err != nil {
			return SubmitFactsOutput{}, fmt.Errorf("evaluate rule %s: %w", rule.Code, err)
		}
		if !matched {
			continue
		}
		status := domain.FindingStatusOpen
		if rule.Mode == domain.RuleModeShadow {
			status = domain.FindingStatusShadow
		}
		items = append(items, domain.Finding{
			OrgID:          evaluation.OrgID,
			RuleID:         rule.ID,
			OwnerSystem:    rule.OwnerSystem,
			SourceSystem:   evaluation.SourceSystem,
			FactType:       evaluation.FactType,
			SourceEventID:  evaluation.SourceEventID,
			SubjectType:    evaluation.SubjectType,
			SubjectID:      evaluation.SubjectID,
			Code:           rule.Code,
			Severity:       rule.Severity,
			Title:          rule.Title,
			Message:        rule.Message,
			Recommendation: rule.Recommendation,
			Evidence: map[string]any{
				"schema_version": "nexus.finding.v1",
				"rule": map[string]any{
					"id":         rule.ID.String(),
					"code":       rule.Code,
					"expression": rule.Expression,
					"mode":       rule.Mode,
					"severity":   rule.Severity,
				},
				"facts":  evaluation.Facts,
				"source": source,
			},
			Status:    status,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	storedEval, storedFindings, err := uc.repo.CreateEvaluationWithFindings(ctx, evaluation, items)
	if err != nil {
		return SubmitFactsOutput{}, err
	}
	return SubmitFactsOutput{Evaluation: storedEval, Findings: storedFindings}, nil
}

func (uc *Usecases) GetEvaluation(ctx context.Context, id uuid.UUID) (domain.FactEvaluation, error) {
	return uc.repo.GetEvaluation(ctx, id)
}

func (uc *Usecases) ListFindings(ctx context.Context, in ListFindingsInput) ([]domain.Finding, error) {
	in.OrgID = strings.TrimSpace(in.OrgID)
	if in.OrgID == "" {
		return nil, errors.New("org_id is required")
	}
	return uc.repo.ListFindings(ctx, in)
}

func (uc *Usecases) GetFinding(ctx context.Context, id uuid.UUID) (domain.Finding, error) {
	return uc.repo.GetFinding(ctx, id)
}

func (uc *Usecases) UpdateFindingStatus(ctx context.Context, id uuid.UUID, status domain.FindingStatus, note string) (domain.Finding, error) {
	status = domain.FindingStatus(strings.TrimSpace(string(status)))
	switch status {
	case domain.FindingStatusOpen, domain.FindingStatusAcknowledged, domain.FindingStatusResolved, domain.FindingStatusDismissed, domain.FindingStatusShadow:
	default:
		return domain.Finding{}, errors.New("invalid finding status")
	}
	return uc.repo.UpdateFindingStatus(ctx, id, status, strings.TrimSpace(note))
}

func (uc *Usecases) ruleFromCreate(in CreateRuleInput) (domain.FindingRule, error) {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	rule := domain.FindingRule{
		OrgID:          strings.TrimSpace(in.OrgID),
		OwnerSystem:    strings.TrimSpace(in.OwnerSystem),
		SourceSystem:   strings.TrimSpace(in.SourceSystem),
		FactType:       strings.TrimSpace(in.FactType),
		Code:           strings.TrimSpace(in.Code),
		Name:           strings.TrimSpace(in.Name),
		Description:    strings.TrimSpace(in.Description),
		Expression:     strings.TrimSpace(in.Expression),
		Severity:       strings.TrimSpace(in.Severity),
		Title:          strings.TrimSpace(in.Title),
		Message:        strings.TrimSpace(in.Message),
		Recommendation: strings.TrimSpace(in.Recommendation),
		Mode:           in.Mode,
		Enabled:        enabled,
		Priority:       in.Priority,
	}
	if rule.Mode == "" {
		rule.Mode = domain.RuleModeEnforced
	}
	if rule.Priority == 0 {
		rule.Priority = 100
	}
	if err := validateRule(rule); err != nil {
		return domain.FindingRule{}, err
	}
	return rule, nil
}

func (uc *Usecases) evaluationFromInput(in SubmitFactsInput) (domain.FactEvaluation, error) {
	evaluation := domain.FactEvaluation{
		ID:            uuid.New(),
		OrgID:         strings.TrimSpace(in.OrgID),
		OwnerSystem:   strings.TrimSpace(in.OwnerSystem),
		SourceSystem:  strings.TrimSpace(in.SourceSystem),
		FactType:      strings.TrimSpace(in.FactType),
		SourceEventID: strings.TrimSpace(in.SourceEventID),
		SubjectType:   strings.TrimSpace(in.SubjectType),
		SubjectID:     strings.TrimSpace(in.SubjectID),
		Facts:         in.Facts,
		CreatedAt:     uc.now(),
	}
	if evaluation.Facts == nil {
		evaluation.Facts = map[string]any{}
	}
	if evaluation.OrgID == "" || evaluation.OwnerSystem == "" || evaluation.SourceSystem == "" ||
		evaluation.FactType == "" || evaluation.SourceEventID == "" {
		return domain.FactEvaluation{}, errors.New("org_id, owner_system, source_system, fact_type and source_event_id are required")
	}
	return evaluation, nil
}

func validateRule(rule domain.FindingRule) error {
	if rule.OrgID == "" || rule.OwnerSystem == "" || rule.SourceSystem == "" ||
		rule.FactType == "" || rule.Code == "" || rule.Name == "" ||
		rule.Expression == "" || rule.Severity == "" || rule.Title == "" || rule.Message == "" {
		return errors.New("org_id, owner_system, source_system, fact_type, code, name, expression, severity, title and message are required")
	}
	switch rule.Mode {
	case domain.RuleModeEnforced, domain.RuleModeShadow:
	default:
		return errors.New("invalid rule mode")
	}
	switch rule.Severity {
	case "info", "warning", "critical":
	default:
		return errors.New("invalid rule severity")
	}
	return nil
}

func firstNonEmpty(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

type noopLifecycleAudit struct{}

func (noopLifecycleAudit) Append(context.Context, lifecycle.ArchiveAudit) error {
	return nil
}
