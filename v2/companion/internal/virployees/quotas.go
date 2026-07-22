package virployees

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/google/uuid"
)

func (u *UseCases) consumeQuota(ctx context.Context, key quotas.Key, idempotencyKey, subjectType, subjectID string, units int64) error {
	if u.quota == nil {
		return nil
	}
	_, err := u.quota.Consume(ctx, quotas.ConsumeRequest{
		Key: key, IdempotencyKey: idempotencyKey, SubjectType: subjectType, SubjectID: subjectID, Units: units,
	})
	return err
}

func quotaKey(tenantID, productSurface, area string) quotas.Key {
	productSurface = strings.TrimSpace(productSurface)
	if productSurface == "" {
		productSurface = "axis"
	}
	return quotas.Key{TenantID: normalizeTenantID(tenantID), ProductSurface: productSurface, Area: area}
}

func estimatedAnswerTokens(input json.RawMessage, parts []artifacts.ContentPart) int64 {
	bytes := int64(len(input))
	for _, part := range parts {
		bytes += int64(len(part.Text) + len(part.Data) + len(part.URI))
	}
	// Reserve the configured answer ceiling in addition to a conservative
	// four-characters-per-token input estimate. This fails before paid work.
	return (bytes+3)/4 + 4096
}

func (u *UseCases) recordLLMUsage(ctx context.Context, run AssistRun, output AnswerOutput) {
	if u.usageLedger == nil {
		return
	}
	_ = u.usageLedger.RecordUsage(ctx, quotas.Usage{
		Key:            quotaKey(run.TenantID, run.ProductSurface, quotas.AreaLLM),
		IdempotencyKey: run.ID.String() + ":actual", SubjectType: "assist_run", SubjectID: run.ID.String(),
		Units: output.InputTokens + output.OutputTokens, Model: output.ModelID,
		EstimatedCostMicroUSD: output.EstimatedCostMicroUSD,
		Metadata:              map[string]any{"input_tokens": output.InputTokens, "output_tokens": output.OutputTokens, "estimated": true},
	})
}

func estimatedDryRunTokens(input string, runtimeCtx runtimecontext.Context) int64 {
	bytes := int64(len(input) + len(runtimeCtx.ProfileTemplate.SystemPrompt) + len(runtimeCtx.JobRole.Name))
	for _, capability := range runtimeCtx.Capabilities {
		bytes += int64(len(capability.CapabilityKey) + len(capability.Name) + len(capability.Description))
	}
	for _, memory := range runtimeCtx.MemoryContext {
		bytes += int64(len(memory.Title) + len(memory.Type) + len(memory.Content))
	}
	return (bytes+3)/4 + 2048
}

func (u *UseCases) recordProposalUsage(ctx context.Context, tenantID string, virployeeID uuid.UUID, idempotencyKey string, proposal dryrun.Proposal) {
	if u.usageLedger == nil {
		return
	}
	_ = u.usageLedger.RecordUsage(ctx, quotas.Usage{
		Key: quotaKey(tenantID, "axis", quotas.AreaLLM), IdempotencyKey: idempotencyKey + ":actual",
		SubjectType: "virployee", SubjectID: virployeeID.String(), Units: proposal.InputTokens + proposal.OutputTokens,
		Model: proposal.Intent.ModelID, EstimatedCostMicroUSD: proposal.EstimatedCostMicroUSD,
		Metadata: map[string]any{"input_tokens": proposal.InputTokens, "output_tokens": proposal.OutputTokens, "estimated": true},
	})
}
