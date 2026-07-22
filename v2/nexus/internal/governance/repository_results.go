package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/attestation"
	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) RecordExecutionResult(ctx context.Context, orgID, checkID string, input domain.ExecutionResultInput) (domain.ExecutionResult, error) {
	parsedCheckID, err := uuid.Parse(strings.TrimSpace(checkID))
	if err != nil {
		return domain.ExecutionResult{}, domainerr.Validation("invalid governance check id")
	}
	var bindingHash, decision, approvalStatus, requesterID string
	err = r.pool.QueryRow(ctx, `
		SELECT c.binding_hash, c.decision, COALESCE(a.status, ''), c.requester_id
		FROM governance_checks c
		LEFT JOIN approvals a ON a.governance_check_id = c.id AND a.org_id = c.org_id
		WHERE c.org_id = $1 AND c.id = $2
	`, orgID, parsedCheckID).Scan(&bindingHash, &decision, &approvalStatus, &requesterID)
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
	resultHash, err := attestation.ResultHash(input.Result)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	id := uuid.New()
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO governance_execution_results (
			id, org_id, governance_check_id, idempotency_key, request_fingerprint,
			binding_hash, status, duration_ms, result, attestation_version,
			executor_version, attestation, result_hash, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13, $14, $14)
		ON CONFLICT (org_id, governance_check_id) DO NOTHING
	`, id, orgID, parsedCheckID, input.IdempotencyKey, fingerprint, input.BindingHash, input.Status, input.DurationMS, raw, input.AttestationVersion, input.ExecutorVersion, input.Attestation, resultHash, now)
	if err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("record execution result: %w", err)
	}
	created := tag.RowsAffected() == 1
	var out domain.ExecutionResult
	var storedFingerprint string
	var storedRaw []byte
	err = r.pool.QueryRow(ctx, `
		SELECT id::text, governance_check_id::text, request_fingerprint, binding_hash, status, duration_ms, result,
		       idempotency_key, attestation_version, executor_version, attestation, result_hash
		FROM governance_execution_results
		WHERE org_id = $1 AND governance_check_id = $2
	`, orgID, parsedCheckID).Scan(&out.ID, &out.GovernanceCheckID, &storedFingerprint, &out.BindingHash, &out.Status, &out.DurationMS, &storedRaw,
		&out.IdempotencyKey, &out.AttestationVersion, &out.ExecutorVersion, &out.Attestation, &out.ResultHash)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	if storedFingerprint != fingerprint {
		return domain.ExecutionResult{}, domainerr.Conflict("execution result already exists with different payload")
	}
	if err := json.Unmarshal(storedRaw, &out.Result); err != nil {
		return domain.ExecutionResult{}, err
	}
	out.RequesterID = requesterID
	out.Created = created
	return out, nil
}

func resultFingerprint(input domain.ExecutionResultInput, raw []byte) string {
	value := fmt.Sprintf("%s\n%s\n%d\n%s\n%s\n%s", input.BindingHash, input.Status, input.DurationMS, raw, input.AttestationVersion, input.ExecutorVersion)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
