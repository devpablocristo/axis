package nexusclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/devpablocristo/platform/http/go/httpclient"
)

type Client struct {
	caller *httpclient.Caller
}

func NewClient(baseURL, apiKey string) *Client {
	h := make(http.Header)
	h.Set("X-API-Key", apiKey)
	return &Client{
		caller: &httpclient.Caller{
			BaseURL:     baseURL,
			Header:      h,
			HTTP:        &http.Client{Timeout: 30 * time.Second},
			MaxBodySize: 1 << 20,
		},
	}
}

const (
	DecisionAllow           = "allow"
	DecisionDeny            = "deny"
	DecisionRequireApproval = "require_approval"
)

const (
	ToolIntentSchemaVersion             = "tool_intent.v1"
	ActionTypeAgentCapabilityInvoke     = "agent.capability.invoke"
	ActionTypeAgentCapabilityCompensate = "agent.capability.compensate"
)

const (
	StatusPending         = "pending"
	StatusEvaluated       = "evaluated"
	StatusAllowed         = "allowed"
	StatusDenied          = "denied"
	StatusPendingApproval = "pending_approval"
	StatusApproved        = "approved"
	StatusRejected        = "rejected"
	StatusExpired         = "expired"
	StatusExecuted        = "executed"
	StatusFailed          = "failed"
	StatusCancelled       = "cancelled"
)

var KnownStatuses = []string{
	StatusPending,
	StatusEvaluated,
	StatusAllowed,
	StatusDenied,
	StatusPendingApproval,
	StatusApproved,
	StatusRejected,
	StatusExpired,
	StatusExecuted,
	StatusFailed,
	StatusCancelled,
}

type SubmitRequestBody struct {
	RequesterType  string         `json:"requester_type"`
	RequesterID    string         `json:"requester_id"`
	RequesterName  string         `json:"requester_name,omitempty"`
	ActionType     string         `json:"action_type"`
	TargetSystem   string         `json:"target_system,omitempty"`
	TargetResource string         `json:"target_resource,omitempty"`
	ActionBinding  map[string]any `json:"action_binding,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	Context        string         `json:"context,omitempty"`
}

type SubmitResponse struct {
	RequestID      string `json:"request_id"`
	Decision       string `json:"decision"`
	RiskLevel      string `json:"risk_level"`
	DecisionReason string `json:"decision_reason"`
	Status         string `json:"status"`
	BindingHash    string `json:"binding_hash,omitempty"`
	Approval       *struct {
		ID        string `json:"id"`
		ExpiresAt string `json:"expires_at"`
	} `json:"approval,omitempty"`
}

type RequestSummary struct {
	ID             string `json:"id"`
	RequesterType  string `json:"requester_type"`
	RequesterID    string `json:"requester_id"`
	ActionType     string `json:"action_type"`
	TargetSystem   string `json:"target_system"`
	TargetResource string `json:"target_resource"`
	Reason         string `json:"reason"`
	RiskLevel      string `json:"risk_level"`
	Decision       string `json:"decision"`
	DecisionReason string `json:"decision_reason"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func (c *Client) SubmitRequest(ctx context.Context, idempotencyKey string, body SubmitRequestBody) (SubmitResponse, error) {
	var opts []httpclient.RequestOption
	if idempotencyKey != "" {
		opts = append(opts, httpclient.WithIdempotencyKey(idempotencyKey))
	}

	var out SubmitResponse
	st, raw, err := c.caller.DoJSON(ctx, http.MethodPost, "/v1/requests", body, opts...)
	if err != nil {
		return out, fmt.Errorf("nexus submit: %w", err)
	}
	if st != http.StatusCreated {
		return out, fmt.Errorf("nexus submit: status %d body %s", st, string(raw))
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("decode submit response: %w", err)
	}
	return out, nil
}

func (c *Client) GetRequest(ctx context.Context, id string) (RequestSummary, int, error) {
	var out RequestSummary
	st, raw, err := c.caller.DoJSON(ctx, http.MethodGet, "/v1/requests/"+id, nil)
	if err != nil {
		return out, 0, fmt.Errorf("nexus get request: %w", err)
	}
	if st == http.StatusNotFound {
		return out, st, nil
	}
	if st != http.StatusOK {
		return out, st, fmt.Errorf("nexus get request: status %d body %s", st, string(raw))
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, st, fmt.Errorf("decode get response: %w", err)
	}
	return out, st, nil
}

func (c *Client) ListRequests(ctx context.Context, query string) (int, []byte, error) {
	return c.listRequests(ctx, query)
}

func (c *Client) ListRequestsForOrg(ctx context.Context, query, orgID string) (int, []byte, error) {
	var opts []httpclient.RequestOption
	if orgID != "" {
		opts = append(opts, httpclient.WithHeader("X-Org-ID", orgID))
	}
	return c.listRequests(ctx, query, opts...)
}

func (c *Client) listRequests(ctx context.Context, query string, opts ...httpclient.RequestOption) (int, []byte, error) {
	path := "/v1/requests"
	if query != "" {
		path += "?" + query
	}
	return c.caller.DoJSON(ctx, http.MethodGet, path, nil, opts...)
}

func (c *Client) SubmitProposal(ctx context.Context, body any) (int, []byte, error) {
	return c.caller.DoJSON(ctx, http.MethodPost, "/v1/learning/proposals", body)
}

func (c *Client) ListPendingApprovals(ctx context.Context) (int, []byte, error) {
	return c.caller.DoJSON(ctx, http.MethodGet, "/v1/approvals/pending", nil)
}

func (c *Client) ListPolicies(ctx context.Context) (int, []byte, error) {
	return c.caller.DoJSON(ctx, http.MethodGet, "/v1/policies", nil)
}

func ParseErrorBody(raw []byte) string {
	var eb struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &eb) == nil && eb.Message != "" {
		return eb.Message
	}
	return string(raw)
}
