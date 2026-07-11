package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) RecordExecutionResult(ctx context.Context, tenantID, checkID string, input domain.ExecutionResultInput) (domain.ExecutionResult, error) {
	parsedCheckID, err := uuid.Parse(strings.TrimSpace(checkID))
	if err != nil {
		return domain.ExecutionResult{}, domainerr.Validation("invalid governance check id")
	}
	var bindingHash, decision, approvalStatus string
	err = r.pool.QueryRow(ctx, `
		SELECT c.binding_hash, c.decision, COALESCE(a.status, '')
		FROM governance_checks c
		LEFT JOIN approvals a ON a.governance_check_id = c.id AND a.tenant_id = c.tenant_id
		WHERE c.tenant_id = $1 AND c.id = $2
	`, tenantID, parsedCheckID).Scan(&bindingHash, &decision, &approvalStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.ExecutionResult{}, domainerr.NotFound("governance check not found")
		}
		return domain.ExecutionResult{}, err
	}
	if bindingHash == "" || bindingHash != input.BindingHash {
		return domain.ExecutionResult{}, domainerr.Conflict("execution result binding does not match governance check")
	}
	if decision == "deny" || (decision == "require_approval" && approvalStatus != "approved") {
		return domain.ExecutionResult{}, domainerr.Conflict("governance check is not executable")
	}
	raw, err := json.Marshal(input.Result)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	fingerprint := resultFingerprint(input, raw)
	id := uuid.New()
	now := time.Now().UTC()
	_, err = r.pool.Exec(ctx, `
		INSERT INTO governance_execution_results (
			id, tenant_id, governance_check_id, idempotency_key, request_fingerprint,
			binding_hash, status, duration_ms, result, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $10)
		ON CONFLICT (tenant_id, governance_check_id) DO NOTHING
	`, id, tenantID, parsedCheckID, input.IdempotencyKey, fingerprint, input.BindingHash, input.Status, input.DurationMS, raw, now)
	if err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("record execution result: %w", err)
	}
	var out domain.ExecutionResult
	var storedFingerprint string
	var storedRaw []byte
	err = r.pool.QueryRow(ctx, `
		SELECT id::text, governance_check_id::text, request_fingerprint, binding_hash, status, duration_ms, result
		FROM governance_execution_results
		WHERE tenant_id = $1 AND governance_check_id = $2
	`, tenantID, parsedCheckID).Scan(&out.ID, &out.GovernanceCheckID, &storedFingerprint, &out.BindingHash, &out.Status, &out.DurationMS, &storedRaw)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	if storedFingerprint != fingerprint {
		return domain.ExecutionResult{}, domainerr.Conflict("execution result already exists with different payload")
	}
	if err := json.Unmarshal(storedRaw, &out.Result); err != nil {
		return domain.ExecutionResult{}, err
	}
	return out, nil
}

func resultFingerprint(input domain.ExecutionResultInput, raw []byte) string {
	value := fmt.Sprintf("%s\n%s\n%d\n%s", input.BindingHash, input.Status, input.DurationMS, raw)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
