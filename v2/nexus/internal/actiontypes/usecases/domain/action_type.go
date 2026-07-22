package domain

import (
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RiskClass string

const (
	RiskClassLow    RiskClass = "low"
	RiskClassMedium RiskClass = "medium"
	RiskClassHigh   RiskClass = "high"
	RiskClassCritical RiskClass = "critical"
)

type ActionType struct {
	ID            uuid.UUID
	TenantID      string
	ActionTypeKey string
	Name          string
	Description   string
	Category      string
	RiskClass     RiskClass
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateInput struct {
	ActionTypeKey string
	Name          string
	Description   string
	Category      string
	RiskClass     string
	Enabled       *bool
}

type UpdateInput struct {
	Name        string
	Description string
	Category    string
	RiskClass   string
	Enabled     *bool
}

type NormalizedCreateInput struct {
	ActionTypeKey string
	Name          string
	Description   string
	Category      string
	RiskClass     RiskClass
	Enabled       bool
}

type NormalizedUpdateInput struct {
	Name        string
	Description string
	Category    string
	RiskClass   RiskClass
	Enabled     bool
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	key := strings.TrimSpace(in.ActionTypeKey)
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	category := strings.TrimSpace(in.Category)
	riskClass, err := normalizeRiskClass(in.RiskClass)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if !validActionTypeKey(key) {
		return NormalizedCreateInput{}, domainerr.Validation("action_type_key must use lowercase dotted segments")
	}
	if name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	return NormalizedCreateInput{
		ActionTypeKey: key,
		Name:          name,
		Description:   description,
		Category:      category,
		RiskClass:     riskClass,
		Enabled:       enabled,
	}, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	category := strings.TrimSpace(in.Category)
	riskClass, err := normalizeRiskClass(in.RiskClass)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if name == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("name is required")
	}
	return NormalizedUpdateInput{
		Name:        name,
		Description: description,
		Category:    category,
		RiskClass:   riskClass,
		Enabled:     enabled,
	}, nil
}

var actionTypeKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

func validActionTypeKey(value string) bool {
	return actionTypeKeyPattern.MatchString(value)
}

func normalizeRiskClass(raw string) (RiskClass, error) {
	value := RiskClass(strings.TrimSpace(strings.ToLower(raw)))
	if value == "" {
		return RiskClassLow, nil
	}
	switch value {
	case RiskClassLow, RiskClassMedium, RiskClassHigh, RiskClassCritical:
		return value, nil
	default:
		return "", domainerr.Validation("risk_class must be one of low, medium, high, critical")
	}
}
