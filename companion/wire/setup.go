package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/assist"
	"github.com/devpablocristo/companion/internal/connectors"
	"github.com/devpablocristo/companion/internal/connectors/registry"
	"github.com/devpablocristo/companion/internal/memory"
	nexusassist "github.com/devpablocristo/companion/internal/nexus_assist"
	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/companion/internal/tasks"
	"github.com/devpablocristo/companion/internal/watchers"
	"github.com/devpablocristo/companion/internal/watchers/pymesclient"
	"github.com/devpablocristo/platform/authn/go/internaljwt"
	"github.com/devpablocristo/platform/config/go/envconfig"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/http/go/health"

	memdomain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

type taskMemoryAdapter struct {
	uc   *memory.Usecases
	repo tasks.Repository
}

// taskOrgGetter resuelve el org_id de una task para que el handler de
// memoria pueda autorizar memorias scope=task contra el principal.
type taskOrgGetter struct {
	repo tasks.Repository
}

func (g taskOrgGetter) GetTaskOrg(ctx context.Context, taskID uuid.UUID) (string, error) {
	t, err := g.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return "", err
	}
	return t.OrgID, nil
}

func (a taskMemoryAdapter) UpsertTaskMemory(ctx context.Context, taskID uuid.UUID, kind, key string, contentText string, payload json.RawMessage) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	task, err := a.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	_, err = a.uc.Upsert(ctx, memory.UpsertInput{
		OrgID:           task.OrgID,
		UserID:          task.CreatedBy,
		ProductSurface:  "companion",
		Kind:            memdomain.MemoryKind(kind),
		MemoryType:      memdomain.TypeForKind(memdomain.MemoryKind(kind)),
		ScopeType:       memdomain.ScopeTask,
		ScopeID:         taskID.String(),
		Key:             key,
		PayloadJSON:     payload,
		ContentText:     contentText,
		ProvenanceJSON:  json.RawMessage(`{"source":"task_projection"}`),
		Confidence:      1,
		RetentionPolicy: "task",
	})
	return err
}

func nexusSyncInterval() time.Duration {
	return envconfig.Duration("COMPANION_NEXUS_SYNC_INTERVAL_SEC", 30*time.Second)
}

func watcherInterval() time.Duration {
	return envconfig.Duration("COMPANION_WATCHER_INTERVAL_SEC", 0)
}

func watcherSyncInterval() time.Duration {
	return envconfig.Duration("COMPANION_WATCHER_SYNC_INTERVAL_SEC", watcherInterval())
}

// defaultAutonomyLevel lee el nivel de autonomía base del runtime desde env
// (COMPANION_DEFAULT_AUTONOMY_LEVEL). Acepta A0..A5; cualquier otro valor causa
// fail-fast en boot para evitar arrancar con configuración ambigua. Default A2.
func defaultAutonomyLevel() (runtime.AutonomyLevel, error) {
	raw := envconfig.Get("COMPANION_DEFAULT_AUTONOMY_LEVEL", "A2")
	switch runtime.AutonomyLevel(raw) {
	case runtime.AutonomyA0, runtime.AutonomyA1, runtime.AutonomyA2,
		runtime.AutonomyA3, runtime.AutonomyA4, runtime.AutonomyA5:
		return runtime.AutonomyLevel(raw), nil
	default:
		return "", fmt.Errorf("invalid COMPANION_DEFAULT_AUTONOMY_LEVEL=%q (expected A0..A5)", raw)
	}
}

// Config arranque del servicio Companion.
type Config struct {
	DatabaseURL         string
	APIKeys             string
	AuthIssuerURL       string
	AuthAudience        string
	InternalJWTSecret   string
	InternalJWTIssuer   string
	InternalJWTAudience string
	NexusBaseURL        string
	NexusAPIKey         string
	PymesBaseURL        string
	PymesAPIKey         string
	PontiBaseURL        string
	PontiAPIKey         string
	LLMProvider         string
	LLMModel            string
	LLMVertexProject    string
	LLMVertexLocation   string
	MigrationFiles      fs.FS
}

