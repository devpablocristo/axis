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
		Memory:       memoryFrom(rc.MemoryContext),
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

// EnrichRequest carries the deterministic distilled procedure text to be
// reworded by the LLM. Only structural, PII-free fields — never draft values.
type EnrichRequest struct {
	CapabilityKey string
	Title         string
	Content       string
}

// EnrichResult is the reworded procedure. Enriched is false when the runtime
// could not improve it (e.g. Echo/no model); the caller then keeps the
// deterministic text.
type EnrichResult struct {
	Title         string
	Content       string
	Enriched      bool
	ModelID       string
	PromptVersion string
}

// Enrich asks the runtime to improve the wording of a distilled procedure.
func (c *Client) Enrich(ctx context.Context, in EnrichRequest) (EnrichResult, error) {
	raw, err := json.Marshal(enrichRequest(in))
	if err != nil {
		return EnrichResult{}, fmt.Errorf("encode enrich request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/enrich", bytes.NewReader(raw))
	if err != nil {
		return EnrichResult{}, fmt.Errorf("build enrich request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.internalAuthSecret != "" {
		req.Header.Set("X-Axis-Internal-Token", c.internalAuthSecret)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return EnrichResult{}, fmt.Errorf("enrich: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return EnrichResult{}, fmt.Errorf("enrich: status %d", resp.StatusCode)
	}

	var out enrichResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EnrichResult{}, fmt.Errorf("decode enrich response: %w", err)
	}
	return EnrichResult{
		Title:         out.Title,
		Content:       out.Content,
		Enriched:      out.Enriched,
		ModelID:       out.Model,
		PromptVersion: out.PromptVersion,
	}, nil
}

// AnswerRequest carries the input data to be processed under the virployee's
// system-prompt role, and an optional response schema for a structured answer.
// It is the "process and respond" path (read/explain), not classification.
type AnswerRequest struct {
	SystemPrompt   string
	JobRole        string
	InputJSON      json.RawMessage
	ResponseSchema map[string]any
	ContentParts   []ContentPart
}

type ContentPart struct {
	Kind       string          `json:"kind"`
	Text       string          `json:"text,omitempty"`
	Data       []byte          `json:"data,omitempty"`
	URI        string          `json:"uri,omitempty"`
	MIMEType   string          `json:"mime_type,omitempty"`
	Name       string          `json:"name,omitempty"`
	SHA256     string          `json:"sha256,omitempty"`
	DocumentID string          `json:"document_id,omitempty"`
	Locator    json.RawMessage `json:"locator,omitempty"`
}

// AnswerResult is the runtime's answer. Answered is false when the model did not
// produce a usable answer (e.g. Echo / no model configured); the caller then
// marks the run as degraded rather than treating the canned text as a real answer.
type AnswerResult struct {
	OutputText    string
	OutputJSON    json.RawMessage
	Answered      bool
	ModelID       string
	PromptVersion string
}

const (
	EmbeddingTaskDocument = "RETRIEVAL_DOCUMENT"
	EmbeddingTaskQuery    = "RETRIEVAL_QUERY"
)

type EmbedRequest struct {
	Texts    []string
	TaskType string
}

type EmbedResult struct {
	Model      string
	Dimensions int
	Embeddings [][]float32
}

func (c *Client) Embed(ctx context.Context, in EmbedRequest) (EmbedResult, error) {
	raw, err := json.Marshal(struct {
		Texts    []string `json:"texts"`
		TaskType string   `json:"task_type"`
	}{Texts: in.Texts, TaskType: in.TaskType})
	if err != nil {
		return EmbedResult{}, fmt.Errorf("encode embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/embeddings", bytes.NewReader(raw))
	if err != nil {
		return EmbedResult{}, fmt.Errorf("build embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.internalAuthSecret != "" {
		req.Header.Set("X-Axis-Internal-Token", c.internalAuthSecret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return EmbedResult{}, fmt.Errorf("embeddings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return EmbedResult{}, fmt.Errorf("embeddings: status %d", resp.StatusCode)
	}
	var out struct {
		Model      string      `json:"model"`
		Dimensions int         `json:"dimensions"`
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return EmbedResult{}, fmt.Errorf("decode embedding response: %w", err)
	}
	return EmbedResult(out), nil
}

// Answer asks the runtime to process the input JSON and respond. A transport or
// non-200 error is returned to the caller (fail-closed: no silent success).
func (c *Client) Answer(ctx context.Context, in AnswerRequest) (AnswerResult, error) {
	raw, err := json.Marshal(answerRequest(in))
	if err != nil {
		return AnswerResult{}, fmt.Errorf("encode answer request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/answer", bytes.NewReader(raw))
	if err != nil {
		return AnswerResult{}, fmt.Errorf("build answer request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.internalAuthSecret != "" {
		req.Header.Set("X-Axis-Internal-Token", c.internalAuthSecret)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return AnswerResult{}, fmt.Errorf("answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return AnswerResult{}, fmt.Errorf("answer: status %d", resp.StatusCode)
	}

	var out answerResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return AnswerResult{}, fmt.Errorf("decode answer response: %w", err)
	}
	return AnswerResult{
		OutputText:    out.OutputText,
		OutputJSON:    out.OutputJSON,
		Answered:      out.Answered,
		ModelID:       out.Model,
		PromptVersion: out.PromptVersion,
	}, nil
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

func memoryFrom(items []memories.ContextItem) []memoryRef {
	out := make([]memoryRef, 0, len(items))
	for _, item := range items {
		out = append(out, memoryRef{Title: item.Title, Type: item.Type, Content: item.Content})
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
	Title   string `json:"title,omitempty"`
	Type    string `json:"type,omitempty"`
	Content string `json:"content,omitempty"`
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

type enrichRequest struct {
	CapabilityKey string `json:"capability_key"`
	Title         string `json:"title"`
	Content       string `json:"content"`
}

type enrichResponse struct {
	Title         string `json:"title"`
	Content       string `json:"content"`
	Enriched      bool   `json:"enriched"`
	Model         string `json:"model,omitempty"`
	PromptVersion string `json:"prompt_version,omitempty"`
}

type answerRequest struct {
	SystemPrompt   string          `json:"system_prompt,omitempty"`
	JobRole        string          `json:"job_role,omitempty"`
	InputJSON      json.RawMessage `json:"input_json"`
	ResponseSchema map[string]any  `json:"response_schema,omitempty"`
	ContentParts   []ContentPart   `json:"content_parts,omitempty"`
}

type answerResponse struct {
	OutputText    string          `json:"output_text,omitempty"`
	OutputJSON    json.RawMessage `json:"output_json,omitempty"`
	Answered      bool            `json:"answered"`
	Model         string          `json:"model,omitempty"`
	PromptVersion string          `json:"prompt_version,omitempty"`
}
