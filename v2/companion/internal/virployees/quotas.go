package virployees

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/quotas"
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
