package wire

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/secrets"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2/google"
)

type notificationDestinationResolver struct{ environment string }

func (r notificationDestinationResolver) ResolveNotificationDestination(ctx context.Context, reference string) ([]byte, error) {
	reference = strings.TrimSpace(reference)
	if strings.HasPrefix(reference, "env://") {
		if r.environment == "production" {
			return nil, errors.New("environment notification references are disabled in production")
		}
		value, ok := os.LookupEnv(strings.TrimPrefix(reference, "env://"))
		if !ok || strings.TrimSpace(value) == "" {
			return nil, errors.New("notification destination is unavailable")
		}
		return []byte(value), nil
	}
	tokens, err := google.DefaultTokenSource(ctx, secretManagerScope)
	if err != nil {
		return nil, errors.New("notification destination is unavailable")
	}
	resolver, err := secrets.NewGCPResolver("", tokens, &http.Client{Timeout: 10 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)})
	if err != nil {
		return nil, errors.New("notification destination is unavailable")
	}
	value, err := resolver.Resolve(ctx, secrets.Ref(reference))
	if err != nil {
		return nil, errors.New("notification destination is unavailable")
	}
	defer value.Destroy()
	return append([]byte(nil), value.Bytes...), nil
}
