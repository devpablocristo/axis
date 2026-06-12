package wire

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/devpablocristo/nexus/internal/actiontypes"
	"github.com/devpablocristo/nexus/internal/approvals"
	"github.com/devpablocristo/nexus/internal/audit"
	"github.com/devpablocristo/nexus/internal/callbacks"
	nexusconfig "github.com/devpablocristo/nexus/internal/config"
	"github.com/devpablocristo/nexus/internal/contracts"
	"github.com/devpablocristo/nexus/internal/dashboard"
	"github.com/devpablocristo/nexus/internal/delegations"
	"github.com/devpablocristo/nexus/internal/evidence"
	"github.com/devpablocristo/nexus/internal/findings"
	"github.com/devpablocristo/nexus/internal/learning"
	"github.com/devpablocristo/nexus/internal/ops"
	"github.com/devpablocristo/nexus/internal/policies"
	"github.com/devpablocristo/nexus/internal/rbac"
	"github.com/devpablocristo/nexus/internal/requests"
	"github.com/devpablocristo/platform/authn/go/internaljwt"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/http/go/health"
	sharedobservability "github.com/devpablocristo/platform/observability/go"
)

type Config struct {
	DatabaseURL          string
	APIKeys              string
	AuthIssuerURL        string
	AuthAudience         string
	InternalJWTSecret    string
	InternalJWTIssuer    string
	InternalJWTAudience  string
	ProductJWTKeys       string
	ApprovalTTL          time.Duration
	SigningKey           string
	CallbackToken        string
	PendingCallbackURLs  []string
	ResolvedCallbackURLs []string
	MigrationFiles       fs.FS
	TracingExporter      string
	TracingEndpoint      string
	TracingInsecure      bool
	TracingSampleRatio   float64
	Environment          string
}

