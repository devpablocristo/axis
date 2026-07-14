package wire

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	cfg "github.com/devpablocristo/companion-v2/cmd/config"
	"github.com/devpablocristo/companion-v2/internal/capabilities"
	"github.com/devpablocristo/companion-v2/internal/infra/migrations"
	"github.com/devpablocristo/companion-v2/internal/jobroles"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/companion-v2/internal/nexusclient"
	"github.com/devpablocristo/companion-v2/internal/profiletemplates"
	"github.com/devpablocristo/companion-v2/internal/virployees"
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
		return nil, fmt.Errorf("COMPANION_V2_DATABASE_URL or DATABASE_URL is required")
	}
	if config.InternalAuthSecret == "" {
		return nil, fmt.Errorf("COMPANION_V2_INTERNAL_AUTH_SECRET is required")
	}

	dbConfig, err := postgres.ConfigFromEnv("COMPANION_V2_DB", "companion_v2")
	if err != nil {
		return nil, err
	}
	db, err := postgres.OpenWithConfig(ctx, config.DatabaseURL, dbConfig)
	if err != nil {
		return nil, err
	}

	if config.RunMigrations {
		if err := postgres.MigrateUp(ctx, db, "companion_v2", migrations.Files, migrations.Dir); err != nil {
			db.Close()
			return nil, err
		}
	}

	jobRolesRepo := jobroles.NewRepository(db.Pool())
	jobRolesUsecases, err := jobroles.NewUseCases(jobRolesRepo)
	if err != nil {
		db.Close()
		return nil, err
	}
	jobRolesHandler := jobroles.NewHandler(jobRolesUsecases)

	capabilitiesRepo := capabilities.NewRepository(db.Pool())
	capabilitiesUsecases, err := capabilities.NewUseCases(capabilitiesRepo)
	if err != nil {
		db.Close()
		return nil, err
	}
	capabilitiesHandler := capabilities.NewHandler(capabilitiesUsecases)

	profileTemplatesRepo := profiletemplates.NewRepository(db.Pool())
	profileTemplatesUsecases, err := profiletemplates.NewUseCases(profileTemplatesRepo)
	if err != nil {
		db.Close()
		return nil, err
	}
	profileTemplatesHandler := profiletemplates.NewHandler(profileTemplatesUsecases)

	virployeesRepo := virployees.NewRepository(db.Pool())
	virployeesUsecases, err := virployees.NewUseCases(virployeesRepo, jobRolesUsecases)
	if err != nil {
		db.Close()
		return nil, err
	}
	virployeesUsecases.SetCapabilityValidator(capabilitiesUsecases)
	virployeesUsecases.SetProfileTemplateReader(profileTemplatesUsecases)
	if config.NexusBaseURL != "" {
		nexusClient := nexusclient.New(config.NexusBaseURL, &http.Client{Timeout: 5 * time.Second}, config.InternalAuthSecret)
		virployeesUsecases.SetGovernanceChecker(nexusClient)
		virployeesUsecases.SetApprovalReader(nexusClient)
		virployeesUsecases.SetExecutionResultReporter(nexusClient)
	}
	if config.ExecutionMode == "local" {
		virployeesUsecases.RegisterExecutor("calendar.events.create", virployees.NewLocalCalendarExecutor(virployeesRepo))
	}
	memoriesUsecases := memories.NewUseCases(memories.NewRepository(db.Pool()))
	virployeesUsecases.SetMemoryReader(memoriesUsecases)
	virployeesHandler := virployees.NewHandler(virployeesUsecases)
	memoriesHandler := memories.NewHandler(memoriesUsecases)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(ginmw.NewBodySizeLimit(config.MaxBodyBytes))
	router.Use(ginmw.NewCORS(ginmw.CORSConfig{
		Origins:      config.CORSOrigins,
		AllowHeaders: []string{"X-Actor-ID", "X-Tenant-ID", "X-Axis-Tenant-Role"},
	}))
	ginmw.RegisterHealthEndpoints(router, db.Ping)
	api := router.Group("/v1")
	api.Use(internalAuthMiddleware(config.InternalAuthSecret))
	jobRolesHandler.Routes(api)
	capabilitiesHandler.Routes(api)
	profileTemplatesHandler.Routes(api)
	virployeesHandler.Routes(api)
	memoriesHandler.Routes(api)

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
