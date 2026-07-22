package config

import (
	"strings"
	"time"

	"github.com/devpablocristo/platform/config/go/envconfig"
)

type Config struct {
	Environment          string
	Port                 string
	DatabaseURL          string
	RunMigrations        bool
	MaxBodyBytes         int64
	CORSOrigins          []string
	InternalAuthSecret   string
	SigningKey           string
	ApprovalTTL          time.Duration
	WatcherInterval      time.Duration
	WatcherBatchSize     int
	WatcherMaxAttempts   int
	JobWorkerConcurrency int
	JobPollInterval      time.Duration
	JobLease             time.Duration
	JobTimeout           time.Duration

	ServiceVersion string
	OTelExporter   string
	OTelEndpoint   string
	OTelInsecure   bool
}

func Load() Config {
	return Config{
		Environment:          envconfig.NormalizeEnv(envconfig.Get("NEXUS_V2_ENV", "development")),
		Port:                 envconfig.Get("PORT", "19087"),
		DatabaseURL:          envconfig.Get("NEXUS_V2_DATABASE_URL", envconfig.Get("DATABASE_URL", "")),
		RunMigrations:        envconfig.Bool("NEXUS_V2_RUN_MIGRATIONS", true),
		MaxBodyBytes:         int64(envconfig.Int("NEXUS_V2_MAX_BODY_BYTES", 1<<20)),
		CORSOrigins:          splitCSV(envconfig.Get("NEXUS_V2_CORS_ORIGINS", "")),
		InternalAuthSecret:   strings.TrimSpace(envconfig.Get("NEXUS_V2_INTERNAL_AUTH_SECRET", envconfig.Get("AXIS_V2_INTERNAL_AUTH_SECRET", ""))),
		SigningKey:           strings.TrimSpace(envconfig.Get("NEXUS_V2_SIGNING_KEY", "")),
		ApprovalTTL:          time.Duration(envconfig.Int("NEXUS_V2_APPROVAL_TTL_SEC", 3600)) * time.Second,
		WatcherInterval:      time.Duration(envconfig.Int("NEXUS_V2_WATCHER_INTERVAL_SEC", 30)) * time.Second,
		WatcherBatchSize:     envconfig.Int("NEXUS_V2_WATCHER_BATCH_SIZE", 100),
		WatcherMaxAttempts:   envconfig.Int("NEXUS_V2_WATCHER_MAX_ATTEMPTS", 3),
		JobWorkerConcurrency: envconfig.Int("NEXUS_V2_JOB_WORKER_CONCURRENCY", 2),
		JobPollInterval:      time.Duration(envconfig.Int("NEXUS_V2_JOB_POLL_INTERVAL_SEC", 1)) * time.Second,
		JobLease:             time.Duration(envconfig.Int("NEXUS_V2_JOB_LEASE_SEC", 30)) * time.Second,
		JobTimeout:           time.Duration(envconfig.Int("NEXUS_V2_JOB_TIMEOUT_SEC", 300)) * time.Second,
		ServiceVersion:       envconfig.Get("NEXUS_V2_SERVICE_VERSION", ""),
		OTelExporter:         strings.ToLower(strings.TrimSpace(envconfig.Get("NEXUS_V2_OTEL_EXPORTER", "none"))),
		OTelEndpoint:         strings.TrimSpace(envconfig.Get("NEXUS_V2_OTEL_OTLP_ENDPOINT", "")),
		OTelInsecure:         envconfig.Bool("NEXUS_V2_OTEL_OTLP_INSECURE", true),
	}
}

func (c Config) Addr() string {
	return ":" + c.Port
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
