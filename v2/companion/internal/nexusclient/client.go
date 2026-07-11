package nexusclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string, client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    client,
	}
}

func (c *Client) Check(ctx context.Context, input executiongate.GovernanceCheckInput) (executiongate.GovernanceCheckResult, error) {
	body := checkRequest{
		RequesterType:  input.RequesterType,
		RequesterID:    input.RequesterID,
		ActionType:     input.ActionType,
		TargetSystem:   input.TargetSystem,
		TargetResource: input.TargetResource,
		Params:         input.Params,
		Reason:         input.Reason,
		Context:        input.Context,
		BindingHash:    input.BindingHash,
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
	if input.TenantID != "" {
		req.Header.Set("X-Tenant-ID", input.TenantID)
	}

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
	}, nil
}

func (c *Client) GetApproval(ctx context.Context, tenantID string, id uuid.UUID) (executiongate.GovernanceApproval, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/approvals/"+id.String(), nil)
	if err != nil {
		return executiongate.GovernanceApproval{}, fmt.Errorf("build approval request: %w", err)
	}
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}

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
		ID:                out.ID,
		GovernanceCheckID: out.GovernanceCheckID,
		RequesterID:       out.RequesterID,
		BindingHash:       out.BindingHash,
		Status:            out.Status,
	}, nil
}

type checkRequest struct {
	RequesterType  string         `json:"requester_type,omitempty"`
	RequesterID    string         `json:"requester_id"`
	ActionType     string         `json:"action_type"`
	TargetSystem   string         `json:"target_system,omitempty"`
	TargetResource string         `json:"target_resource,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	Context        string         `json:"context,omitempty"`
	BindingHash    string         `json:"binding_hash,omitempty"`
}

type approvalResponse struct {
	ID                string `json:"id"`
	GovernanceCheckID string `json:"governance_check_id"`
	RequesterID       string `json:"requester_id"`
	BindingHash       string `json:"binding_hash"`
	Status            string `json:"status"`
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
}

func (c *Client) ReportExecutionResult(ctx context.Context, tenantID, checkID, idempotencyKey, bindingHash, status string, durationMS int64, result map[string]any) error {
	body := map[string]any{"binding_hash": bindingHash, "status": status, "duration_ms": durationMS, "result": result}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode execution result: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/governance/checks/"+checkID+"/result", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build execution result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
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
