package wire

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	cfg "github.com/devpablocristo/bff-v2/cmd/config"
	"github.com/devpablocristo/bff-v2/internal/gateway"
	"github.com/devpablocristo/bff-v2/internal/identity"
	clerkprovider "github.com/devpablocristo/bff-v2/internal/identity/provider/clerk"
	devprovider "github.com/devpablocristo/bff-v2/internal/identity/provider/dev"
	"github.com/devpablocristo/bff-v2/internal/inbound"
	"github.com/devpablocristo/bff-v2/internal/infra/migrations"
	"github.com/devpablocristo/bff-v2/internal/orgs"
	"github.com/devpablocristo/bff-v2/internal/products"
	"github.com/devpablocristo/bff-v2/internal/session"
	"github.com/devpablocristo/bff-v2/internal/users"
	authnoidc "github.com/devpablocristo/platform/authn/go/oidc"
	postgres "github.com/devpablocristo/platform/databases/postgres/go"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	observability "github.com/devpablocristo/platform/observability/go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Dependencies struct {
	Config         cfg.Config
	DB             *postgres.DB
	Router         *gin.Engine
	Server         *http.Server
	tracerShutdown func(context.Context) error
}

func Initialize(ctx context.Context) (*Dependencies, error) {
	config := cfg.Load()
	if err := validateAuthConfig(config); err != nil {
		return nil, err
	}
	if config.DatabaseURL == "" {
		return nil, fmt.Errorf("BFF_V2_DATABASE_URL or DATABASE_URL is required")
	}
	if config.CompanionBaseURL == "" {
		return nil, fmt.Errorf("BFF_V2_COMPANION_BASE_URL is required")
	}
	if config.NexusBaseURL == "" {
		return nil, fmt.Errorf("BFF_V2_NEXUS_BASE_URL is required")
	}

	dbConfig, err := postgres.ConfigFromEnv("BFF_V2_DB", "bff_v2")
	if err != nil {
		return nil, err
	}
	db, err := postgres.OpenWithConfig(ctx, config.DatabaseURL, dbConfig)
	if err != nil {
		return nil, err
	}

	if config.RunMigrations {
		if err := postgres.MigrateUp(ctx, db, "bff_v2", migrations.Files, migrations.Dir); err != nil {
			db.Close()
			return nil, err
		}
	}

	logger := observability.NewJSONLogger("bff-v2")
	tracerShutdown, err := observability.NewTracerProvider(ctx, observability.TracingConfig{
		ServiceName:    "bff-v2",
		ServiceVersion: config.ServiceVersion,
		Environment:    config.Environment,
		Exporter:       config.OTelExporter,
		OTLPEndpoint:   config.OTelEndpoint,
		OTLPInsecure:   config.OTelInsecure,
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	identityRepo := identity.NewRepository(db.Pool())
	identityUC := identity.NewUseCases(identityRepo)
	identityProvider, orgProvider, invitationProvider := identityProviders(config)
	var tokenVerifier session.TokenVerifierPort
	if config.IdentityProvider == "clerk" && config.ClerkIssuerURL != "" {
		tokenVerifier = authnoidc.NewDiscoveryClient(config.ClerkIssuerURL)
	}

	productsRepo := products.NewRepository(db.Pool())
	productsUC := products.NewUseCases(productsRepo, orgProvider)
	orgsRepo := orgs.NewRepository(db.Pool())
	orgsUC := orgs.NewUseCases(orgsRepo, productsUC, orgProvider, identityUC)

	usersRepo := users.NewRepository(db.Pool())
	usersUC := users.NewUseCases(
		usersRepo,
		productsUC,
		identityUC,
		identityProvider,
		orgProvider,
		invitationProvider,
		users.Options{InvitationRedirectURL: config.ClerkInviteRedirectURL},
	)

	sessionUC := session.NewUseCases(identityUC, productsUC, session.Defaults{
		PrincipalID:    config.DevPrincipalID,
		PrincipalEmail: config.DevPrincipalEmail,
		OrgID:          config.DevOrgID,
	}, tokenVerifier, orgProvider)
	sessionHandler := session.NewHandler(sessionUC)

	gatewayUC, err := gateway.NewUseCases(productsUC, config.CompanionBaseURL, config.NexusBaseURL)
	if err != nil {
		db.Close()
		return nil, err
	}
	gatewayHandler := gateway.NewHandler(gatewayUC, gateway.Options{
		DefaultPrincipalID:  config.DevPrincipalID,
		InternalAuthSecret:  config.InternalAuthSecret,
		SupervisorValidator: usersUC,
		Client:              &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
	})

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(routeAwareBodySizeLimit(config.MaxBodyBytes, config.KnowledgeUploadMaxBodyBytes))
	router.Use(ginmw.NewCORS(ginmw.CORSConfig{
		Origins: config.CORSOrigins,
		AllowHeaders: []string{
			"Authorization",
			"Content-Type",
			"X-Actor-ID",
			"X-Actor-Email",
			"X-Axis-Org-ID",
			"X-Product-ID",
			"X-Axis-Virployee-ID",
			"X-Axis-Subject-ID",
			"X-Axis-Case-ID",
			"X-Idempotency-Key",
		},
	}))
	ginmw.RegisterHealthEndpoints(router, db.Ping)

	api := router.Group("/api")
	identity.NewWebhookHandler(identityUC, productsUC, config.ClerkWebhookSecret).Routes(api)
	protected := api.Group("")
	protected.Use(session.NewAuthenticationMiddleware(sessionUC, config.IdentityProvider == "dev" || config.Environment == "test"))
	sessionHandler.Routes(protected)
	orgs.NewHandler(orgsUC, orgs.HandlerOptions{
		DefaultPrincipalID: config.DevPrincipalID,
	}).Routes(protected)
	products.NewHandler(productsUC, products.HandlerOptions{
		DefaultPrincipalID: config.DevPrincipalID,
	}).OrganizationProductRoutes(protected)
	users.NewHandler(usersUC, users.HandlerOptions{
		DefaultPrincipalID: config.DevPrincipalID,
	}).Routes(protected)
	gatewayHandler.Routes(protected)

	// Product-facing inbound edge (machine auth via API key), mounted at the root
	// OUTSIDE the human-session middleware. A configured consumer POSTs
	// /v1/assist-runs with an API key that maps to a product + virployee.
	if bindings := inbound.ParseBindings(config.ProductAPIKeys); len(bindings) > 0 {
		inbound.NewHandler(bindings, config.CompanionBaseURL, config.InternalAuthSecret, nil).Routes(router)
	}

	server := &http.Server{
		Addr:    config.Addr(),
		Handler: tracedServerHandler("bff-v2", observability.Middleware(logger, router)),
	}

	return &Dependencies{
		Config:         config,
		DB:             db,
		Router:         router,
		Server:         server,
		tracerShutdown: tracerShutdown,
	}, nil
}

// tracedServerHandler wraps an HTTP handler with an OTel server span per
// request, extracting incoming trace context so the trace continues across
// services. Health probes are excluded to avoid flooding traces.
func tracedServerHandler(service string, h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, service,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
		otelhttp.WithFilter(func(r *http.Request) bool {
			p := r.URL.Path
			return p != "/readyz" && p != "/healthz"
		}),
	)
}

func validateAuthConfig(config cfg.Config) error {
	if strings.TrimSpace(config.InternalAuthSecret) == "" {
		return fmt.Errorf("BFF_V2_INTERNAL_AUTH_SECRET is required")
	}
	switch config.IdentityProvider {
	case "clerk":
		if config.Environment != "test" && strings.TrimSpace(config.ClerkIssuerURL) == "" {
			return fmt.Errorf("BFF_V2_CLERK_ISSUER_URL is required when Clerk authentication is enabled")
		}
	case "dev":
		if config.Environment != "development" && config.Environment != "test" {
			return fmt.Errorf("development identity provider is not allowed in %s", config.Environment)
		}
	default:
		return fmt.Errorf("unsupported BFF_V2_IDENTITY_PROVIDER %q", config.IdentityProvider)
	}
	return nil
}

func identityProviders(config cfg.Config) (
	identity.IdentityProviderPort,
	identity.OrgProviderPort,
	identity.InvitationProviderPort,
) {
	if config.IdentityProvider == "clerk" {
		provider := clerkprovider.NewProvider(clerkprovider.Config{
			SecretKey:         config.ClerkSecretKey,
			BaseURL:           config.ClerkAPIBaseURL,
			InviteRedirectURL: config.ClerkInviteRedirectURL,
		})
		return provider, provider, provider
	}
	provider := devprovider.NewProvider()
	return provider, provider, provider
}

func (d *Dependencies) Close() {
	if d == nil {
		return
	}
	if d.tracerShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.tracerShutdown(shutdownCtx)
		cancel()
	}
	if d.DB != nil {
		d.DB.Close()
	}
}
