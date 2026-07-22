package virployees

import (
	"context"
	"strings"
	"time"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type AuthorityEvaluatorPort interface {
	EvaluateAuthority(context.Context, executiongate.AuthorityCheckInput) (executiongate.AuthorityCheckResult, error)
}

type ConversationScopeEvaluatorPort interface {
	EvaluateConversationScope(context.Context, executiongate.ConversationScopeInput) (executiongate.ConversationScopeResult, error)
}

func (u *UseCases) evaluateConversationScope(ctx context.Context, tenantID string, virployeeID, jobRoleID uuid.UUID, query string) (executiongate.ConversationScopeResult, bool) {
	evaluator, ok := u.authority.(ConversationScopeEvaluatorPort)
	if !ok || evaluator == nil {
		return executiongate.ConversationScopeResult{}, false
	}
	result, err := evaluator.EvaluateConversationScope(ctx, executiongate.ConversationScopeInput{
		TenantID: tenantID, VirployeeID: virployeeID, JobRoleID: jobRoleID, Query: query,
	})
	if err != nil {
		return executiongate.ConversationScopeResult{
			Allowed: false, Decision: "abstain", Reason: "scope_evaluation_unavailable",
		}, true
	}
	return result, true
}

type AuthorityPreparedActionRepositoryPort interface {
	BindPreparedActionAuthority(context.Context, string, uuid.UUID, uuid.UUID, string) error
}

type NexusPolicyPreparedActionRepositoryPort interface {
	BindPreparedActionNexusPolicy(context.Context, string, uuid.UUID, uuid.UUID, string) error
}

func (u *UseCases) SetAuthorityEvaluator(evaluator AuthorityEvaluatorPort) { u.authority = evaluator }

func (u *UseCases) evaluateAuthority(ctx context.Context, tenantID string, virployeeID, jobRoleID uuid.UUID, capability capabilitydomain.Capability, principal executiongate.PrincipalContext, mcp *preparedactions.MCPContextBinding) (executiongate.AuthorityCheckResult, error) {
	if u.authority == nil {
		return executiongate.AuthorityCheckResult{}, domainerr.Conflict("professional authority evaluator is not configured")
	}
	resourceType, resourceID := principal.Type, principal.ID
	if mcp != nil {
		resourceType, resourceID = "work_subject", mcp.SubjectID
		if strings.TrimSpace(mcp.CaseID) != "" {
			resourceType, resourceID = "case", mcp.CaseID
		}
	}
	return u.authority.EvaluateAuthority(ctx, executiongate.AuthorityCheckInput{
		TenantID: tenantID, VirployeeID: virployeeID, JobRoleID: jobRoleID,
		CapabilityKey: capability.CapabilityKey, ProductSurface: capability.Manifest.ProductSurface,
		ResourceType: resourceType, ResourceID: resourceID, RiskClass: capability.RiskClass,
		PrincipalType: principal.Type, PrincipalID: principal.ID, At: time.Now().UTC(),
	})
}

func (u *UseCases) verifyCurrentAuthority(ctx context.Context, tenantID string, virployeeID uuid.UUID, capability capabilitydomain.Capability, action preparedactions.Action, expectedHash string) (executiongate.AuthorityCheckResult, error) {
	if u.authority == nil {
		if strings.TrimSpace(expectedHash) != "" {
			return executiongate.AuthorityCheckResult{}, domainerr.Conflict("professional authority evaluator is unavailable")
		}
		return executiongate.AuthorityCheckResult{}, nil
	}
	if strings.TrimSpace(expectedHash) == "" {
		return executiongate.AuthorityCheckResult{}, domainerr.Conflict("approval has no professional authority binding")
	}
	virployee, err := u.repo.Get(ctx, tenantID, virployeeID)
	if err != nil {
		return executiongate.AuthorityCheckResult{}, err
	}
	principal, err := executiongate.NormalizePrincipalContext(executiongate.PrincipalContext{Type: action.PrincipalType, ID: action.PrincipalID})
	if err != nil {
		return executiongate.AuthorityCheckResult{}, domainerr.Conflict("prepared action principal context is invalid")
	}
	result, err := u.evaluateAuthority(ctx, tenantID, virployeeID, virployee.JobRoleID, capability, principal, action.MCPContext)
	if err != nil {
		return executiongate.AuthorityCheckResult{}, domainerr.Conflict("professional authority could not be revalidated")
	}
	if !result.Allowed {
		return executiongate.AuthorityCheckResult{}, domainerr.Conflict("professional authority no longer permits this action")
	}
	if result.SnapshotHash != expectedHash {
		return executiongate.AuthorityCheckResult{}, domainerr.Conflict("professional authority changed after approval")
	}
	return result, nil
}
