// Package productedge defines the application-facing contracts used by the
// machine-authenticated product edge. It deliberately contains no HTTP,
// database, or downstream-service details.
package productedge

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	AccessModeDirect          = "direct"
	AccessModeViaOrchestrator = "via_orchestrator"
)

// InvocationContext is the neutral authority and tenancy context propagated
// across every product-initiated operation.
type InvocationContext struct {
	OrgID               string   `json:"org_id"`
	ProductID           string   `json:"product_id"`
	ProductSurface      string   `json:"product_surface,omitempty"`
	IntegrationID       string   `json:"integration_id,omitempty"`
	IntegrationRevision int64    `json:"integration_revision,omitempty"`
	IntegrationHash     string   `json:"integration_hash,omitempty"`
	PrincipalID         string   `json:"principal_id"`
	PrincipalType       string   `json:"principal_type,omitempty"`
	Scopes              []string `json:"scopes,omitempty"`
	AccessMode          string   `json:"access_mode"`
}

type CapabilityRef struct {
	ID           string `json:"id,omitempty"`
	Key          string `json:"key,omitempty"`
	Version      string `json:"version"`
	ManifestHash string `json:"manifest_hash"`
}

type EventContract struct {
	Type       string          `json:"type"`
	Version    string          `json:"version"`
	Schema     json.RawMessage `json:"schema"`
	SchemaHash string          `json:"schema_hash"`
}

type MachineBinding struct {
	Context             InvocationContext
	VirployeeID         string
	RoutingPoolID       string
	AllowedVirployeeIDs []string
	AllowedPoolIDs      []string
	AllowedCapabilities []CapabilityRef
	AllowedEvents       []EventContract
	MaxRequestBytes     int64
}

type AssistInput struct {
	VirployeeID          string
	Input                json.RawMessage
	IdempotencyKey       string
	AssistType           string
	CapabilityID         string
	CapabilityKey        string
	SubjectID            string
	RepositoryGeneration string
	CaseID               string
	AssignmentID         string
}

type AssistRun struct {
	ID                     string          `json:"id"`
	CaseID                 string          `json:"case_id"`
	ResponsibleVirployeeID string          `json:"responsible_virployee_id"`
	CapabilityID           string          `json:"capability_id"`
	CapabilityKey          string          `json:"capability_key"`
	CapabilityManifestHash string          `json:"capability_manifest_hash"`
	Status                 string          `json:"status"`
	AnswerStatus           string          `json:"answer_status"`
	Citations              json.RawMessage `json:"citations"`
	Output                 json.RawMessage `json:"output"`
	Orchestration          json.RawMessage `json:"orchestration"`
	Error                  string          `json:"error_message"`
}

type RoutingInput struct {
	PoolID        string
	SubjectID     string
	CapabilityID  string
	CapabilityKey string
}

type RoutingAssignment struct {
	ID          string `json:"id"`
	VirployeeID string `json:"virployee_id"`
}

type RoutingResolution struct {
	Status     string             `json:"status"`
	Assignment *RoutingAssignment `json:"assignment,omitempty"`
}

type ProductEvent struct {
	EventID     string
	EventType   string
	Version     string
	VirployeeID string
	Payload     json.RawMessage
}

type Response struct {
	StatusCode  int
	ContentType string
	RetryAfter  string
	Body        []byte
}

type StartAssist interface {
	StartAssist(context.Context, InvocationContext, AssistInput) (AssistRun, error)
}

type GetAssistRun interface {
	GetAssistRun(context.Context, InvocationContext, string, string) (AssistRun, error)
}

type PublishProductEvent interface {
	PublishProductEvent(context.Context, InvocationContext, ProductEvent) (Response, error)
}

type ResolveRouting interface {
	ResolveRouting(context.Context, InvocationContext, RoutingInput) (RoutingResolution, error)
}

type AssistCapabilities interface {
	AssistCapabilities(context.Context, InvocationContext) (any, error)
}

type ProductAuthenticator interface {
	AuthenticateAPIKey(context.Context, string) (MachineBinding, error)
}

type Ports struct {
	StartAssist         StartAssist
	GetAssistRun        GetAssistRun
	PublishProductEvent PublishProductEvent
	ResolveRouting      ResolveRouting
	AssistCapabilities  AssistCapabilities
}

type DownstreamError struct {
	StatusCode int
	RetryAfter string
}

func (e *DownstreamError) Error() string {
	return fmt.Sprintf("product edge downstream returned status %d", e.StatusCode)
}
