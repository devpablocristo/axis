package config

import (
	"strings"

	"github.com/devpablocristo/platform/config/go/envconfig"
)

type Config struct {
	Environment   string
	Port          string
	DatabaseURL   string
	RunMigrations bool
	MaxBodyBytes  int64
	CORSOrigins   []string
	NexusBaseURL  string
	ExecutionMode string
}

func Load() Config {
	return Config{
		Environment:   envconfig.NormalizeEnv(envconfig.Get("COMPANION_V2_ENV", "development")),
		Port:          envconfig.Get("PORT", "19086"),
		DatabaseURL:   envconfig.Get("COMPANION_V2_DATABASE_URL", envconfig.Get("DATABASE_URL", "")),
		RunMigrations: envconfig.Bool("COMPANION_V2_RUN_MIGRATIONS", true),
		MaxBodyBytes:  int64(envconfig.Int("COMPANION_V2_MAX_BODY_BYTES", 1<<20)),
		CORSOrigins:   splitCSV(envconfig.Get("COMPANION_V2_CORS_ORIGINS", "")),
		NexusBaseURL:  strings.TrimRight(envconfig.Get("COMPANION_V2_NEXUS_BASE_URL", ""), "/"),
		ExecutionMode: strings.ToLower(strings.TrimSpace(envconfig.Get("COMPANION_V2_EXECUTION_MODE", "disabled"))),
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