func NewServer(cfg Config) (http.Handler, func(), error) {
	ctx := context.Background()
	shutdownTracing, err := sharedobservability.NewTracerProvider(ctx, sharedobservability.TracingConfig{
		ServiceName:    "nexus",
		Environment:    cfg.Environment,
		Exporter:       cfg.TracingExporter,
		OTLPEndpoint:   cfg.TracingEndpoint,
		OTLPInsecure:   cfg.TracingInsecure,
		SampleRatio:    cfg.TracingSampleRatio,
		ServiceVersion: "0.0.0",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure tracing: %w", err)
	}

	// Base de datos
	db, err := sharedpostgres.OpenWithConfig(ctx, cfg.DatabaseURL, sharedpostgres.DefaultConfig("nexus"))
	if err != nil {
		_ = shutdownTracing(ctx)
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	// Migraciones del servicio nexus.
	if err := sharedpostgres.MigrateUp(ctx, db, "nexus", cfg.MigrationFiles, "."); err != nil {
		db.Close()
		_ = shutdownTracing(ctx)
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	// Repositorios (todos postgres)
	policyRepo := policies.NewPostgresRepository(db)
	approvalRepo := approvals.NewPostgresRepository(db)
	auditRepo := audit.NewPostgresRepository(db, audit.WithSigner(cfg.SigningKey, "default"))
	reqRepo := requests.NewPostgresRepository(db)
	idemStore := requests.NewPostgresIdempotencyStore(db)
	resultReportStore := requests.NewPostgresResultReportStore(db)
	learningRepo := learning.NewPostgresRepository(db)
	configRepo := nexusconfig.NewPostgresRepository(db.Pool())
	contractRepo := contracts.NewPostgresRepository(db)
	actionTypeRepo := actiontypes.NewPostgresRepository(db)
	findingRepo := findings.NewPostgresRepository(db)
	opsRepo := ops.NewRepository(db)
	rateLimiter := ops.NewRateLimiter(db)

	// Adapters
	auditSink := requests.NewAuditSinkAdapter(auditRepo)
	evaluator := requests.NewPolicyEvaluator()
	riskConfig := requests.DefaultRiskConfig()
	callbackPublisher := callbacks.NewOutboxApprovalPublisher(db, cfg.CallbackToken, cfg.PendingCallbackURLs, cfg.ResolvedCallbackURLs)

	ttl := cfg.ApprovalTTL
	if ttl <= 0 {
		ttl = time.Hour
	}

	// Usecases
	configUC := nexusconfig.NewUsecases(configRepo)
	contractUC := contracts.NewUsecases(contractRepo)
	policyUC := policies.NewUsecases(policyRepo)
	policyLister := newPolicyListerAdapter(policyUC)
	execStats := requests.NewPostgresExecutionStatsStore(db.Pool())

	// Break-glass: default rules (configurable via /v1/config)
	breakGlassCfg := requests.BreakGlassConfig{
		DefaultApprovals: 2,
		Rules: []requests.BreakGlassRule{
			{ActionTypes: []string{"delete"}, RiskLevel: "critical", RequiredApprovals: 2},
			{ActionTypes: []string{"runbook.execute"}, RiskLevel: "high", RequiredApprovals: 2},
		},
	}

	actionTypeUC := actiontypes.NewUsecases(actionTypeRepo)
	delegationRepo := delegations.NewPostgresRepository(db)
	delegationUC := delegations.NewUsecases(delegationRepo)
	rbacRepo := rbac.NewPostgresRepository(db)
	rbacUC := rbac.NewUsecases(rbacRepo)
	findingUC := findings.NewUsecases(findingRepo, findings.NewEvaluator())
	opsHandler := ops.NewHandler(opsRepo)

	attestationStore := requests.NewPostgresAttestationStore(db.Pool())

	// B.3: Attestation verifier real. En producción no se permite "none": una
	// attestation sin verificación criptográfica es sólo un claim, no evidencia.
	attestVerifierMode := strings.TrimSpace(os.Getenv("NEXUS_ATTESTATION_VERIFIER"))
	if attestVerifierMode == "" {
		attestVerifierMode = "none"
	}
	var attestVerifier requests.AttestationVerifier
	switch attestVerifierMode {
	case "none":
		if nexusProdEnv() {
			db.Close()
			_ = shutdownTracing(ctx)
			return nil, nil, fmt.Errorf("NEXUS_ATTESTATION_VERIFIER=none is not allowed in production")
		}
	case "hmac", "hmac-sha256":
		verifier, err := requests.NewHMACAttestationVerifier(os.Getenv("NEXUS_ATTESTATION_HMAC_SECRET"))
		if err != nil {
			db.Close()
			_ = shutdownTracing(ctx)
			return nil, nil, err
		}
		attestVerifier = verifier
	default:
		db.Close()
		_ = shutdownTracing(ctx)
		return nil, nil, fmt.Errorf("unsupported NEXUS_ATTESTATION_VERIFIER=%q", attestVerifierMode)
	}

	reqOptions := []requests.Option{
		requests.WithIdempotencyStore(idemStore),
		requests.WithAuditSink(auditSink),
		requests.WithRiskConfig(riskConfig),
		requests.WithApprovalTTL(ttl),
		requests.WithShadowHitRecorder(policyRepo),
		requests.WithExecutionStats(execStats),
		requests.WithBreakGlassConfig(breakGlassCfg),
		requests.WithActionTypeChecker(newActionTypeCheckerAdapter(actionTypeUC)),
		requests.WithDelegationChecker(newDelegationCheckerAdapter(delegationUC)),
		requests.WithAttestationStore(attestationStore),
		requests.WithApprovalGetter(approvalRepo),
		requests.WithApprovalCallbacks(callbackPublisher),
		requests.WithResultReportStore(resultReportStore),
		requests.WithContractValidator(contractUC),
	}
	if attestVerifier != nil {
		reqOptions = append(reqOptions, requests.WithAttestationVerifier(attestVerifier))
	}
	reqUC := requests.NewUsecases(reqRepo, policyLister, approvalRepo, evaluator, reqOptions...)
	approvalUC := approvals.NewUsecases(approvalRepo, reqRepo).
		WithAuditSink(auditSink).
		WithApprovalCallbacks(callbackPublisher).
		WithDecisionTx(approvals.NewDecisionApplier(db))
	replayGetter := newReplayRequestGetter(reqRepo)
	auditUC := audit.NewUsecases(auditRepo, replayGetter)

	// Learning con analyzer + proposer determinístico.
	// Nexus es AI-independent: sólo arma propuestas a partir de templates.
	// La generación AI-assisted vive en Companion y POSTea a /v1/learning/proposals.
	learningPolicyCreator := newLearningPolicyCreator(policyRepo)
	analyzer := learning.NewInMemoryPatternAnalyzer(reqRepo)
	learningUC := learning.NewUsecases(learningRepo, learningPolicyCreator).
		WithAnalyzer(analyzer).
		WithProposer(learning.NewStubProposer())

	// Handlers
	reqHandler := requests.NewHandler(reqUC)
	policyHandler := policies.NewHandler(policyUC)
	auditHandler := audit.NewHandler(auditUC)
	approvalHandler := approvals.NewHandler(approvalUC)
	learningHandler := learning.NewHandler(learningUC)
	dashboardHandler := dashboard.NewHandler(reqRepo)
	configHandler := nexusconfig.NewHandler(configUC)
	contractHandler := contracts.NewHandler(contractUC)
	actionTypeHandler := actiontypes.NewHandler(actionTypeUC)
	delegationHandler := delegations.NewHandler(delegationUC)
	rbacHandler := rbac.NewHandler(rbacUC)
	findingHandler := findings.NewHandler(findingUC)

	// Evidence packs.
	// Sin default fallback: si la clave no está, falla startup. Un default
	// hardcodeado terminaría firmando evidence packs en prod con una clave
	// pública.
	if cfg.SigningKey == "" {
		db.Close()
		_ = shutdownTracing(ctx)
		return nil, nil, fmt.Errorf("NEXUS_SIGNING_KEY is required")
	}
	signer, err := evidence.NewSigner(cfg.SigningKey, "default")
	if err != nil {
		db.Close()
		_ = shutdownTracing(ctx)
		return nil, nil, fmt.Errorf("create evidence signer: %w", err)
	}
	evidenceUC := evidence.NewUsecases(reqRepo, approvalRepo, auditRepo, signer).
		WithAttestationReader(attestationStore)
	evidenceHandler := evidence.NewHandler(evidenceUC)

	// Router
	mux := http.NewServeMux()
	health.RegisterEndpoints(mux, func(ctx context.Context) error {
		return db.Ping(ctx)
	})
	reqHandler.Register(mux)
	policyHandler.Register(mux)
	auditHandler.Register(mux)
	approvalHandler.Register(mux)
	learningHandler.Register(mux)
	dashboardHandler.Register(mux)
	configHandler.Register(mux)
	contractHandler.Register(mux)
	actionTypeHandler.Register(mux)
	delegationHandler.Register(mux)
	rbacHandler.Register(mux)
	evidenceHandler.Register(mux)
	findingHandler.Register(mux)
	opsHandler.Register(mux)

	authMW, err := newAuthMiddleware(cfg.APIKeys, cfg.AuthIssuerURL, cfg.AuthAudience, internaljwt.Config{
		Secret:   cfg.InternalJWTSecret,
		Issuer:   cfg.InternalJWTIssuer,
		Audience: cfg.InternalJWTAudience,
	}, cfg.ProductJWTKeys)
	if err != nil {
		db.Close()
		_ = shutdownTracing(ctx)
		return nil, nil, fmt.Errorf("create authenticator: %w", err)
	}
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	callbackPublisher.StartWorker(workerCtx, 2*time.Second, 25)

	cleanup := func() {
		stopWorkers()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = shutdownTracing(shutdownCtx)
		db.Close()
	}

	return authMW(traceMiddleware(rateLimiter.Middleware(mux))), cleanup, nil
}

func nexusProdEnv() bool {
	for _, key := range []string{"NEXUS_ENV", "APP_ENV", "ENVIRONMENT"} {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
		case "prod", "production":
			return true
		}
	}
	return false
}

func traceMiddleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	tracer := sharedobservability.Tracer("nexus/http")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), strings.TrimSpace(r.Method+" "+r.URL.Path))
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
