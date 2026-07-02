package assist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/assist/usecases/domain"
)

const resourceAssistPack = "assist_pack"

type UpsertPackInput struct {
	OrgID          string
	OwnerSystem    string
	ProductSurface string
	AssistType     string
	Name           string
	Description    string
	PromptTemplate string
	ModelPolicy    map[string]any
	OutputSchema   map[string]any
	Enabled        *bool
}

type UpdatePackInput struct {
	ID             uuid.UUID
	OwnerSystem    string
	ProductSurface string
	AssistType     string
	Name           string
	Description    string
	PromptTemplate string
	ModelPolicy    map[string]any
	OutputSchema   map[string]any
	Enabled        *bool
}

type RunAssistInput struct {
	OrgID          string
	OwnerSystem    string
	ProductSurface string
	AssistType     string
	SubjectType    string
	SubjectID      string
	Input          map[string]any
	IdempotencyKey string
}

type ListPacksInput = PackFilter
type ListRunsInput = RunFilter

type Usecases struct {
	repo      Repository
	provider  runtime.LLMProvider
	lifecycle *lifecycle.Service
	now       func() time.Time
}

// NewUsecases builds the assist usecases. audit persists lifecycle
// (archive/restore/hard-delete) events; pass nil to disable auditing (falls back
// to a no-op). Returns an error instead of panicking so the caller controls
// startup failure.
func NewUsecases(repo Repository, provider runtime.LLMProvider, audit lifecycle.AuditPort) (*Usecases, error) {
	if audit == nil {
		audit = noopLifecycleAudit{}
	}
	lifecycleSvc, err := lifecycle.NewServiceWithRepos(
		map[string]lifecycle.RepositoryPort{resourceAssistPack: repo},
		audit,
		lifecycle.NewStaticPolicyRegistry(&lifecycle.ArchivePolicy{
			ResourceType:    resourceAssistPack,
			AllowArchive:    true,
			AllowHardDelete: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("assist: build lifecycle service: %w", err)
	}
	return &Usecases{repo: repo, provider: provider, lifecycle: lifecycleSvc, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (uc *Usecases) UpsertPack(ctx context.Context, in UpsertPackInput) (domain.AssistPack, error) {
	pack, err := uc.packFromInput(in)
	if err != nil {
		return domain.AssistPack{}, err
	}
	// Refuse to silently un-archive: if the logical pack exists and is archived,
	// the caller must Restore it deliberately. The repo upsert no longer resets
	// archived_at, so this is the single guard against reviving retired packs.
	existing, err := uc.repo.GetPackByTypeIncludingArchived(ctx, pack.OrgID, pack.OwnerSystem, pack.ProductSurface, pack.AssistType)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return domain.AssistPack{}, err
	}
	if err == nil && existing.ArchivedAt != nil {
		return domain.AssistPack{}, domainerr.Conflict("assist pack is archived; restore it before upserting")
	}
	return uc.repo.UpsertPack(ctx, pack)
}

func (uc *Usecases) GetPack(ctx context.Context, id uuid.UUID) (domain.AssistPack, error) {
	return uc.repo.GetPack(ctx, id)
}

func (uc *Usecases) ListPacks(ctx context.Context, in ListPacksInput) ([]domain.AssistPack, error) {
	in.OrgID = strings.TrimSpace(in.OrgID)
	if in.OrgID == "" {
		return nil, errors.New("org_id is required")
	}
	return uc.repo.ListPacks(ctx, in)
}

func (uc *Usecases) UpdatePack(ctx context.Context, in UpdatePackInput) (domain.AssistPack, error) {
	current, err := uc.repo.GetPack(ctx, in.ID)
	if err != nil {
		return domain.AssistPack{}, err
	}
	current.OwnerSystem = firstNonEmpty(in.OwnerSystem, current.OwnerSystem)
	current.ProductSurface = firstNonEmpty(in.ProductSurface, current.ProductSurface)
	current.AssistType = firstNonEmpty(in.AssistType, current.AssistType)
	current.Name = firstNonEmpty(in.Name, current.Name)
	current.Description = in.Description
	current.PromptTemplate = firstNonEmpty(in.PromptTemplate, current.PromptTemplate)
	if in.ModelPolicy != nil {
		current.ModelPolicy = in.ModelPolicy
	}
	if in.OutputSchema != nil {
		current.OutputSchema = in.OutputSchema
	}
	if in.Enabled != nil {
		current.Enabled = *in.Enabled
	}
	if err := validatePack(current); err != nil {
		return domain.AssistPack{}, err
	}
	return uc.repo.UpdatePack(ctx, current)
}

func (uc *Usecases) ArchivePack(ctx context.Context, orgID, actor string, id uuid.UUID) error {
	return uc.lifecycle.SoftDelete(ctx, &lifecycle.ArchiveRequest{
		ResourceType: resourceAssistPack,
		ResourceID:   id,
		TenantID:     strings.TrimSpace(orgID),
		Actor:        strings.TrimSpace(actor),
	})
}

func (uc *Usecases) RestorePack(ctx context.Context, orgID, actor string, id uuid.UUID) error {
	return uc.lifecycle.Restore(ctx, &lifecycle.RestoreRequest{
		ResourceType: resourceAssistPack,
		ResourceID:   id,
		TenantID:     strings.TrimSpace(orgID),
		Actor:        strings.TrimSpace(actor),
	})
}

func (uc *Usecases) HardDeletePack(ctx context.Context, orgID, actor string, id uuid.UUID) error {
	return uc.lifecycle.HardDelete(ctx, &lifecycle.HardDeleteRequest{
		ResourceType:   resourceAssistPack,
		ResourceID:     id,
		TenantID:       strings.TrimSpace(orgID),
		Actor:          strings.TrimSpace(actor),
		MustBeArchived: true,
	})
}

func (uc *Usecases) RunAssist(ctx context.Context, in RunAssistInput) (domain.AssistRun, error) {
	in.OrgID = strings.TrimSpace(in.OrgID)
	in.OwnerSystem = strings.TrimSpace(in.OwnerSystem)
	in.ProductSurface = strings.TrimSpace(in.ProductSurface)
	in.AssistType = strings.TrimSpace(in.AssistType)
	if in.OrgID == "" || in.OwnerSystem == "" || in.ProductSurface == "" || in.AssistType == "" {
		return domain.AssistRun{}, domainerr.Validation("org_id, owner_system, product_surface and assist_type are required")
	}
	if in.Input == nil {
		in.Input = map[string]any{}
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)

	// Idempotent replay: a run already created for this key is returned as-is,
	// without re-invoking the LLM.
	if in.IdempotencyKey != "" {
		existing, err := uc.repo.GetRunByIdempotencyKey(ctx, in.OrgID, in.IdempotencyKey)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return domain.AssistRun{}, err
		}
	}

	pack, err := uc.repo.GetPackByType(ctx, in.OrgID, in.OwnerSystem, in.ProductSurface, in.AssistType)
	if err != nil {
		return domain.AssistRun{}, err
	}
	if !pack.Enabled {
		return domain.AssistRun{}, domainerr.Validation("assist pack is disabled")
	}

	now := uc.now()
	run := domain.AssistRun{
		ID:             uuid.New(),
		OrgID:          in.OrgID,
		PackID:         pack.ID,
		OwnerSystem:    pack.OwnerSystem,
		ProductSurface: pack.ProductSurface,
		AssistType:     pack.AssistType,
		SubjectType:    strings.TrimSpace(in.SubjectType),
		SubjectID:      strings.TrimSpace(in.SubjectID),
		Input:          in.Input,
		Status:         "running",
		IdempotencyKey: in.IdempotencyKey,
		CreatedAt:      now,
	}

	if in.IdempotencyKey != "" {
		// Reserve the run BEFORE calling the LLM so concurrent duplicates collide
		// on the partial unique index rather than both invoking the model.
		reserved, err := uc.repo.CreateRun(ctx, run)
		if err != nil {
			if domainerr.IsKind(err, domainerr.KindConflict) {
				if existing, getErr := uc.repo.GetRunByIdempotencyKey(ctx, in.OrgID, in.IdempotencyKey); getErr == nil {
					return existing, nil
				}
			}
			return domain.AssistRun{}, err
		}
		output, llmErr := uc.runLLM(ctx, pack, in.Input)
		completedAt := uc.now()
		if llmErr != nil {
			updated, updErr := uc.repo.UpdateRunResult(ctx, reserved.ID, "failed", map[string]any{}, llmErr.Error(), completedAt)
			if updErr != nil {
				return domain.AssistRun{}, updErr
			}
			return updated, llmErr
		}
		return uc.repo.UpdateRunResult(ctx, reserved.ID, "completed", output, "", completedAt)
	}

	// Keyless: run then persist the final row (no reservation).
	output, err := uc.runLLM(ctx, pack, in.Input)
	completedAt := uc.now()
	run.CompletedAt = &completedAt
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		run.Output = map[string]any{}
		stored, storeErr := uc.repo.CreateRun(ctx, run)
		if storeErr != nil {
			return domain.AssistRun{}, storeErr
		}
		return stored, err
	}
	run.Status = "completed"
	run.Output = output
	return uc.repo.CreateRun(ctx, run)
}

func (uc *Usecases) GetRun(ctx context.Context, id uuid.UUID) (domain.AssistRun, error) {
	return uc.repo.GetRun(ctx, id)
}

func (uc *Usecases) ListRuns(ctx context.Context, in ListRunsInput) ([]domain.AssistRun, error) {
	in.OrgID = strings.TrimSpace(in.OrgID)
	if in.OrgID == "" {
		return nil, errors.New("org_id is required")
	}
	return uc.repo.ListRuns(ctx, in)
}

func (uc *Usecases) packFromInput(in UpsertPackInput) (domain.AssistPack, error) {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	pack := domain.AssistPack{
		ID:             uuid.New(),
		OrgID:          strings.TrimSpace(in.OrgID),
		OwnerSystem:    strings.TrimSpace(in.OwnerSystem),
		ProductSurface: strings.TrimSpace(in.ProductSurface),
		AssistType:     strings.TrimSpace(in.AssistType),
		Name:           strings.TrimSpace(in.Name),
		Description:    strings.TrimSpace(in.Description),
		PromptTemplate: strings.TrimSpace(in.PromptTemplate),
		ModelPolicy:    in.ModelPolicy,
		OutputSchema:   in.OutputSchema,
		Enabled:        enabled,
	}
	if pack.ModelPolicy == nil {
		pack.ModelPolicy = map[string]any{}
	}
	if pack.OutputSchema == nil {
		pack.OutputSchema = map[string]any{}
	}
	if err := validatePackForCreate(pack); err != nil {
		return domain.AssistPack{}, err
	}
	return pack, nil
}

func (uc *Usecases) runLLM(ctx context.Context, pack domain.AssistPack, input map[string]any) (map[string]any, error) {
	if uc.provider == nil {
		return nil, errors.New("llm provider is not configured")
	}
	inputJSON, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal assist input: %w", err)
	}
	prompt := strings.TrimSpace(pack.PromptTemplate)
	if strings.Contains(prompt, "{{input_json}}") {
		prompt = strings.ReplaceAll(prompt, "{{input_json}}", string(inputJSON))
	} else {
		prompt += "\n\nInput JSON:\n" + string(inputJSON)
	}
	maxTokens := 1024
	if raw, ok := pack.ModelPolicy["max_tokens"].(float64); ok && raw > 0 {
		maxTokens = int(raw)
	}
	chatReq := runtime.ChatRequest{
		SystemPrompt: "You are Companion assist-packs. Explain the provided product-owned facts and findings. Do not create deterministic rules or decide policy outcomes.",
		Messages: []runtime.LLMMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: maxTokens,
	}
	// Structured output: si el pack trae output_schema, forzamos al provider a
	// devolver JSON conforme. Vacío → texto libre + parseo best-effort como antes.
	if len(pack.OutputSchema) > 0 {
		chatReq.ResponseSchema = pack.OutputSchema
	}
	resp, err := uc.provider.Chat(ctx, chatReq)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(resp.Text)
	if text == "" {
		text = "No response generated."
	}
	if output, ok := parseLLMOutput(text); ok {
		return output, nil
	}
	return map[string]any{
		"summary":  text,
		"raw_text": text,
	}, nil
}

