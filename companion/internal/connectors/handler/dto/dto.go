package dto

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/capabilities"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// ConnectorResponse respuesta de un conector.
type ConnectorResponse struct {
	ID        string          `json:"id"`
	ConnectorID string        `json:"connector_id"`
	OrgID     string          `json:"org_id,omitempty"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Enabled   bool            `json:"enabled"`
	Status    string          `json:"status"`
	Config    json.RawMessage `json:"config"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
	ArchivedAt string         `json:"archived_at,omitempty"`
	TrashedAt  string         `json:"trashed_at,omitempty"`
	Version   int             `json:"version"`
}

// ConnectorRefreshResult reporta el resultado de refresh por connector.
type ConnectorRefreshResult struct {
	ConnectorID string `json:"connector_id"`
	Refreshed   bool   `json:"refreshed"`
	Error       string `json:"error,omitempty"`
}

// ConnectorRefreshResponse respuesta de POST /v1/connectors/refresh.
type ConnectorRefreshResponse struct {
	Results []ConnectorRefreshResult `json:"results"`
}

// ConnectorListResponse lista de conectores.
type ConnectorListResponse struct {
	Connectors []ConnectorResponse `json:"connectors"`
	Data       []ConnectorResponse `json:"data"`
}

type ConnectorTypesResponse struct {
	Types []domain.ConnectorType `json:"types"`
	Data  []domain.ConnectorType `json:"data"`
}

// ExecuteRequest petición para ejecutar una operación.
type ExecuteRequest struct {
	ConnectorID    string          `json:"connector_id"`
	Operation      string          `json:"operation"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	TaskID         string          `json:"task_id,omitempty"`
	NexusRequestID string          `json:"nexus_request_id,omitempty"`
}

// ActionBindingRequest pide el contrato exacto que debe aprobar Nexus antes
// de una ejecución gobernada. Intencionalmente comparte shape con execute.
type ActionBindingRequest = ExecuteRequest

type ActionBindingResponse struct {
	ActionBinding map[string]any `json:"action_binding"`
	BindingHash   string         `json:"binding_hash"`
}

// ExecutionResponse resultado de una ejecución.
type ExecutionResponse struct {
	ID             string          `json:"id"`
	ConnectorID    string          `json:"connector_id"`
	OrgID          string          `json:"org_id,omitempty"`
	ActorID        string          `json:"actor_id,omitempty"`
	Operation      string          `json:"operation"`
	Status         string          `json:"status"`
	ExternalRef    string          `json:"external_ref"`
	Result         json.RawMessage `json:"result"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	DurationMS     int64           `json:"duration_ms"`
	CreatedAt      string          `json:"created_at"`
}

// ExecutionListResponse lista de ejecuciones.
type ExecutionListResponse struct {
	Executions []ExecutionResponse `json:"executions"`
}

// CapabilityResponse capacidad de un conector.
type CapabilityResponse struct {
	ConnectorID      string                      `json:"connector_id"`
	Kind             string                      `json:"kind"`
	Capabilities     []domain.Capability         `json:"capabilities"`
	RuntimeDecisions []domain.CapabilityDecision `json:"runtime_decisions"`
}

// CapabilitiesListResponse lista de capacidades.
type CapabilitiesListResponse struct {
	Connectors []CapabilityResponse `json:"connectors"`
}

type CapabilityManifestListResponse struct {
	Capabilities []capabilities.Manifest `json:"capabilities"`
}

// SaveConnectorRequest petición para guardar un conector.
type SaveConnectorRequest struct {
	Name    string           `json:"name"`
	Kind    string           `json:"kind"`
	Enabled *bool            `json:"enabled,omitempty"`
	Status  string           `json:"status,omitempty"`
	Config  json.RawMessage  `json:"config,omitempty"`
}

// ConnectorToResponse convierte entidad a DTO.
func ConnectorToResponse(c domain.Connector) ConnectorResponse {
	return ConnectorResponse{
		ID:          c.ID.String(),
		ConnectorID: c.ID.String(),
		OrgID:       c.OrgID,
		Name:        c.Name,
		Kind:        c.Kind,
		Enabled:     c.Enabled,
		Status:      c.Status,
		Config:      MaskConnectorConfig(c.ConfigJSON),
		CreatedAt:   c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   c.UpdatedAt.UTC().Format(time.RFC3339),
		ArchivedAt:  optionalTime(c.ArchivedAt),
		TrashedAt:   optionalTime(c.TrashedAt),
		Version:      c.Version,
	}
}

func optionalTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func MaskConnectorConfig(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return json.RawMessage(`{}`)
	}
	for key := range cfg {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "api_key", "apikey", "token", "secret", "password", "client_secret", "private_key":
			cfg[key] = "***"
		}
	}
	masked, err := json.Marshal(cfg)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return masked
}

// ExecutionToResponse convierte resultado a DTO.
func ExecutionToResponse(e domain.ExecutionResult) ExecutionResponse {
	return ExecutionResponse{
		ID:             e.ID.String(),
		ConnectorID:    e.ConnectorID.String(),
		OrgID:          e.OrgID,
		ActorID:        e.ActorID,
		Operation:      e.Operation,
		Status:         e.Status,
		ExternalRef:    e.ExternalRef,
		Result:         e.ResultJSON,
		Evidence:       e.EvidenceJSON,
		ErrorMessage:   e.ErrorMessage,
		IdempotencyKey: e.IdempotencyKey,
		DurationMS:     e.DurationMS,
		CreatedAt:      e.CreatedAt.UTC().Format(time.RFC3339),
	}
}
