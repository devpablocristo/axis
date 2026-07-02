package wire

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	cfg "github.com/devpablocristo/bff-v2/cmd/config"
	"github.com/devpablocristo/bff-v2/internal/gateway"
	"github.com/devpablocristo/bff-v2/internal/identity"
	"github.com/devpablocristo/bff-v2/internal/infra/migrations"
	"github.com/devpablocristo/bff-v2/internal/session"
	"github.com/devpablocristo/bff-v2/internal/tenancy"
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

	tenancyRepo := tenancy.NewRepository(db.Pool())
	tenancyUC := tenancy.NewUseCases(tenancyRepo)

	sessionUC := session.NewUseCases(identityUC, tenancyUC, session.Defaults{
		PrincipalID:    config.DevPrincipalID,
		PrincipalEmail: config.DevPrincipalEmail,
		PrincipalName:  config.DevPrincipalName,
		OrgID:          config.DevOrgID,
	})
	sessionHandler := session.NewHandler(sessionUC)

	gatewayUC, err := gateway.NewUseCases(tenancyUC, config.CompanionBaseURL)
	if err != nil {
		db.Close()
		return nil, err
	}
	gatewayHandler := gateway.NewHandler(gatewayUC, gateway.Options{
		DefaultPrincipalID: config.DevPrincipalID,
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
			"X-Actor-Name",
			"X-Axis-Org-ID",
			"X-Tenant-ID",
		},
	}))
	ginmw.RegisterHealthEndpoints(router, db.Ping)

	api := router.Group("/api")
	sessionHandler.Routes(api)
	tenancy.NewHandler(tenancyUC, tenancy.HandlerOptions{
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

func (d *Dependencies) Close() {
	if d == nil || d.DB == nil {
		return
	}
	d.DB.Close()
}
