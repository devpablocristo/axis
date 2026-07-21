package config

import (
	"strings"

	"github.com/devpablocristo/platform/config/go/envconfig"
)

type Config struct {
	Environment        string
	Port               string
	DatabaseURL        string
	RunMigrations      bool
	MaxBodyBytes       int64
	CORSOrigins        []string
	NexusBaseURL       string
	RuntimeBaseURL     string
	ExecutionMode      string
	InternalAuthSecret string
	// GoogleCalendarID is the target calendar for the real executor
	// (ExecutionMode=google_calendar). Defaults to "primary".
	GoogleCalendarID string

	// LearningMinExecutions is the default number of successful executions of a
	// capability a virployee needs before the analyzer proposes a procedure.
	LearningMinExecutions int
	// LearningEnricherEnabled turns on the optional LLM rewrite of a distilled
	// procedure (PR5). Off by default; requires RuntimeBaseURL to be set too.
	LearningEnricherEnabled bool

	ServiceVersion string
	OTelExporter   string
	OTelEndpoint   string
	OTelInsecure   bool
}

func Load() Config {
	return Config{
		Environment:             envconfig.NormalizeEnv(envconfig.Get("COMPANION_V2_ENV", "development")),
		Port:                    envconfig.Get("PORT", "19086"),
		DatabaseURL:             envconfig.Get("COMPANION_V2_DATABASE_URL", envconfig.Get("DATABASE_URL", "")),
		RunMigrations:           envconfig.Bool("COMPANION_V2_RUN_MIGRATIONS", true),
		MaxBodyBytes:            int64(envconfig.Int("COMPANION_V2_MAX_BODY_BYTES", 1<<20)),
		CORSOrigins:             splitCSV(envconfig.Get("COMPANION_V2_CORS_ORIGINS", "")),
		NexusBaseURL:            strings.TrimRight(envconfig.Get("COMPANION_V2_NEXUS_BASE_URL", ""), "/"),
		RuntimeBaseURL:          strings.TrimRight(envconfig.Get("COMPANION_V2_RUNTIME_BASE_URL", ""), "/"),
		ExecutionMode:           strings.ToLower(strings.TrimSpace(envconfig.Get("COMPANION_V2_EXECUTION_MODE", "disabled"))),
		InternalAuthSecret:      strings.TrimSpace(envconfig.Get("COMPANION_V2_INTERNAL_AUTH_SECRET", envconfig.Get("AXIS_V2_INTERNAL_AUTH_SECRET", ""))),
		GoogleCalendarID:        strings.TrimSpace(envconfig.Get("COMPANION_V2_GOOGLE_CALENDAR_ID", "primary")),
		LearningMinExecutions:   envconfig.Int("COMPANION_V2_LEARNING_MIN_EXECUTIONS", 3),
		LearningEnricherEnabled: envconfig.Bool("COMPANION_V2_LEARNING_ENRICHER_ENABLED", false),
		ServiceVersion:          envconfig.Get("COMPANION_V2_SERVICE_VERSION", ""),
		OTelExporter:            strings.ToLower(strings.TrimSpace(envconfig.Get("COMPANION_V2_OTEL_EXPORTER", "none"))),
		OTelEndpoint:            strings.TrimSpace(envconfig.Get("COMPANION_V2_OTEL_OTLP_ENDPOINT", "")),
		OTelInsecure:            envconfig.Bool("COMPANION_V2_OTEL_OTLP_INSECURE", true),
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
