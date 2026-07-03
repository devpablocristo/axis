package wire

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	cfg "github.com/devpablocristo/bff-v2/cmd/config"
	"github.com/devpablocristo/bff-v2/internal/gateway"
	"github.com/devpablocristo/bff-v2/internal/identity"
	clerkprovider "github.com/devpablocristo/bff-v2/internal/identity/provider/clerk"
	devprovider "github.com/devpablocristo/bff-v2/internal/identity/provider/dev"
	"github.com/devpablocristo/bff-v2/internal/infra/migrations"
	"github.com/devpablocristo/bff-v2/internal/orgs"
	"github.com/devpablocristo/bff-v2/internal/products"
	"github.com/devpablocristo/bff-v2/internal/session"
	"github.com/devpablocristo/bff-v2/internal/tenancy"
	"github.com/devpablocristo/bff-v2/internal/users"
	authnoidc "github.com/devpablocristo/platform/authn/go/oidc"
	postgres "github.com/devpablocristo/platform/databases/postgres/go"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

type Dependencies struct {
	Config cfg.Config
	DB     *postgres.DB
	Router *gin.Engine
	Server *http.Server
}

func Initialize(ctx context.Context) (*Dependencies, error) {
	config := cfg.Load()
	if config.DatabaseURL == "" {
		return nil, fmt.Errorf("BFF_V2_DATABASE_URL or DATABASE_URL is required")
	}
	if config.CompanionBaseURL == "" {
		return nil, fmt.Errorf("BFF_V2_COMPANION_BASE_URL is required")
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

	identityRepo := identity.NewRepository(db.Pool())
	identityUC := identity.NewUseCases(identityRepo)
	identityProvider, orgProvider, invitationProvider := identityProviders(config)
	var tokenVerifier session.TokenVerifierPort
	if config.IdentityProvider == "clerk" && config.ClerkIssuerURL != "" {
		tokenVerifier = authnoidc.NewDiscoveryClient(config.ClerkIssuerURL)
	}

	productsRepo := products.NewRepository(db.Pool())
	tenancyRepo := tenancy.NewRepository(db.Pool())
	tenancyUC := tenancy.NewUseCasesWithProductResolver(tenancyRepo, productsRepo, orgProvider)
	orgsRepo := orgs.NewRepository(db.Pool())
	orgsUC := orgs.NewUseCases(orgsRepo, tenancyUC, orgProvider)
	productsUC := products.NewUseCases(productsRepo, tenancyUC)

	usersRepo := users.NewRepository(db.Pool())
	usersUC := users.NewUseCases(
		usersRepo,
		tenancyUC,
		identityUC,
		identityProvider,
		orgProvider,
		invitationProvider,
		users.Options{InvitationRedirectURL: config.ClerkInviteRedirectURL},
	)

	sessionUC := session.NewUseCases(identityUC, tenancyUC, session.Defaults{
		PrincipalID:    config.DevPrincipalID,
		PrincipalEmail: config.DevPrincipalEmail,
		OrgID:          config.DevOrgID,
	}, tokenVerifier, orgProvider)
	sessionHandler := session.NewHandler(sessionUC)

	gatewayUC, err := gateway.NewUseCases(tenancyUC, config.CompanionBaseURL)
	if err != nil {
		db.Close()
		return nil, err
	}
	gatewayHandler := gateway.NewHandler(gatewayUC, gateway.Options{
		DefaultPrincipalID:  config.DevPrincipalID,
		SupervisorValidator: usersUC,
	})

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(ginmw.NewBodySizeLimit(config.MaxBodyBytes))
	router.Use(ginmw.NewCORS(ginmw.CORSConfig{
		Origins: config.CORSOrigins,
		AllowHeaders: []string{
			"Authorization",
			"Content-Type",
			"X-Actor-ID",
			"X-Actor-Email",
			"X-Axis-Org-ID",
			"X-Tenant-ID",
		},
	}))
	ginmw.RegisterHealthEndpoints(router, db.Ping)

	api := router.Group("/api")
	identity.NewWebhookHandler(identityUC, tenancyUC, config.ClerkWebhookSecret).Routes(api)
	sessionHandler.Routes(api)
	orgs.NewHandler(orgsUC, orgs.HandlerOptions{
		DefaultPrincipalID: config.DevPrincipalID,
	}).Routes(api)
	products.NewHandler(productsUC, products.HandlerOptions{
		DefaultPrincipalID: config.DevPrincipalID,
	}).Routes(api)
	tenancy.NewHandler(tenancyUC, tenancy.HandlerOptions{
		DefaultPrincipalID: config.DevPrincipalID,
	}).Routes(api)
	users.NewHandler(usersUC, users.HandlerOptions{
		DefaultPrincipalID: config.DevPrincipalID,
	}).Routes(api)
	gatewayHandler.Routes(api)

	server := &http.Server{
		Addr:    config.Addr(),
		Handler: router,
	}

	return &Dependencies{
		Config: config,
		DB:     db,
		Router: router,
		Server: server,
	}, nil
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
	if d == nil || d.DB == nil {
		return
	}
	d.DB.Close()
}
