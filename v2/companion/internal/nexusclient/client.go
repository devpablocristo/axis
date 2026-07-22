package nexusclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/professionalauthority"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Client struct {
	baseURL            string
	http               *http.Client
	internalAuthSecret string
}

type HTTPStatusError struct {
	Operation  string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%s: status %d", e.Operation, e.StatusCode)
}

// IsPermanentHTTPError distinguishes requests Nexus definitively rejected
// from transport, throttling, timeout, and server failures that may succeed on
// retry.
func IsPermanentHTTPError(err error) bool {
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.StatusCode >= 400 && statusErr.StatusCode < 500 &&
		statusErr.StatusCode != http.StatusRequestTimeout && statusErr.StatusCode != http.StatusTooManyRequests
}

func New(baseURL string, client *http.Client, internalAuthSecret ...string) *Client {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	secret := ""
	if len(internalAuthSecret) > 0 {
		secret = strings.TrimSpace(internalAuthSecret[0])
	}
	return &Client{
		baseURL:            strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:               client,
		internalAuthSecret: secret,
	}
}

func (c *Client) setTrustedHeaders(req *http.Request, tenantID, actorID string) {
	if c.internalAuthSecret != "" {
		req.Header.Set("X-Axis-Internal-Token", c.internalAuthSecret)
	}
	if strings.TrimSpace(tenantID) != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	if strings.TrimSpace(actorID) != "" {
		req.Header.Set("X-Actor-ID", actorID)
	}
}