func validatePack(pack domain.AssistPack) error {
	if pack.OrgID == "" || pack.OwnerSystem == "" || pack.ProductSurface == "" ||
		pack.AssistType == "" || pack.Name == "" || pack.PromptTemplate == "" {
		return domainerr.Validation("org_id, owner_system, product_surface, assist_type, name and prompt_template are required")
	}
	return nil
}

// validatePackForCreate adds the invariants that only apply when a pack is
// first authored. The prompt is pure natural language: the input injection is a
// technical concern the author should not deal with, so the {{input_json}}
// placeholder is NOT required. runLLM injects the structured input autonomously
// (interpolating the placeholder if the author opted to place it, otherwise
// appending it), so authors paste prompt text only.
func validatePackForCreate(pack domain.AssistPack) error {
	return validatePack(pack)
}

func parseLLMOutput(text string) (map[string]any, bool) {
	candidates := []string{strings.TrimSpace(text)}
	if unwrapped := unwrapMarkdownJSON(text); unwrapped != "" && unwrapped != candidates[0] {
		candidates = append(candidates, unwrapped)
	}
	if extracted := extractJSONObject(text); extracted != "" && extracted != candidates[0] {
		candidates = append(candidates, extracted)
	}
	for _, candidate := range candidates {
		var output map[string]any
		if err := json.Unmarshal([]byte(candidate), &output); err == nil && len(output) > 0 {
			return output, true
		}
	}
	return nil, false
}

func unwrapMarkdownJSON(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return ""
	}
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSpace(text)
	if strings.HasPrefix(strings.ToLower(text), "json") {
		text = strings.TrimSpace(text[len("json"):])
	}
	if idx := strings.LastIndex(text, "```"); idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(text[start : end+1])
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
