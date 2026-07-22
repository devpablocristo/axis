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
	InternalAuthSecret string
	SigningKey         string

	ServiceVersion string
	OTelExporter   string
	OTelEndpoint   string
	OTelInsecure   bool
}

func Load() Config {
	return Config{
		Environment:        envconfig.NormalizeEnv(envconfig.Get("NEXUS_V2_ENV", "development")),
		Port:               envconfig.Get("PORT", "19087"),
		DatabaseURL:        envconfig.Get("NEXUS_V2_DATABASE_URL", envconfig.Get("DATABASE_URL", "")),
		RunMigrations:      envconfig.Bool("NEXUS_V2_RUN_MIGRATIONS", true),
		MaxBodyBytes:       int64(envconfig.Int("NEXUS_V2_MAX_BODY_BYTES", 1<<20)),
		CORSOrigins:        splitCSV(envconfig.Get("NEXUS_V2_CORS_ORIGINS", "")),
		InternalAuthSecret: strings.TrimSpace(envconfig.Get("NEXUS_V2_INTERNAL_AUTH_SECRET", envconfig.Get("AXIS_V2_INTERNAL_AUTH_SECRET", ""))),
		SigningKey:         strings.TrimSpace(envconfig.Get("NEXUS_V2_SIGNING_KEY", "")),
		ServiceVersion:     envconfig.Get("NEXUS_V2_SERVICE_VERSION", ""),
		OTelExporter:       strings.ToLower(strings.TrimSpace(envconfig.Get("NEXUS_V2_OTEL_EXPORTER", "none"))),
		OTelEndpoint:       strings.TrimSpace(envconfig.Get("NEXUS_V2_OTEL_OTLP_ENDPOINT", "")),
		OTelInsecure:       envconfig.Bool("NEXUS_V2_OTEL_OTLP_INSECURE", true),
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
