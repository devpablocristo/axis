package contracts

import (
	"context"
	"fmt"
	"strings"

	domain "github.com/devpablocristo/nexus/internal/contracts/usecases/domain"
)

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

type ValidateInput struct {
	OrgID       *string
	Name        string
	SubjectType string
	SubjectID   string
	Payload     map[string]any
}

type ValidateOutput struct {
	ContractName    string   `json:"contract_name"`
	ContractVersion string   `json:"contract_version"`
	Mode            string   `json:"mode"`
	Valid           bool     `json:"valid"`
	Errors          []string `json:"errors,omitempty"`
	PayloadHash     string   `json:"payload_hash"`
}

func (u *Usecases) Upsert(ctx context.Context, contract domain.Contract) (domain.Contract, error) {
	if strings.TrimSpace(contract.Name) == "" || strings.TrimSpace(contract.Version) == "" {
		return domain.Contract{}, fmt.Errorf("contract name and version are required")
	}
	if strings.TrimSpace(contract.SubjectType) == "" {
		contract.SubjectType = "json"
	}
	if contract.Schema == nil {
		contract.Schema = make(map[string]any)
	}
	if errs := ValidateSchema(map[string]any{}, map[string]any{"type": "object"}); len(errs) > 0 {
		return domain.Contract{}, fmt.Errorf("schema validator unavailable")
	}
	return u.repo.Upsert(ctx, contract)
}

func (u *Usecases) List(ctx context.Context, orgID *string, includeGlobal bool) ([]domain.Contract, error) {
	return u.repo.List(ctx, orgID, includeGlobal)
}

func (u *Usecases) Validate(ctx context.Context, in ValidateInput) (ValidateOutput, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ValidateOutput{}, fmt.Errorf("contract name is required")
	}
	payload := in.Payload
	if payload == nil {
		payload = make(map[string]any)
	}
	contract, err := u.repo.GetActive(ctx, name, in.OrgID)
	if err != nil {
		return ValidateOutput{}, err
	}
	errs := ValidateSchema(payload, contract.Schema)
	payloadHash, err := HashPayload(payload)
	if err != nil {
		return ValidateOutput{}, err
	}
	out := ValidateOutput{
		ContractName:    contract.Name,
		ContractVersion: contract.Version,
		Mode:            string(contract.ValidationMode),
		Valid:           len(errs) == 0,
		Errors:          errs,
		PayloadHash:     payloadHash,
	}
	report := domain.ValidationReport{
		OrgID:           in.OrgID,
		ContractName:    contract.Name,
		ContractVersion: contract.Version,
		SubjectType:     firstNonEmpty(in.SubjectType, contract.SubjectType),
		SubjectID:       in.SubjectID,
		Mode:            contract.ValidationMode,
		Valid:           out.Valid,
		Errors:          errs,
		PayloadHash:     payloadHash,
	}
	if recordErr := u.repo.RecordValidation(ctx, report); recordErr != nil {
		return ValidateOutput{}, recordErr
	}
	if !out.Valid && contract.ValidationMode == domain.ValidationModeEnforce {
		return out, fmt.Errorf("contract %s %s validation failed: %s", contract.Name, contract.Version, strings.Join(errs, "; "))
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
