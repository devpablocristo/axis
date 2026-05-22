package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// MockConnector conector de prueba que loguea sin ejecutar.
type MockConnector struct{}

// NewMockConnector crea un conector mock.
func NewMockConnector() *MockConnector {
	return &MockConnector{}
}

func (m *MockConnector) ID() string   { return "mock" }
func (m *MockConnector) Kind() string { return "mock" }

func (m *MockConnector) Capabilities() []domain.Capability {
	return []domain.Capability{
		{
			Operation:      "mock.echo",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type": "object",
			},
			EvidenceFields: []string{"mock", "message"},
		},
		{
			Operation:      "mock.write",
			Mode:           domain.CapabilityModeWrite,
			SideEffect:     true,
			RiskClass:      "low",
			RequiresGovernance: true,
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"message"},
			},
			EvidenceFields: []string{"mock", "message", "external_ref"},
		},
	}
}

func (m *MockConnector) Validate(spec domain.ExecutionSpec) error {
	return nil
}

func (m *MockConnector) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	slog.Info("mock connector execute",
		"operation", spec.Operation,
		"payload", string(spec.Payload),
		"idempotency_key", spec.IdempotencyKey,
	)
	resultJSON, err := json.Marshal(map[string]string{
		"mock":    "true",
		"message": "operation logged successfully",
	})
	if err != nil {
		// map[string]string es 100% marshalable; este return es por contrato.
		return domain.ExecutionResult{}, fmt.Errorf("marshal mock result: %w", err)
	}
	return domain.ExecutionResult{
		ID:              uuid.New(),
		ConnectorID:     spec.ConnectorID,
		OrgID:           spec.OrgID,
		ActorID:         spec.ActorID,
		Operation:       spec.Operation,
		Status:          domain.ExecSuccess,
		ExternalRef:     "mock-" + uuid.New().String()[:8],
		Payload:         spec.Payload,
		ResultJSON:      json.RawMessage(resultJSON),
		DurationMS:      1,
		IdempotencyKey:  spec.IdempotencyKey,
		TaskID:          spec.TaskID,
		GovernanceRequestID: spec.GovernanceRequestID,
		CreatedAt:       time.Now().UTC(),
	}, nil
}
