package wire

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	cfg "github.com/devpablocristo/companion-v2/cmd/config"
	"github.com/devpablocristo/companion-v2/internal/adapters/out/googlecalendar"
	"github.com/devpablocristo/companion-v2/internal/attestation"
	"github.com/devpablocristo/companion-v2/internal/secrets"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2/google"
)

const secretManagerScope = "https://www.googleapis.com/auth/cloud-platform"

func secretResolver(ctx context.Context) (secrets.ResolverPort, error) {
	tokens, err := google.DefaultTokenSource(ctx, secretManagerScope)
	if err != nil {
		return nil, err
	}
	return secrets.NewGCPResolver("", tokens, &http.Client{Timeout: 10 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)})
}

func resolveAttestationKey(ctx context.Context, config cfg.Config) ([]byte, error) {
	if config.ExecutorAttestationSecretRef != "" {
		resolver, err := secretResolver(ctx)
		if err != nil {
			return nil, fmt.Errorf("executor attestation secret resolver: %w", err)
		}
		value, err := resolver.Resolve(ctx, secrets.Ref(config.ExecutorAttestationSecretRef))
		if err != nil {
			return nil, fmt.Errorf("resolve executor attestation secret: %w", err)
		}
		return value.Bytes, nil
	}
	if config.Environment == "production" {
		return nil, errors.New("production requires COMPANION_V2_EXECUTOR_ATTESTATION_SECRET_REF")
	}
	return attestation.DeriveDevelopmentKey(config.InternalAuthSecret), nil
}

func resolveGoogleCalendarAPI(ctx context.Context, config cfg.Config) (googlecalendar.CalendarAPI, error) {
	if config.GoogleCalendarSecretRef == "" {
		if config.Environment == "production" {
			return nil, errors.New("production google_calendar requires COMPANION_V2_GOOGLE_CALENDAR_SECRET_REF")
		}
		return googlecalendar.NewGoogleCalendarAPI(ctx)
	}
	resolver, err := secretResolver(ctx)
	if err != nil {
		return nil, fmt.Errorf("google calendar secret resolver: %w", err)
	}
	value, err := resolver.Resolve(ctx, secrets.Ref(config.GoogleCalendarSecretRef))
	if err != nil {
		return nil, fmt.Errorf("resolve google calendar credentials: %w", err)
	}
	defer value.Destroy()
	return googlecalendar.NewGoogleCalendarAPIFromJSON(ctx, value.Bytes)
}
