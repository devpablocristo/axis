package wire

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	cfg "github.com/devpablocristo/nexus-v2/cmd/config"
	"github.com/devpablocristo/nexus-v2/internal/actiontypes"
	"github.com/devpablocristo/nexus-v2/internal/approvals"
	"github.com/devpablocristo/nexus-v2/internal/governance"
	"github.com/devpablocristo/nexus-v2/internal/infra/migrations"
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
		return nil, fmt.Errorf("NEXUS_V2_DATABASE_URL or DATABASE_URL is required")
	}

	dbConfig, err := postgres.ConfigFromEnv("NEXUS_V2_DB", "nexus_v2")
	if err != nil {
		return nil, err
	}
	db, err := postgres.OpenWithConfig(ctx, config.DatabaseURL, dbConfig)
	if err != nil {
		return nil, err
	}

	if config.RunMigrations {
		if err := postgres.MigrateUp(ctx, db, "nexus_v2", migrations.Files, migrations.Dir); err != nil {
			db.Close()
			return nil, err
		}
	}

	actionTypeRepo := actiontypes.NewRepository(db.Pool())
	actionTypeUseCases := actiontypes.NewUseCases(actionTypeRepo)
	actionTypeHandler := actiontypes.NewHandler(actionTypeUseCases)

	governanceRepo := governance.NewRepository(db.Pool())
	governanceUseCases := governance.NewUseCases(actionTypeUseCases, governanceRepo)
	governanceHandler := governance.NewHandler(governanceUseCases)

	approvalsRepo := approvals.NewRepository(db.Pool())
	approvalsUseCases := approvals.NewUseCases(approvalsRepo)
	approvalsHandler := approvals.NewHandler(approvalsUseCases)

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
			"X-Tenant-ID",
		},
	}))
	ginmw.RegisterHealthEndpoints(router, db.Ping)

	api := router.Group("/v1")
	actionTypeHandler.Routes(api)
	governanceHandler.Routes(api)
	approvalsHandler.Routes(api)

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
