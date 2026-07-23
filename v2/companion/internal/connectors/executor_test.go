package connectors

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/google/uuid"
)

type fakeConnector struct {
	descriptor Descriptor
	invoke     func(context.Context, InvokeRequest) (InvocationResult, error)
	status     func(context.Context, string) (InvocationResult, error)
}

func (f *fakeConnector) Descriptor() Descriptor { return f.descriptor }

func (f *fakeConnector) Invoke(ctx context.Context, request InvokeRequest) (InvocationResult, error) {
	return f.invoke(ctx, request)
}

func (f *fakeConnector) Status(ctx context.Context, invocationID string) (InvocationResult, error) {
	return f.status(ctx, invocationID)
}

func TestExecutorRecoversAmbiguousInvocationThroughStatus(t *testing.T) {
	capabilityID := uuid.New()
	descriptor := testDescriptor("https://connector.example", capabilityID.String())
	inputHash, _ := schemaHash(descriptor.Operations[0].InputSchema)
	outputHash, _ := schemaHash(descriptor.Operations[0].OutputSchema)
	action, err := preparedactions.NewV2(preparedactions.V2Input{
		CapabilityID: capabilityID, ManifestHash: strings.Repeat("a", 64),
		ExecutorBindingID: descriptor.BindingID, Operation: descriptor.Operations[0].Name,
		InputSchemaHash: inputHash, OutputSchemaHash: outputHash,
		Arguments: map[string]any{"query": "labs"}, RequiredAutonomy: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt := virployees.ExecutionAttempt{ID: uuid.New(), IdempotencyKey: "idem-1"}
	statusCalls := 0
	connector := &fakeConnector{
		descriptor: descriptor,
		invoke: func(context.Context, InvokeRequest) (InvocationResult, error) {
			return InvocationResult{}, &TransportError{Ambiguous: true, Cause: errors.New("timeout")}
		},
		status: func(_ context.Context, invocationID string) (InvocationResult, error) {
			statusCalls++
			return InvocationResult{
				SchemaVersion: SchemaVersion, InvocationID: invocationID,
				ProductID: descriptor.ProductID, CapabilityID: capabilityID.String(),
				Operation: descriptor.Operations[0].Name, Status: "succeeded",
				Payload: map[string]any{"resource_id": "result-1"},
			}, nil
		},
	}
	registry := NewRegistry()
	if err := registry.Register("org-1", connector); err != nil {
		t.Fatal(err)
	}
	outcome, err := NewExecutor(registry).ExecuteV2(
		context.Background(), "org-1", uuid.New(), attempt, action,
	)
	if err != nil || statusCalls != 1 || outcome.ResourceID != "result-1" ||
		outcome.Mode != "connector:"+descriptor.BindingID || !outcome.ExternalEffects {
		t.Fatalf("outcome=%+v status_calls=%d err=%v", outcome, statusCalls, err)
	}
}

func TestExecutorFailsClosedForMissingOrganizationBinding(t *testing.T) {
	capabilityID := uuid.New()
	descriptor := testDescriptor("https://connector.example", capabilityID.String())
	inputHash, _ := schemaHash(descriptor.Operations[0].InputSchema)
	outputHash, _ := schemaHash(descriptor.Operations[0].OutputSchema)
	action, err := preparedactions.NewV2(preparedactions.V2Input{
		CapabilityID: capabilityID, ManifestHash: strings.Repeat("a", 64),
		ExecutorBindingID: descriptor.BindingID, Operation: descriptor.Operations[0].Name,
		InputSchemaHash: inputHash, OutputSchemaHash: outputHash,
		Arguments: map[string]any{}, RequiredAutonomy: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	connector := &fakeConnector{
		descriptor: descriptor,
		invoke: func(context.Context, InvokeRequest) (InvocationResult, error) {
			t.Fatal("connector from another organization must not be invoked")
			return InvocationResult{}, nil
		},
		status: func(context.Context, string) (InvocationResult, error) {
			return InvocationResult{}, nil
		},
	}
	if err := registry.Register("org-2", connector); err != nil {
		t.Fatal(err)
	}
	_, err = NewExecutor(registry).ExecuteV2(
		context.Background(),
		"org-1",
		uuid.New(),
		virployees.ExecutionAttempt{ID: uuid.New(), IdempotencyKey: "idem"},
		action,
	)
	var missing *BindingNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("missing org binding must fail closed, got %T %v", err, err)
	}
}

func TestExecutorFailsClosedWhenAuthorizedSchemaNoLongerMatchesConnector(t *testing.T) {
	capabilityID := uuid.New()
	descriptor := testDescriptor("https://connector.example", capabilityID.String())
	outputHash, _ := schemaHash(descriptor.Operations[0].OutputSchema)
	action, err := preparedactions.NewV2(preparedactions.V2Input{
		CapabilityID: capabilityID, ManifestHash: strings.Repeat("a", 64),
		ExecutorBindingID: descriptor.BindingID, Operation: descriptor.Operations[0].Name,
		InputSchemaHash: strings.Repeat("b", 64), OutputSchemaHash: outputHash,
		Arguments: map[string]any{}, RequiredAutonomy: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}
	connector := &fakeConnector{
		descriptor: descriptor,
		invoke: func(context.Context, InvokeRequest) (InvocationResult, error) {
			t.Fatal("schema drift must block before connector invocation")
			return InvocationResult{}, nil
		},
		status: func(context.Context, string) (InvocationResult, error) {
			return InvocationResult{}, nil
		},
	}
	registry := NewRegistry()
	if err := registry.Register("org-1", connector); err != nil {
		t.Fatal(err)
	}
	_, err = NewExecutor(registry).ExecuteV2(
		context.Background(),
		"org-1",
		uuid.New(),
		virployees.ExecutionAttempt{ID: uuid.New(), IdempotencyKey: "idem"},
		action,
	)
	if err == nil || !strings.Contains(err.Error(), "schema changed") {
		t.Fatalf("schema drift must fail closed, got %v", err)
	}
}

func TestParseRegistrationsRejectsInlineSecretOutsideDevelopment(t *testing.T) {
	raw := `[{"org_id":"org-1","descriptor":{"schema_version":"axis.connector.v1",` +
		`"binding_id":"records.connector","base_url":"https://connector.example",` +
		`"product_id":"` + testProductID + `","secret_ref":"env://CONNECTOR_SECRET",` +
		`"operations":[{"name":"records.search","capability_id":"` + uuid.NewString() + `",` +
		`"input_schema":{"type":"object"},"output_schema":{"type":"object"},"timeout_ms":1000}]}}]`
	if _, err := ParseRegistrations(raw, "production"); err == nil {
		t.Fatal("production connector registration must reject env secret references")
	}
	if registrations, err := ParseRegistrations(raw, "development"); err != nil || len(registrations) != 1 {
		t.Fatalf("development registration: %+v, %v", registrations, err)
	}
}
