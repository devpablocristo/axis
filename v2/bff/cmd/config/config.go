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
	CompanionBaseURL   string
	NexusBaseURL       string
	IdentityProvider   string
	InternalAuthSecret string

	ClerkSecretKey         string
	ClerkAPIBaseURL        string
	ClerkIssuerURL         string
	ClerkWebhookSecret     string
	ClerkInviteRedirectURL string

	DevPrincipalID    string
	DevPrincipalEmail string
	DevOrgID          string

	ServiceVersion string
	OTelExporter   string
	OTelEndpoint   string
	OTelInsecure   bool
}

func Load() Config {
	return Config{
		Environment:        envconfig.NormalizeEnv(envconfig.Get("BFF_V2_ENV", "development")),
		Port:               envconfig.Get("PORT", "19080"),
		DatabaseURL:        envconfig.Get("BFF_V2_DATABASE_URL", envconfig.Get("DATABASE_URL", "")),
		RunMigrations:      envconfig.Bool("BFF_V2_RUN_MIGRATIONS", true),
		MaxBodyBytes:       int64(envconfig.Int("BFF_V2_MAX_BODY_BYTES", 1<<20)),
		CORSOrigins:        splitCSV(envconfig.Get("BFF_V2_CORS_ORIGINS", "")),
		CompanionBaseURL:   strings.TrimRight(envconfig.Get("BFF_V2_COMPANION_BASE_URL", "http://127.0.0.1:19086"), "/"),
		NexusBaseURL:       strings.TrimRight(envconfig.Get("BFF_V2_NEXUS_BASE_URL", "http://127.0.0.1:19087"), "/"),
		IdentityProvider:   strings.TrimSpace(strings.ToLower(envconfig.Get("BFF_V2_IDENTITY_PROVIDER", "dev"))),
		InternalAuthSecret: strings.TrimSpace(envconfig.Get("BFF_V2_INTERNAL_AUTH_SECRET", envconfig.Get("AXIS_V2_INTERNAL_AUTH_SECRET", ""))),
		ClerkSecretKey:     envconfig.Get("BFF_V2_CLERK_SECRET_KEY", envconfig.Get("BFF_V2_CLERK_SECRET", envconfig.Get("CLERK_SECRET_KEY", ""))),
		ClerkAPIBaseURL:    strings.TrimRight(envconfig.Get("BFF_V2_CLERK_API_BASE_URL", "https://api.clerk.com/v1"), "/"),
		ClerkIssuerURL:     strings.TrimRight(envconfig.Get("BFF_V2_CLERK_ISSUER_URL", envconfig.Get("CLERK_ISSUER_URL", "")), "/"),
		ClerkWebhookSecret: envconfig.Get(
			"BFF_V2_CLERK_WEBHOOK_SECRET",
			envconfig.Get("CLERK_WEBHOOK_SECRET", ""),
		),
		ClerkInviteRedirectURL: envconfig.Get("BFF_V2_CLERK_INVITE_REDIRECT_URL", ""),
		DevPrincipalID:         envconfig.Get("BFF_V2_DEV_PRINCIPAL_ID", envconfig.Get("BFF_V2_DEV_ACTOR_ID", "dev-user")),
		DevPrincipalEmail:      envconfig.Get("BFF_V2_DEV_PRINCIPAL_EMAIL", envconfig.Get("BFF_V2_DEV_ACTOR_EMAIL", "dev@example.local")),
		DevOrgID:               envconfig.Get("BFF_V2_DEV_ORG_ID", "dev-org"),
		ServiceVersion:         envconfig.Get("BFF_V2_SERVICE_VERSION", ""),
		OTelExporter:           strings.ToLower(strings.TrimSpace(envconfig.Get("BFF_V2_OTEL_EXPORTER", "none"))),
		OTelEndpoint:           strings.TrimSpace(envconfig.Get("BFF_V2_OTEL_OTLP_ENDPOINT", "")),
		OTelInsecure:           envconfig.Bool("BFF_V2_OTEL_OTLP_INSECURE", true),
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