// NewServer abre DB, migra, monta mux y auth.
func NewServer(cfg Config) (http.Handler, func(), error) {
	ctx := context.Background()

	db, err := sharedpostgres.OpenWithConfig(ctx, cfg.DatabaseURL, sharedpostgres.DefaultConfig("companion"))
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	if err := sharedpostgres.MigrateUp(ctx, db, "companion", cfg.MigrationFiles, "."); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	nexusGateway := newNexusGateway(cfg.NexusBaseURL, cfg.NexusAPIKey)
	rc := nexusGateway.client
	pymesClient := pymesclient.NewClient(cfg.PymesBaseURL, cfg.PymesAPIKey)

	// Connectors module
	connReg := registry.NewRegistry()
	connReg.Register(registry.NewMockConnector())
	if cfg.PymesBaseURL != "" {
		connReg.Register(registry.NewPymesConnector(pymesClient))
	}
	if cfg.PontiBaseURL != "" {
		pontiClient := registry.NewPontiClient(cfg.PontiBaseURL, cfg.PontiAPIKey)
		connReg.Register(registry.NewPontiConnector(pontiClient))
	}
	connRepo := connectors.NewPostgresRepository(db)
	nexusChecker := connectors.NewNexusCheckerAdapter(func(c context.Context, id uuid.UUID) (connectors.NexusRequestMeta, int, error) {
		return nexusGateway.GetRequestMeta(c, id.String())
	})
	connUC := connectors.NewUsecases(connRepo, connReg, nexusChecker)
	connHandler := connectors.NewHandler(connUC)

	repo := tasks.NewPostgresRepository(db)
	uc := tasks.NewUsecases(repo, nexusGateway)
	uc.SetNexusSyncInterval(nexusSyncInterval())
	uc.SetExecutor(connUC)
	h := tasks.NewHandler(uc)

	// Watchers module
	watcherRepo := watchers.NewPostgresRepository(db)
	watcherUC := watchers.NewUsecases(watcherRepo, nexusGateway)
	watcherUC.SetConnectorExecutor(connUC)
	watcherHandler := watchers.NewHandler(watcherUC)

	// Memory module
	memRepo := memory.NewPostgresRepository(db)
	memUC := memory.NewUsecases(memRepo)
	memHandler := memory.NewHandler(memUC, taskOrgGetter{repo: repo})
	uc.SetTaskMemory(taskMemoryAdapter{uc: memUC, repo: repo})

	// Agent memory (conversation history durable per user). El repo de memory
	// implementa los métodos agent_* — pasamos el mismo *PostgresRepository.
	agentMemUC := memory.NewAgentMemoryUC(memRepo)
	uc.SetAgentMemory(agentMemUC)
	chatHandler := memory.NewChatHandler(agentMemUC)

	// Runtime del compañero (LLM + tools + context)
	llmProvider, err := runtime.NewProvider(runtime.ProviderConfig{
		Provider:       cfg.LLMProvider,
		Model:          cfg.LLMModel,
		VertexProject:  cfg.LLMVertexProject,
		VertexLocation: cfg.LLMVertexLocation,
	})
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("configure Gemini provider: %w", err)
	}
	toolkit := runtime.NewToolKit(rc, memUC, watcherUC)

	// Bridge LLM ↔ connectors: expone cada capability declarada por los
	// connector types registrados como runtime tool (LLM-callable). Reads van
	// directo al executor; writes controlled pasan por Nexus antes de ejecutar.
	connectorViews := make([]runtime.ConnectorTypeView, 0)
	for _, c := range connReg.List() {
		connectorViews = append(connectorViews, c)
	}
	runtime.RegisterConnectorCapabilities(toolkit, runtime.CapabilityBridgeDeps{
		Connectors: connectorViews,
		Executor:   connUC,
		Submitter:  nexusGateway,
	})
	contextPorts := runtime.ContextPorts{
		NexusClient: rc,
		MemoryFind: func(c context.Context, orgID, userID, productSurface string, st memdomain.ScopeType, sid string, k memdomain.MemoryKind, limit int) ([]memdomain.MemoryEntry, error) {
			return memUC.Find(c, memory.FindQuery{OrgID: orgID, UserID: userID, ProductSurface: productSurface, ScopeType: st, ScopeID: sid, Kind: k, Limit: limit})
		},
	}
	orchestrator := runtime.NewOrchestrator(llmProvider, toolkit, contextPorts)
	orchestrator.SetModel(cfg.LLMModel)
	autonomy, err := defaultAutonomyLevel()
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	orchestrator.SetDefaultAutonomy(autonomy)
	traceRepo := runtime.NewPostgresTraceRepository(db)
	orchestrator.SetTraceRepository(traceRepo)
	runtimeGovernanceRepo := runtime.NewPostgresRuntimeGovernanceRepository(db)
	orchestrator.SetRuntimeGovernance(runtimeGovernanceRepo)
	traceHandler := runtime.NewTraceHandler(traceRepo)
	runtimeGovernanceHandler := runtime.NewRuntimeGovernanceHandler(runtimeGovernanceRepo)
	adapter := runtime.NewOrchestratorAdapter(orchestrator)
	uc.SetOrchestrator(adapter)
	// Watchers empujan alertas al chat del suscriptor
	watcherUC.SetNotifier(uc)
	slog.Info("companion runtime initialized", "llm_provider", cfg.LLMProvider)

	// Nexus-assist: lee Nexus + arma proposals/summaries con Gemini.
	// Le pasamos el mismo provider del runtime para no duplicar config.
	nexusAssistProposer := nexusassist.NewProposer(rc, llmProvider)
	nexusAssistContextualizer := nexusassist.NewContextualizer(rc, llmProvider)
	nexusAssistHandler := nexusassist.NewHandler(nexusAssistProposer, nexusAssistContextualizer)

	assistRepo := assist.NewPostgresRepository(db)
	assistUC := assist.NewUsecases(assistRepo, llmProvider)
	assistHandler := assist.NewHandler(assistUC)

	mux := http.NewServeMux()
	health.RegisterEndpoints(mux, func(c context.Context) error {
		return db.Ping(c)
	})
	h.Register(mux)
	watcherHandler.Register(mux)
	memHandler.Register(mux)
	chatHandler.Register(mux)
	connHandler.Register(mux)
	traceHandler.Register(mux)
	runtimeGovernanceHandler.Register(mux)
	nexusAssistHandler.Register(mux)
	assistHandler.Register(mux)

	// Seed conectores por defecto
	if err := connUC.SeedDefaultConnectors(ctx); err != nil {
		slog.Error("seed default connectors", "error", err)
	}

	authMW, err := newAuthMiddleware(cfg.APIKeys, cfg.AuthIssuerURL, cfg.AuthAudience, internaljwt.Config{
		Secret:   cfg.InternalJWTSecret,
		Issuer:   cfg.InternalJWTIssuer,
		Audience: cfg.InternalJWTAudience,
	})
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("create authenticator: %w", err)
	}

	cleanup := func() {
		db.Close()
	}
	if d := nexusSyncInterval(); d > 0 {
		syncCtx, syncCancel := context.WithCancel(context.Background())
		go uc.RunNexusSyncLoop(syncCtx, d, 50)
		prev := cleanup
		cleanup = func() {
			syncCancel()
			prev()
		}
	}
	if d := watcherInterval(); d > 0 {
		watcherCtx, watcherCancel := context.WithCancel(context.Background())
		go watcherUC.RunWatcherLoop(watcherCtx, d, 50)
		prev := cleanup
		cleanup = func() {
			watcherCancel()
			prev()
		}
	}
	if d := watcherSyncInterval(); d > 0 {
		watcherSyncCtx, watcherSyncCancel := context.WithCancel(context.Background())
		go watcherUC.RunPendingProposalSyncLoop(watcherSyncCtx, d, 50)
		prev := cleanup
		cleanup = func() {
			watcherSyncCancel()
			prev()
		}
	}

	// Memory purge loop: limpia entradas expiradas cada hora
	{
		purgeCtx, purgeCancel := context.WithCancel(context.Background())
		go memUC.RunPurgeLoop(purgeCtx, 1*time.Hour)
		prev := cleanup
		cleanup = func() {
			purgeCancel()
			prev()
		}
	}

	return authMW(mux), cleanup, nil
}
