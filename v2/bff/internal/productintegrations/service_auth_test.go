package productintegrations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/devpablocristo/bff-v2/internal/productedge"
	"github.com/google/uuid"
)

type credentialRepositoryFunc func(context.Context, []byte) (ActiveCredential, error)

func (f credentialRepositoryFunc) ActiveByDigest(ctx context.Context, digest []byte) (ActiveCredential, error) {
	return f(ctx, digest)
}

func TestAuthenticateAPIKeyBuildsInvocationFromRepositoryAndV3Contract(t *testing.T) {
	virployeeID := uuid.New()
	capabilityID := uuid.New()
	contract, err := normalizeContract(Contract{
		SchemaVersion: FunctionalSchemaVersion,
		Authentication: AuthenticationRequirements{
			Mode: "api_key", Scopes: []string{"assist.write"},
		},
		Limits:      Limits{MaxRequestBytes: 1024, MaxResultBytes: 2048, RatePerMinute: 60},
		Entrypoints: []Entrypoint{{Kind: "virployee", ID: virployeeID}},
		Capabilities: []FunctionalCapability{{
			ID: capabilityID, Name: "Analizar estudios", Version: "1",
			ManifestHash: strings.Repeat("a", 64), ExecutorBindingID: "studies.adapter",
			Operation: "analyze", InputSchemaHash: strings.Repeat("b", 64),
			OutputSchemaHash: strings.Repeat("c", 64),
		}},
		ConnectorBindings: []ConnectorBinding{{
			ID: "studies.adapter", ConnectorID: "tenant.studies", Operation: "analyze",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(contract)
	integrationID := uuid.New()
	repository := credentialRepositoryFunc(func(context.Context, []byte) (ActiveCredential, error) {
		return ActiveCredential{
			Context: productedge.InvocationContext{
				OrgID: "org-1", ProductID: "product-1", ProductSurface: "surface-a",
				PrincipalID: "service:consumer", Scopes: []string{"assist.write"},
				IntegrationRevision: 3, IntegrationHash: strings.Repeat("d", 64),
			},
			IntegrationID: integrationID,
			Contract:      raw,
		}, nil
	})
	service := NewServiceWithRepository(
		nil,
		NewParticipantRegistry().WithInvocationProjection(ProductInvocationProjection),
		repository,
	)
	binding, err := service.AuthenticateAPIKey(context.Background(), "axis_pk_012345678901234567890123456789")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if binding.Context.IntegrationID != integrationID.String() ||
		binding.Context.PrincipalType != "service" ||
		binding.Context.AccessMode != productedge.AccessModeDirect ||
		len(binding.AllowedVirployeeIDs) != 1 || binding.AllowedVirployeeIDs[0] != virployeeID.String() ||
		len(binding.AllowedCapabilities) != 1 || binding.AllowedCapabilities[0].ID != capabilityID.String() {
		t.Fatalf("unexpected machine binding: %+v", binding)
	}
}
