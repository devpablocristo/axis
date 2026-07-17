package config

import (
	"strings"

	"github.com/devpablocristo/platform/config/go/envconfig"
)

type Config struct {
	Environment        string
	Port               string
	MaxBodyBytes       int64
	CORSOrigins        []string
	InternalAuthSecret string

	// LLM provider selection. For provider "vertex", Vertex AI (Gemini) is used
	// with Application Default Credentials and requires VertexProject; without a
	// project (or, for other providers, without an API key) it falls back to the
	// Echo provider, so dev and CI start without a secret or external calls.
	LLMProvider    string
	LLMAPIKey      string
	LLMModel       string
	VertexProject  string
	VertexLocation string

	ServiceVersion string
	OTelExporter   string
	OTelEndpoint   string
	OTelInsecure   bool
}

func Load() Config {
	return Config{
		Environment:        envconfig.NormalizeEnv(envconfig.Get("RUNTIME_V2_ENV", "development")),
		Port:               envconfig.Get("PORT", "19088"),
		MaxBodyBytes:       int64(envconfig.Int("RUNTIME_V2_MAX_BODY_BYTES", 1<<20)),
		CORSOrigins:        splitCSV(envconfig.Get("RUNTIME_V2_CORS_ORIGINS", "")),
		InternalAuthSecret: strings.TrimSpace(envconfig.Get("RUNTIME_V2_INTERNAL_AUTH_SECRET", envconfig.Get("AXIS_V2_INTERNAL_AUTH_SECRET", ""))),
		LLMProvider:        strings.ToLower(strings.TrimSpace(envconfig.Get("RUNTIME_V2_LLM_PROVIDER", "vertex"))),
		LLMAPIKey:          strings.TrimSpace(envconfig.Get("RUNTIME_V2_LLM_API_KEY", "")),
		LLMModel:           strings.TrimSpace(envconfig.Get("RUNTIME_V2_LLM_MODEL", "gemini-2.5-flash-lite")),
		VertexProject:      strings.TrimSpace(envconfig.Get("RUNTIME_V2_VERTEX_PROJECT", "")),
		VertexLocation:     strings.TrimSpace(envconfig.Get("RUNTIME_V2_VERTEX_LOCATION", "us-central1")),
		ServiceVersion:     envconfig.Get("RUNTIME_V2_SERVICE_VERSION", ""),
		OTelExporter:       strings.ToLower(strings.TrimSpace(envconfig.Get("RUNTIME_V2_OTEL_EXPORTER", "none"))),
		OTelEndpoint:       strings.TrimSpace(envconfig.Get("RUNTIME_V2_OTEL_OTLP_ENDPOINT", "")),
		OTelInsecure:       envconfig.Bool("RUNTIME_V2_OTEL_OTLP_INSECURE", true),
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
