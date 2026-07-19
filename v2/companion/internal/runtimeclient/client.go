// Package runtimeclient talks to the runtime-v2 service to obtain an intent
// proposal for a natural-language input. It implements the virployees
// RuntimePlannerPort: it only proposes; Companion decides on the proposal.
package runtimeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

type Client struct {
	baseURL            string
	http               *http.Client
	internalAuthSecret string
}

func New(baseURL string, client *http.Client, internalAuthSecret string) *Client {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:            strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:               client,
		internalAuthSecret: strings.TrimSpace(internalAuthSecret),
	}
}

// Propose asks the runtime to classify the input into one of the assigned
// capabilities and maps the response into a dryrun.Proposal.
func (c *Client) Propose(ctx context.Context, input string, rc runtimecontext.Context) (dryrun.Proposal, error) {
	body := proposeRequest{
		Input:        input,
		SystemPrompt: rc.ProfileTemplate.SystemPrompt,
		JobRole:      rc.JobRole.Name,
		Capabilities: capabilitiesFrom(rc.Capabilities),
		Memory:       memoryFrom(rc.MemoryReferences),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return dryrun.Proposal{}, fmt.Errorf("encode propose request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/propose", bytes.NewReader(raw))
	if err != nil {
		return dryrun.Proposal{}, fmt.Errorf("build propose request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.internalAuthSecret != "" {
		req.Header.Set("X-Axis-Internal-Token", c.internalAuthSecret)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return dryrun.Proposal{}, fmt.Errorf("propose: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return dryrun.Proposal{}, fmt.Errorf("propose: status %d", resp.StatusCode)
	}

	var out proposeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return dryrun.Proposal{}, fmt.Errorf("decode propose response: %w", err)
	}
	return toProposal(out), nil
}

func capabilitiesFrom(items []capabilitydomain.Capability) []capabilityInfo {
	out := make([]capabilityInfo, 0, len(items))
	for _, item := range items {
		out = append(out, capabilityInfo{
			CapabilityKey:    item.CapabilityKey,
			Name:             item.Name,
			Description:      item.Description,
			RequiredAutonomy: string(item.RequiredAutonomy),
			RiskClass:        item.RiskClass,
			SideEffectClass:  item.SideEffectClass,
		})
	}
	return out
}

func memoryFrom(refs []memories.Reference) []memoryRef {
	out := make([]memoryRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, memoryRef{Title: ref.Title, Type: ref.Type})
	}
	return out
}

func toProposal(resp proposeResponse) dryrun.Proposal {
	intent := dryrun.Intent{
		Matched:       resp.Intent.Matched,
		CapabilityKey: resp.Intent.CapabilityKey,
		Domain:        resp.Intent.Domain,
		Resource:      resp.Intent.Resource,
		Action:        resp.Intent.Action,
		Confidence:    resp.Intent.Confidence,
		MatchedBy:     []string{},
		Rules:         []dryrun.IntentRule{},
	}
	// Provenance is stamped whenever the runtime answered — also on "no
	// capability applies" — so the console never misattributes an LLM answer
	// to the deterministic matcher.
	intent.ProposedBy = "llm"
	intent.ModelID = resp.Model
	intent.PromptVersion = resp.PromptVersion
	if intent.Matched {
		intent.MatchedBy = []string{"runtime"}
	}
	return dryrun.Proposal{
		Intent:           intent,
		RequiredAutonomy: virployeedomain.AutonomyLevel(resp.Intent.RequiredAutonomy),
	}
}

type proposeRequest struct {
	Input        string           `json:"input"`
	SystemPrompt string           `json:"system_prompt,omitempty"`
	JobRole      string           `json:"job_role,omitempty"`
	Capabilities []capabilityInfo `json:"capabilities"`
	Memory       []memoryRef      `json:"memory,omitempty"`
}

type capabilityInfo struct {
	CapabilityKey    string `json:"capability_key"`
	Name             string `json:"name,omitempty"`
	Description      string `json:"description,omitempty"`
	RequiredAutonomy string `json:"required_autonomy,omitempty"`
	RiskClass        string `json:"risk_class,omitempty"`
	SideEffectClass  string `json:"side_effect_class,omitempty"`
}

type memoryRef struct {
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
}

type proposeResponse struct {
	Intent        proposedIntent `json:"intent"`
	Model         string         `json:"model,omitempty"`
	PromptVersion string         `json:"prompt_version,omitempty"`
}

type proposedIntent struct {
	Matched          bool    `json:"matched"`
	CapabilityKey    string  `json:"capability_key,omitempty"`
	Domain           string  `json:"domain,omitempty"`
	Resource         string  `json:"resource,omitempty"`
	Action           string  `json:"action,omitempty"`
	RequiredAutonomy string  `json:"required_autonomy,omitempty"`
	Confidence       float64 `json:"confidence,omitempty"`
}
