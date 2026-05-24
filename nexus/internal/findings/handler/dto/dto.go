package dto

import "encoding/json"

type FindingRuleRequest struct {
	OwnerSystem    string `json:"owner_system"`
	SourceSystem   string `json:"source_system"`
	FactType       string `json:"fact_type"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Expression     string `json:"expression"`
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation"`
	Mode           string `json:"mode"`
	Enabled        *bool  `json:"enabled,omitempty"`
	Priority       int    `json:"priority"`
}

type SubmitFactsRequest struct {
	OwnerSystem   string         `json:"owner_system"`
	SourceSystem  string         `json:"source_system"`
	FactType      string         `json:"fact_type"`
	SourceEventID string         `json:"source_event_id"`
	SubjectType   string         `json:"subject_type"`
	SubjectID     string         `json:"subject_id"`
	Facts         map[string]any `json:"facts"`
}

type UpdateFindingRequest struct {
	Status         string `json:"status"`
	ResolutionNote string `json:"resolution_note"`
}

type FactEvaluationResponse struct {
	ID            string         `json:"id"`
	OrgID         string         `json:"org_id"`
	OwnerSystem   string         `json:"owner_system"`
	SourceSystem  string         `json:"source_system"`
	FactType      string         `json:"fact_type"`
	SourceEventID string         `json:"source_event_id"`
	SubjectType   string         `json:"subject_type"`
	SubjectID     string         `json:"subject_id"`
	Facts         map[string]any `json:"facts"`
	CreatedAt     string         `json:"created_at"`
}

type FindingRuleResponse struct {
	ID             string `json:"id"`
	OrgID          string `json:"org_id"`
	OwnerSystem    string `json:"owner_system"`
	SourceSystem   string `json:"source_system"`
	FactType       string `json:"fact_type"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Expression     string `json:"expression"`
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation"`
	Mode           string `json:"mode"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
	ArchivedAt     string `json:"archived_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type FindingResponse struct {
	ID             string         `json:"id"`
	OrgID          string         `json:"org_id"`
	EvaluationID   string         `json:"evaluation_id"`
	RuleID         string         `json:"rule_id"`
	OwnerSystem    string         `json:"owner_system"`
	SourceSystem   string         `json:"source_system"`
	FactType       string         `json:"fact_type"`
	SourceEventID  string         `json:"source_event_id"`
	SubjectType    string         `json:"subject_type"`
	SubjectID      string         `json:"subject_id"`
	Code           string         `json:"code"`
	Severity       string         `json:"severity"`
	Title          string         `json:"title"`
	Message        string         `json:"message"`
	Recommendation string         `json:"recommendation"`
	Evidence       map[string]any `json:"evidence"`
	Status         string         `json:"status"`
	ResolutionNote string         `json:"resolution_note"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type SubmitFactsResponse struct {
	Evaluation FactEvaluationResponse `json:"evaluation"`
	Findings   []FindingResponse      `json:"findings"`
}

type RawJSON = json.RawMessage
