package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/google/uuid"
)

type BindingNotFoundError struct {
	OrgID     string
	BindingID string
}

func (e *BindingNotFoundError) Error() string {
	return "connector executor binding is not registered for organization"
}

type ExecutionError struct{ Code string }

func (e *ExecutionError) Error() string {
	if strings.TrimSpace(e.Code) == "" {
		return "connector execution failed"
	}
	return "connector execution failed: " + e.Code
}

// Executor is the single generic outbound executor used for all connector
// bindings. Domain names and product identifiers never select code; the
// organization-scoped registry and the immutable PreparedActionV2 binding do.
type Executor struct {
	registry     *Registry
	pollInterval time.Duration
}

func NewExecutor(registry *Registry) *Executor {
	return &Executor{registry: registry, pollInterval: 250 * time.Millisecond}
}

func (e *Executor) ExecuteV2(
	ctx context.Context,
	orgID string,
	_ uuid.UUID,
	attempt virployees.ExecutionAttempt,
	action preparedactions.PreparedActionV2,
) (virployees.ExecutionOutcome, error) {
	if e == nil || e.registry == nil {
		return virployees.ExecutionOutcome{}, &BindingNotFoundError{OrgID: orgID, BindingID: action.ExecutorBindingID}
	}
	if err := action.Validate(); err != nil {
		return virployees.ExecutionOutcome{}, err
	}
	connector, ok := e.registry.Resolve(orgID, action.ExecutorBindingID)
	if !ok {
		return virployees.ExecutionOutcome{}, &BindingNotFoundError{OrgID: orgID, BindingID: action.ExecutorBindingID}
	}
	descriptor := connector.Descriptor()
	operation, ok := descriptor.Operation(action.Operation)
	if !ok || operation.CapabilityID != action.CapabilityID {
		return virployees.ExecutionOutcome{}, fmt.Errorf("connector action binding does not match descriptor")
	}
	inputHash, err := schemaHash(operation.InputSchema)
	if err != nil || inputHash != action.InputSchemaHash {
		return virployees.ExecutionOutcome{}, fmt.Errorf("connector input schema changed after authorization")
	}
	outputHash, err := schemaHash(operation.OutputSchema)
	if err != nil || outputHash != action.OutputSchemaHash {
		return virployees.ExecutionOutcome{}, fmt.Errorf("connector output schema changed after authorization")
	}
	arguments, err := action.ArgumentsMap()
	if err != nil {
		return virployees.ExecutionOutcome{}, err
	}
	invocationID := attempt.ID.String()
	if attempt.ID == uuid.Nil || strings.TrimSpace(attempt.IdempotencyKey) == "" {
		return virployees.ExecutionOutcome{}, fmt.Errorf("connector execution attempt binding is invalid")
	}
	timeout := time.Duration(operation.TimeoutMS) * time.Millisecond
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request := InvokeRequest{
		SchemaVersion: SchemaVersion, InvocationID: invocationID,
		OrgID: strings.TrimSpace(orgID), ProductID: descriptor.ProductID,
		CapabilityID: action.CapabilityID, Operation: action.Operation,
		Arguments: arguments, IdempotencyKey: attempt.IdempotencyKey,
	}
	result, invokeErr := connector.Invoke(execCtx, request)
	if invokeErr != nil {
		var transportErr *TransportError
		if !errors.As(invokeErr, &transportErr) || !transportErr.Ambiguous {
			return virployees.ExecutionOutcome{}, invokeErr
		}
		result, invokeErr = connector.Status(execCtx, invocationID)
		if invokeErr != nil {
			return virployees.ExecutionOutcome{}, invokeErr
		}
	}
	result, err = e.awaitTerminal(execCtx, connector, invocationID, result)
	if err != nil {
		return virployees.ExecutionOutcome{}, err
	}
	if err := validResult(
		result,
		invocationID,
		descriptor.ProductID,
		action.CapabilityID,
		action.Operation,
	); err != nil {
		return virployees.ExecutionOutcome{}, err
	}
	if result.Status == "failed" {
		return virployees.ExecutionOutcome{
			Mode: "connector:" + action.ExecutorBindingID, ExternalEffects: true,
		}, &ExecutionError{Code: result.ErrorCode}
	}
	payload := make(map[string]any, len(result.Payload)+1)
	for key, value := range result.Payload {
		payload[key] = value
	}
	payload["connector_invocation_id"] = invocationID
	resourceID, _ := payload["resource_id"].(string)
	return virployees.ExecutionOutcome{
		ResourceID: resourceID, Mode: "connector:" + action.ExecutorBindingID,
		ExternalEffects: true, Result: payload,
	}, nil
}

func (e *Executor) awaitTerminal(
	ctx context.Context,
	connector Connector,
	invocationID string,
	result InvocationResult,
) (InvocationResult, error) {
	interval := e.pollInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	for result.Status == "pending" {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return InvocationResult{}, &TransportError{
				InvocationID: invocationID, Ambiguous: true, Cause: ctx.Err(),
			}
		case <-timer.C:
		}
		next, err := connector.Status(ctx, invocationID)
		if err != nil {
			return InvocationResult{}, err
		}
		result = next
	}
	return result, nil
}

func schemaHash(schema map[string]any) (string, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

var _ virployees.ActionExecutorV2Port = (*Executor)(nil)
