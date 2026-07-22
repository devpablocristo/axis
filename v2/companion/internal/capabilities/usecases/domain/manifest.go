package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type IdempotencyContract struct {
	Mode      string   `json:"mode"`
	KeyFields []string `json:"key_fields"`
}

type RetryContract struct {
	MaxAttempts int `json:"max_attempts"`
	BackoffMS   int `json:"backoff_ms"`
}

type Manifest struct {
	Version             string              `json:"version"`
	ProductSurface      string              `json:"product_surface"`
	InputSchema         map[string]any      `json:"input_schema"`
	OutputSchema        map[string]any      `json:"output_schema"`
	RequiredScopes      []string            `json:"required_scopes"`
	Idempotency         IdempotencyContract `json:"idempotency"`
	RollbackMode        string              `json:"rollback_mode"`
	TimeoutMS           int                 `json:"timeout_ms"`
	Retry               RetryContract       `json:"retry"`
	Postconditions      []string            `json:"postconditions"`
	QuotaAreas          []string            `json:"quota_areas"`
	SecretRefs          []string            `json:"secret_refs"`
	AttestationRequired bool                `json:"attestation_required"`
	CostClass           string              `json:"cost_class"`
}

type ManifestInput struct {
	Version             string
	ProductSurface      string
	InputSchema         json.RawMessage
	OutputSchema        json.RawMessage
	RequiredScopes      []string
	Idempotency         IdempotencyContract
	RollbackMode        string
	TimeoutMS           int
	Retry               RetryContract
	Postconditions      []string
	QuotaAreas          []string
	SecretRefs          []string
	AttestationRequired bool
	CostClass           string
}

type ConformanceCheck struct {
	Key    string `json:"key"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
}

type ConformanceReport struct {
	Conformant   bool               `json:"conformant"`
	ManifestHash string             `json:"manifest_hash"`
	Checks       []ConformanceCheck `json:"checks"`
}

func NormalizeManifest(input ManifestInput) (Manifest, string, error) {
	inputSchema, err := normalizeSchema(input.InputSchema, "input_schema")
	if err != nil {
		return Manifest{}, "", err
	}
	outputSchema, err := normalizeSchema(input.OutputSchema, "output_schema")
	if err != nil {
		return Manifest{}, "", err
	}
	manifest := Manifest{
		Version:             strings.TrimSpace(input.Version),
		ProductSurface:      strings.ToLower(strings.TrimSpace(input.ProductSurface)),
		InputSchema:         inputSchema,
		OutputSchema:        outputSchema,
		RequiredScopes:      normalizedSet(input.RequiredScopes, false),
		Idempotency:         IdempotencyContract{Mode: strings.ToLower(strings.TrimSpace(input.Idempotency.Mode)), KeyFields: normalizedSet(input.Idempotency.KeyFields, false)},
		RollbackMode:        strings.ToLower(strings.TrimSpace(input.RollbackMode)),
		TimeoutMS:           input.TimeoutMS,
		Retry:               input.Retry,
		Postconditions:      normalizedSet(input.Postconditions, false),
		QuotaAreas:          normalizedSet(input.QuotaAreas, true),
		SecretRefs:          normalizedSet(input.SecretRefs, false),
		AttestationRequired: input.AttestationRequired,
		CostClass:           strings.ToLower(strings.TrimSpace(input.CostClass)),
	}
	manifestHash, err := HashManifest(manifest)
	if err != nil {
		return Manifest{}, "", err
	}
	return manifest, manifestHash, nil
}

func HashManifest(manifest Manifest) (string, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeSchema(raw json.RawMessage, field string) (map[string]any, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return map[string]any{}, nil
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil || schema == nil {
		return nil, domainerr.Validation(field + " must be a JSON object")
	}
	return schema, nil
}

func normalizedSet(values []string, lowercase bool) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if lowercase {
			value = strings.ToLower(value)
		}
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
