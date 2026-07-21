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
	// ExecutionMode is the raw COMPANION_V2_EXECUTION_MODE value (kept for logging).
	ExecutionMode string
	// ExecutionModes is the parsed set of enabled executor modes. The variable is a
	// comma-separated list (e.g. "local", "local,google_calendar"); "disabled" and
	// empty entries yield an empty set (no executor wired = simulation only).
	ExecutionModes     []string
	InternalAuthSecret string

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
		ExecutionModes:          parseExecutionModes(envconfig.Get("COMPANION_V2_EXECUTION_MODE", "disabled")),
		InternalAuthSecret:      strings.TrimSpace(envconfig.Get("COMPANION_V2_INTERNAL_AUTH_SECRET", envconfig.Get("AXIS_V2_INTERNAL_AUTH_SECRET", ""))),
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

// HasExecutionMode reports whether the given executor mode is enabled. Comparison
// is case-insensitive; "disabled" is never a member of the set.
func (c Config) HasExecutionMode(mode string) bool {
	want := strings.ToLower(strings.TrimSpace(mode))
	for _, m := range c.ExecutionModes {
		if m == want {
			return true
		}
	}
	return false
}

// parseExecutionModes turns the comma-separated COMPANION_V2_EXECUTION_MODE value
// into a lowercased set of enabled modes. "disabled" and empty entries are dropped,
// so `disabled` (the default) yields an empty set — no executor is wired.
func parseExecutionModes(raw string) []string {
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		mode := strings.ToLower(strings.TrimSpace(part))
		if mode == "" || mode == "disabled" {
			continue
		}
		out = append(out, mode)
	}
	return out
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
