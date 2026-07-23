package invocation

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const SchemaVersion = "axis.invocation-context.v1"

const (
	AccessModeDirect          = "direct"
	AccessModeViaOrchestrator = "via_orchestrator"
	AccessModeViaCompanion    = "via_companion" // legacy wire value
)

// Context is the product-neutral authority and integration envelope attached to
// every durable invocation. It contains identifiers and hashes only; prompts,
// user content, credentials and product-specific DTOs do not belong here.
type Context struct {
	SchemaVersion       string   `json:"schema_version"`
	OrgID               string   `json:"org_id"`
	ProductID           string   `json:"product_id,omitempty"`
	ProductSurface      string   `json:"product_surface,omitempty"`
	IntegrationID       string   `json:"integration_id,omitempty"`
	IntegrationRevision int64    `json:"integration_revision,omitempty"`
	IntegrationHash     string   `json:"integration_hash,omitempty"`
	PrincipalType       string   `json:"principal_type,omitempty"`
	PrincipalID         string   `json:"principal_id,omitempty"`
	Scopes              []string `json:"scopes,omitempty"`
	AccessMode          string   `json:"access_mode"`
}

// Normalize validates and canonicalizes a trusted context before it is used in
// an idempotency scope or persisted. Legacy direct calls may omit product and
// integration identifiers, but a partially supplied integration binding is
// rejected.
func Normalize(in Context) (Context, error) {
	out := Context{
		SchemaVersion:       SchemaVersion,
		OrgID:               strings.TrimSpace(in.OrgID),
		ProductID:           strings.TrimSpace(in.ProductID),
		ProductSurface:      strings.ToLower(strings.TrimSpace(in.ProductSurface)),
		IntegrationID:       strings.TrimSpace(in.IntegrationID),
		IntegrationRevision: in.IntegrationRevision,
		IntegrationHash:     strings.ToLower(strings.TrimSpace(in.IntegrationHash)),
		PrincipalType:       strings.ToLower(strings.TrimSpace(in.PrincipalType)),
		PrincipalID:         strings.TrimSpace(in.PrincipalID),
		Scopes:              normalizeSet(in.Scopes),
		AccessMode:          strings.ToLower(strings.TrimSpace(in.AccessMode)),
	}
	if out.OrgID == "" {
		return Context{}, fmt.Errorf("invocation organization is required")
	}
	if out.AccessMode == "" {
		out.AccessMode = AccessModeDirect
	}
	switch out.AccessMode {
	case AccessModeDirect, AccessModeViaOrchestrator, AccessModeViaCompanion:
	default:
		return Context{}, fmt.Errorf("invocation access mode is invalid")
	}
	hasIntegration := out.IntegrationID != "" || out.IntegrationRevision != 0 || out.IntegrationHash != ""
	if hasIntegration {
		if out.ProductID == "" || out.IntegrationID == "" || out.IntegrationRevision < 1 || !validSHA256(out.IntegrationHash) {
			return Context{}, fmt.Errorf("invocation integration binding is incomplete")
		}
	}
	if (out.PrincipalType == "") != (out.PrincipalID == "") {
		return Context{}, fmt.Errorf("invocation principal type and id must be provided together")
	}
	return out, nil
}

func normalizeSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
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

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
