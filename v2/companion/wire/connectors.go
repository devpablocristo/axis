package wire

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	cfg "github.com/devpablocristo/companion-v2/cmd/config"
	"github.com/devpablocristo/companion-v2/internal/connectors"
	"github.com/devpablocristo/companion-v2/internal/mcpgovernance"
	"github.com/devpablocristo/companion-v2/internal/secrets"
	"github.com/devpablocristo/companion-v2/internal/virployees"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type connectorSecretResolver struct {
	environment string
	gcp         secrets.ResolverPort
	lookupEnv   func(string) (string, bool)
}

func (r connectorSecretResolver) Resolve(ctx context.Context, ref string) ([]byte, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "env://") {
		if r.environment == "production" {
			return nil, errors.New("environment connector secrets are disabled in production")
		}
		lookup := r.lookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		value, ok := lookup(strings.TrimPrefix(ref, "env://"))
		if !ok || len(value) < 32 {
			return nil, errors.New("connector signing secret is unavailable")
		}
		return []byte(value), nil
	}
	if r.gcp == nil || !secrets.ValidRef(ref) {
		return nil, errors.New("connector Secret Manager reference is invalid")
	}
	value, err := r.gcp.Resolve(ctx, secrets.Ref(ref))
	if err != nil {
		return nil, errors.New("connector signing secret is unavailable")
	}
	return value.Bytes, nil
}

func configureConnectors(
	ctx context.Context,
	config cfg.Config,
	usecases *virployees.UseCases,
) error {
	registrations, err := connectors.ParseRegistrations(
		config.ConnectorRegistrationsJSON,
		config.Environment,
	)
	if err != nil || len(registrations) == 0 {
		return err
	}
	var gcpResolver secrets.ResolverPort
	for _, registration := range registrations {
		if strings.HasPrefix(registration.Descriptor.SecretRef, "secretmanager://") {
			gcpResolver, err = secretResolver(ctx)
			if err != nil {
				return errors.New("connector Secret Manager resolver is unavailable")
			}
			break
		}
	}
	resolver := connectorSecretResolver{
		environment: config.Environment, gcp: gcpResolver, lookupEnv: os.LookupEnv,
	}
	client := &http.Client{
		Timeout:   5 * time.Minute,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	validator := connectors.SchemaValidatorFunc(func(schema map[string]any, value map[string]any) error {
		return mcpgovernance.ValidateJSONSchema(schema, value)
	})
	registry := connectors.NewRegistry()
	bindings := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		adapter, adapterErr := connectors.NewHTTPAdapter(
			registration.Descriptor,
			config.Environment,
			client,
			resolver,
			validator,
		)
		if adapterErr != nil {
			return adapterErr
		}
		if registerErr := registry.Register(registration.OrgID, adapter); registerErr != nil {
			return registerErr
		}
		bindings[registration.Descriptor.BindingID] = struct{}{}
	}
	executor := connectors.NewExecutor(registry)
	orderedBindings := make([]string, 0, len(bindings))
	for bindingID := range bindings {
		orderedBindings = append(orderedBindings, bindingID)
	}
	sort.Strings(orderedBindings)
	for _, bindingID := range orderedBindings {
		usecases.RegisterExecutorBinding(bindingID, executor)
	}
	return nil
}
