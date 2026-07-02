package config

import (
	"strings"

	"github.com/devpablocristo/platform/config/go/envconfig"
)

type Config struct {
	Environment      string
	Port             string
	DatabaseURL      string
	RunMigrations    bool
	MaxBodyBytes     int64
	CORSOrigins      []string
	CompanionBaseURL string

	DevPrincipalID    string
	DevPrincipalEmail string
	DevPrincipalName  string
	DevOrgID          string
}

func Load() Config {
	return Config{
		Environment:       envconfig.NormalizeEnv(envconfig.Get("BFF_V2_ENV", "development")),
		Port:              envconfig.Get("PORT", "18080"),
		DatabaseURL:       envconfig.Get("BFF_V2_DATABASE_URL", envconfig.Get("DATABASE_URL", "")),
		RunMigrations:     envconfig.Bool("BFF_V2_RUN_MIGRATIONS", true),
		MaxBodyBytes:      int64(envconfig.Int("BFF_V2_MAX_BODY_BYTES", 1<<20)),
		CORSOrigins:       splitCSV(envconfig.Get("BFF_V2_CORS_ORIGINS", "")),
		CompanionBaseURL:  strings.TrimRight(envconfig.Get("BFF_V2_COMPANION_BASE_URL", "http://127.0.0.1:18086"), "/"),
		DevPrincipalID:    envconfig.Get("BFF_V2_DEV_PRINCIPAL_ID", envconfig.Get("BFF_V2_DEV_ACTOR_ID", "dev-user")),
		DevPrincipalEmail: envconfig.Get("BFF_V2_DEV_PRINCIPAL_EMAIL", envconfig.Get("BFF_V2_DEV_ACTOR_EMAIL", "dev@example.local")),
		DevPrincipalName:  envconfig.Get("BFF_V2_DEV_PRINCIPAL_NAME", envconfig.Get("BFF_V2_DEV_ACTOR_NAME", "Dev User")),
		DevOrgID:          envconfig.Get("BFF_V2_DEV_ORG_ID", "dev-org"),
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
