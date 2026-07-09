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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return executiongate.GovernanceCheckResult{}, fmt.Errorf("governance check: status %d", resp.StatusCode)
	}
	var out checkResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return executiongate.GovernanceCheckResult{}, fmt.Errorf("decode governance check: %w", err)
	}
	return executiongate.GovernanceCheckResult{
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

type checkResponse struct {
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
