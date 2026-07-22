package wire

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	cfg "github.com/devpablocristo/nexus-v2/cmd/config"
	"github.com/devpablocristo/nexus-v2/internal/attestation"
	"github.com/devpablocristo/nexus-v2/internal/secrets"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2/google"
)

const secretManagerScope = "https://www.googleapis.com/auth/cloud-platform"

func resolveAttestationKey(ctx context.Context, config cfg.Config) ([]byte, error) {
	if config.ExecutorAttestationSecretRef == "" {
		if config.Environment == "production" {
			return nil, errors.New("production requires NEXUS_V2_EXECUTOR_ATTESTATION_SECRET_REF")
		}
		return attestation.DeriveDevelopmentKey(config.InternalAuthSecret), nil
	}
	tokens, err := google.DefaultTokenSource(ctx, secretManagerScope)
	if err != nil {
		return nil, fmt.Errorf("executor attestation secret resolver: %w", err)
	}
	resolver, err := secrets.NewGCPResolver("", tokens, &http.Client{Timeout: 10 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)})
	if err != nil {
		return nil, err
	}
	value, err := resolver.Resolve(ctx, secrets.Ref(config.ExecutorAttestationSecretRef))
	if err != nil {
		return nil, fmt.Errorf("resolve executor attestation secret: %w", err)
	}
	return value.Bytes, nil
}