func (c *Client) ReportOperationalFinding(ctx context.Context, tenantID, idempotencyKey string, payload json.RawMessage) error {
	if !json.Valid(payload) || strings.TrimSpace(idempotencyKey) == "" {
		return fmt.Errorf("operational finding metadata is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/internal/operations/findings", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build operational finding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", strings.TrimSpace(idempotencyKey))
	c.setTrustedHeaders(req, tenantID, "service:companion-operations")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("report operational finding: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return &HTTPStatusError{Operation: "report operational finding", StatusCode: resp.StatusCode}
	}
	return nil
}

func (c *Client) Check(ctx context.Context, input executiongate.GovernanceCheckInput) (executiongate.GovernanceCheckResult, error) {
	body := checkRequest{
		RequesterType:        input.RequesterType,
		RequesterID:          input.RequesterID,
		ProductSurface:       input.ProductSurface,
		SupervisorUserID:     input.SupervisorUserID,
		ActionType:           input.ActionType,
		TargetSystem:         input.TargetSystem,
		TargetResource:       input.TargetResource,
		ResourceType:         input.ResourceType,
		Reason:               input.Reason,
		BindingHash:          input.BindingHash,
		AuthorityBindingHash: input.AuthorityBindingHash,
		ScopeRevision:        input.ScopeRevision,
		PolicyRevisionHash:   input.PolicyRevisionHash,
		DelegationRequired:   input.DelegationRequired,
		DelegationID:         input.DelegationID,
		DelegationRevision:   input.DelegationRevision,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return executiongate.GovernanceCheckResult{}, fmt.Errorf("encode governance check: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/governance/check", bytes.NewReader(raw))
	if err != nil {
		return executiongate.GovernanceCheckResult{}, fmt.Errorf("build governance check request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setTrustedHeaders(req, input.TenantID, input.RequesterID)

	resp, err := c.http.Do(req)
	if err != nil {
		return executiongate.GovernanceCheckResult{}, fmt.Errorf("governance check: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return executiongate.GovernanceCheckResult{}, fmt.Errorf("governance check: status %d", resp.StatusCode)
	}
	var out checkResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return executiongate.GovernanceCheckResult{}, fmt.Errorf("decode governance check: %w", err)
	}
	return executiongate.GovernanceCheckResult{
		CheckID:              out.CheckID,
		Decision:             out.Decision,
		RiskLevel:            out.RiskLevel,
		Status:               out.Status,
		DecisionReason:       out.DecisionReason,
		WouldRequireApproval: out.WouldRequireApproval,
		BindingHash:          out.BindingHash,
		ApprovalID:           out.ApprovalID,
		ApprovalStatus:       out.ApprovalStatus,
		PolicySnapshotHash:   out.PolicySnapshotHash,
	}, nil
}

func (c *Client) GetApproval(ctx context.Context, tenantID string, id uuid.UUID) (executiongate.GovernanceApproval, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/approvals/"+id.String(), nil)
	if err != nil {
		return executiongate.GovernanceApproval{}, fmt.Errorf("build approval request: %w", err)
	}
	c.setTrustedHeaders(req, tenantID, "companion-v2")

	resp, err := c.http.Do(req)
	if err != nil {
		return executiongate.GovernanceApproval{}, fmt.Errorf("get approval: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return executiongate.GovernanceApproval{}, domainerr.NotFound("approval not found")
	}
	if resp.StatusCode != http.StatusOK {
		return executiongate.GovernanceApproval{}, fmt.Errorf("get approval: status %d", resp.StatusCode)
	}
	var out approvalResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return executiongate.GovernanceApproval{}, fmt.Errorf("decode approval: %w", err)
	}
	return executiongate.GovernanceApproval{
		ID:                 out.ID,
		GovernanceCheckID:  out.GovernanceCheckID,
		RequesterID:        out.RequesterID,
		BindingHash:        out.BindingHash,
		Status:             out.Status,
		PolicySnapshotHash: out.PolicySnapshotHash,
	}, nil
}

type checkRequest struct {
	RequesterType        string `json:"requester_type,omitempty"`
	RequesterID          string `json:"requester_id"`
	ProductSurface       string `json:"product_surface,omitempty"`
	SupervisorUserID     string `json:"supervisor_user_id,omitempty"`
	ActionType           string `json:"action_type"`
	TargetSystem         string `json:"target_system,omitempty"`
	TargetResource       string `json:"target_resource,omitempty"`
	ResourceType         string `json:"resource_type,omitempty"`
	Reason               string `json:"reason,omitempty"`
	BindingHash          string `json:"binding_hash,omitempty"`
	AuthorityBindingHash string `json:"authority_binding_hash,omitempty"`
	ScopeRevision        int64  `json:"scope_revision,omitempty"`
	PolicyRevisionHash   string `json:"policy_revision_hash,omitempty"`
	DelegationRequired   bool   `json:"delegation_required,omitempty"`
	DelegationID         string `json:"delegation_id,omitempty"`
	DelegationRevision   int64  `json:"delegation_revision,omitempty"`
}

type approvalResponse struct {
	ID                 string `json:"id"`
	GovernanceCheckID  string `json:"governance_check_id"`
	RequesterID        string `json:"requester_id"`
	BindingHash        string `json:"binding_hash"`
	Status             string `json:"status"`
	PolicySnapshotHash string `json:"policy_snapshot_hash"`
}

type checkResponse struct {
	CheckID              string `json:"check_id"`
	Decision             string `json:"decision"`
	RiskLevel            string `json:"risk_level"`
	Status               string `json:"status"`
	DecisionReason       string `json:"decision_reason"`
	WouldRequireApproval bool   `json:"would_require_approval"`
	Mode                 string `json:"mode"`
	BindingHash          string `json:"binding_hash"`
	ApprovalID           string `json:"approval_id"`
	ApprovalStatus       string `json:"approval_status"`
	PolicySnapshotHash   string `json:"policy_snapshot_hash"`
}

func (c *Client) Revalidate(ctx context.Context, input executiongate.GovernanceRevalidationInput) (executiongate.GovernanceRevalidationResult, error) {
	body := map[string]any{
		"binding_hash": input.BindingHash, "policy_snapshot_hash": input.PolicySnapshotHash,
		"authority_binding_hash": input.AuthorityBindingHash, "scope_revision": input.ScopeRevision,
		"policy_revision_hash": input.PolicyRevisionHash, "delegation_id": input.DelegationID,
		"delegation_revision": input.DelegationRevision,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return executiongate.GovernanceRevalidationResult{}, fmt.Errorf("encode governance revalidation: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/governance/checks/"+strings.TrimSpace(input.CheckID)+"/revalidate", bytes.NewReader(raw))
	if err != nil {
		return executiongate.GovernanceRevalidationResult{}, fmt.Errorf("build governance revalidation: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setTrustedHeaders(req, input.TenantID, "companion-v2")
	resp, err := c.http.Do(req)
	if err != nil {
		return executiongate.GovernanceRevalidationResult{}, fmt.Errorf("governance revalidation: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return executiongate.GovernanceRevalidationResult{}, &HTTPStatusError{Operation: "governance revalidation", StatusCode: resp.StatusCode}
	}
	var out struct {
		Valid              bool   `json:"valid"`
		Reason             string `json:"reason"`
		PolicySnapshotHash string `json:"policy_snapshot_hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return executiongate.GovernanceRevalidationResult{}, fmt.Errorf("decode governance revalidation: %w", err)
	}
	return executiongate.GovernanceRevalidationResult{Valid: out.Valid, Reason: out.Reason, PolicySnapshotHash: out.PolicySnapshotHash}, nil
}

func (c *Client) CheckDelegationAuthorization(ctx context.Context, input professionalauthority.DelegationAuthorizationCheck) (professionalauthority.DelegationAuthorizationResult, error) {
	body := map[string]any{"actor_id": input.ActorID, "actor_role": input.ActorRole, "permission": input.Permission,
		"product_surface": input.ProductSurface, "action_type": input.ActionType, "resource_type": input.ResourceType,
		"resource_id": input.ResourceID, "risk_class": input.RiskClass}
	raw, err := json.Marshal(body)
	if err != nil {
		return professionalauthority.DelegationAuthorizationResult{}, fmt.Errorf("encode delegation authorization: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/internal/authorization:check", bytes.NewReader(raw))
	if err != nil {
		return professionalauthority.DelegationAuthorizationResult{}, fmt.Errorf("build delegation authorization: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Axis-Tenant-Role", input.ActorRole)
	c.setTrustedHeaders(req, input.TenantID, input.ActorID)
	resp, err := c.http.Do(req)
	if err != nil {
		return professionalauthority.DelegationAuthorizationResult{}, fmt.Errorf("delegation authorization: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return professionalauthority.DelegationAuthorizationResult{}, &HTTPStatusError{Operation: "delegation authorization", StatusCode: resp.StatusCode}
	}
	var out struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return professionalauthority.DelegationAuthorizationResult{}, fmt.Errorf("decode delegation authorization: %w", err)
	}
	return professionalauthority.DelegationAuthorizationResult{Allowed: out.Allowed, Reason: out.Reason}, nil
}

// AuditEvent is one entry to append to a virployee's tamper-evident ledger in
// Nexus. Data must carry only hashes + non-sensitive metadata, never PHI.
type AuditEvent struct {
	VirployeeID string
	ActorType   string
	ActorID     string
	SubjectType string
	SubjectID   string
	EventType   string
	Summary     string
	Data        map[string]any
}

// AppendAuditEvent records an event in the Nexus audit ledger. Nexus seals it
// (hash-chains + optionally signs) and holds it append-only, so companion cannot
// forge or rewrite history. The caller treats failures as best-effort.
func (c *Client) AppendAuditEvent(ctx context.Context, tenantID string, e AuditEvent) error {
	return c.AppendAuditEventIdempotent(ctx, tenantID, "", e)
}

// AppendAuditEventIdempotent uses a caller-owned UUID as Nexus's audit event
// ID. Retrying the same outbox message therefore returns the existing sealed
// event instead of appending a duplicate to the immutable ledger.
func (c *Client) AppendAuditEventIdempotent(ctx context.Context, tenantID, idempotencyKey string, e AuditEvent) error {
	body := map[string]any{
		"virployee_id": e.VirployeeID,
		"subject_type": e.SubjectType,
		"subject_id":   e.SubjectID,
		"event_type":   e.EventType,
		"actor_type":   e.ActorType,
		"actor_id":     e.ActorID,
		"summary":      e.Summary,
		"data":         e.Data,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode audit event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/audit/events", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build audit event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey = strings.TrimSpace(idempotencyKey); idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	// Nexus requires a non-empty X-Actor-ID; the acting virployee is the actor.
	c.setTrustedHeaders(req, tenantID, e.ActorID)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return &HTTPStatusError{Operation: "append audit event", StatusCode: resp.StatusCode}
	}
	return nil
}

func (c *Client) ReportExecutionResult(ctx context.Context, tenantID, checkID, idempotencyKey, bindingHash, status string, durationMS int64, result map[string]any, attestationVersion, executorVersion, signature string) error {
	body := map[string]any{
		"binding_hash": bindingHash, "status": status, "duration_ms": durationMS, "result": result,
		"attestation_version": attestationVersion, "executor_version": executorVersion, "attestation": signature,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode execution result: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/governance/checks/"+checkID+"/result", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build execution result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setTrustedHeaders(req, tenantID, "companion-v2")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("report execution result: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("report execution result: status %d", resp.StatusCode)
	}
	return nil
}
