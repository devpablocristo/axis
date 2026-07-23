package productintegrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	SchemaVersion             = "axis.product-integration.v2"
	AccessModeDirect          = "direct"
	AccessModeViaOrchestrator = "via_orchestrator"
	AccessModeViaCompanion    = "via_companion"
)

var (
	codePattern        = regexp.MustCompile(`^[a-z][a-z0-9._:-]{0,127}$`)
	versionPattern     = regexp.MustCompile(`^v[1-9][0-9]*$`)
	contentHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type APIContract struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ActionTypeRef struct {
	Key string `json:"key"`
}

type WebhookSubscription struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
	SecretRef  string   `json:"secret_ref"`
}

type Section struct {
	SchemaVersion string                `json:"schema_version"`
	APIContracts  []APIContract         `json:"api_contracts"`
	ActionTypes   []ActionTypeRef       `json:"action_types,omitempty"`
	AccessModes   []string              `json:"access_modes"`
	Webhooks      []WebhookSubscription `json:"webhooks,omitempty"`
}

type CreateVersionInput struct {
	SourceIntegrationID uuid.UUID `json:"source_integration_id"`
	SourceVersionID     uuid.UUID `json:"source_version_id"`
	Version             int64     `json:"version"`
	ContractHash        string    `json:"contract_hash"`
	ProductSurface      string    `json:"product_surface,omitempty"`
	Section             Section   `json:"section"`
}

type Integration struct {
	ID              uuid.UUID  `json:"id"`
	OrgID           string     `json:"org_id"`
	ProductID       uuid.UUID  `json:"product_id"`
	ProductSurface  string     `json:"product_surface"`
	Lifecycle       string     `json:"lifecycle"`
	ActiveVersionID *uuid.UUID `json:"active_version_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Version struct {
	ID                  uuid.UUID  `json:"id"`
	IntegrationID       uuid.UUID  `json:"integration_id"`
	Revision            int64      `json:"revision"`
	SourceIntegrationID uuid.UUID  `json:"source_integration_id"`
	SourceVersionID     uuid.UUID  `json:"source_version_id"`
	SourceRevision      int64      `json:"source_revision"`
	ContractHash        string     `json:"contract_hash"`
	SchemaVersion       string     `json:"schema_version"`
	Section             Section    `json:"section"`
	ContentHash         string     `json:"content_hash"`
	Status              string     `json:"status"`
	CreatedBy           string     `json:"created_by"`
	CreatedAt           time.Time  `json:"created_at"`
	ActivatedBy         string     `json:"activated_by,omitempty"`
	ActivatedAt         *time.Time `json:"activated_at,omitempty"`
}

type ValidationCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ValidationReport struct {
	ID          uuid.UUID         `json:"id"`
	ProductID   uuid.UUID         `json:"product_id"`
	VersionID   uuid.UUID         `json:"version_id"`
	ContentHash string            `json:"content_hash"`
	Valid       bool              `json:"valid"`
	Checks      []ValidationCheck `json:"checks"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
}

type Readiness struct {
	Service           string            `json:"service"`
	ProductID         uuid.UUID         `json:"product_id"`
	ProductSurface    string            `json:"product_surface"`
	Lifecycle         string            `json:"lifecycle"`
	Status            string            `json:"status"`
	ActiveRevision    int64             `json:"active_revision,omitempty"`
	ActiveContentHash string            `json:"active_content_hash,omitempty"`
	Checks            []ValidationCheck `json:"checks"`
	CheckedAt         time.Time         `json:"checked_at"`
}

type ServedProductStatus struct {
	Service             string     `json:"service"`
	OrgID               string     `json:"org_id"`
	ProductID           uuid.UUID  `json:"product_id"`
	ProductSurface      string     `json:"product_surface"`
	IntegrationID       uuid.UUID  `json:"integration_id"`
	IntegrationRevision int64      `json:"integration_revision,omitempty"`
	IntegrationHash     string     `json:"integration_hash,omitempty"`
	Area                string     `json:"area"`
	AccessMode          string     `json:"access_mode"`
	Lifecycle           string     `json:"lifecycle"`
	Status              string     `json:"status"`
	Configured          bool       `json:"configured"`
	Observed            bool       `json:"observed"`
	Requests            int64      `json:"requests"`
	Succeeded           int64      `json:"succeeded"`
	Denied              int64      `json:"denied"`
	Failed              int64      `json:"failed"`
	SuccessRate         *float64   `json:"success_rate,omitempty"`
	P95LatencyMS        *float64   `json:"p95_latency_ms,omitempty"`
	LastSeenAt          *time.Time `json:"last_seen_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastErrorCode       string     `json:"last_error_code,omitempty"`
}

type RuntimeContext struct {
	OrgID               string
	ProductID           uuid.UUID
	ProductSurface      string
	IntegrationID       uuid.UUID
	IntegrationRevision int64
	IntegrationHash     string
	AccessMode          string
}

type Observation struct {
	RuntimeContext
	Area       string
	StatusCode int
	Latency    time.Duration
	ErrorCode  string
	ObservedAt time.Time
}

func normalizeSection(in Section) (Section, error) {
	in.SchemaVersion = strings.TrimSpace(in.SchemaVersion)
	if in.SchemaVersion != SchemaVersion {
		return Section{}, domainerr.Validation("unsupported product integration schema version")
	}
	if len(in.APIContracts) == 0 || len(in.APIContracts) > 32 {
		return Section{}, domainerr.Validation("at least one bounded API contract is required")
	}
	seenAPIs := make(map[string]struct{}, len(in.APIContracts))
	for i := range in.APIContracts {
		in.APIContracts[i].Name = strings.ToLower(strings.TrimSpace(in.APIContracts[i].Name))
		in.APIContracts[i].Version = strings.ToLower(strings.TrimSpace(in.APIContracts[i].Version))
		if !codePattern.MatchString(in.APIContracts[i].Name) || !versionPattern.MatchString(in.APIContracts[i].Version) {
			return Section{}, domainerr.Validation("API contract name or version is invalid")
		}
		key := in.APIContracts[i].Name + "@" + in.APIContracts[i].Version
		if _, exists := seenAPIs[key]; exists {
			return Section{}, domainerr.Validation("API contracts must be unique")
		}
		seenAPIs[key] = struct{}{}
	}
	if len(in.ActionTypes) > 256 {
		return Section{}, domainerr.Validation("too many action type references")
	}
	seenActions := make(map[string]struct{}, len(in.ActionTypes))
	for i := range in.ActionTypes {
		in.ActionTypes[i].Key = strings.ToLower(strings.TrimSpace(in.ActionTypes[i].Key))
		if !codePattern.MatchString(in.ActionTypes[i].Key) {
			return Section{}, domainerr.Validation("action type reference is invalid")
		}
		if _, exists := seenActions[in.ActionTypes[i].Key]; exists {
			return Section{}, domainerr.Validation("action type references must be unique")
		}
		seenActions[in.ActionTypes[i].Key] = struct{}{}
	}
	if len(in.AccessModes) == 0 || len(in.AccessModes) > 2 {
		return Section{}, domainerr.Validation("at least one access mode is required")
	}
	seenModes := make(map[string]struct{}, len(in.AccessModes))
	for i := range in.AccessModes {
		in.AccessModes[i] = strings.ToLower(strings.TrimSpace(in.AccessModes[i]))
		if in.AccessModes[i] != "direct" && in.AccessModes[i] != "via_companion" {
			return Section{}, domainerr.Validation("access mode must be direct or via_companion")
		}
		seenModes[in.AccessModes[i]] = struct{}{}
	}
	in.AccessModes = in.AccessModes[:0]
	for _, mode := range []string{"direct", "via_companion"} {
		if _, ok := seenModes[mode]; ok {
			in.AccessModes = append(in.AccessModes, mode)
		}
	}
	if len(in.Webhooks) > 32 {
		return Section{}, domainerr.Validation("too many webhook subscriptions")
	}
	for i := range in.Webhooks {
		hook := &in.Webhooks[i]
		hook.URL = strings.TrimSpace(hook.URL)
		hook.SecretRef = strings.TrimSpace(hook.SecretRef)
		parsed, err := url.Parse(hook.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return Section{}, domainerr.Validation("webhook URL must be an absolute HTTPS URL")
		}
		if !strings.HasPrefix(hook.SecretRef, "secret://") || len(hook.SecretRef) > 512 {
			return Section{}, domainerr.Validation("webhook signing secret must be a secret reference")
		}
		if len(hook.EventTypes) == 0 || len(hook.EventTypes) > 64 {
			return Section{}, domainerr.Validation("webhook event types are required")
		}
		seenEvents := make(map[string]struct{}, len(hook.EventTypes))
		for j := range hook.EventTypes {
			hook.EventTypes[j] = strings.ToLower(strings.TrimSpace(hook.EventTypes[j]))
			if !codePattern.MatchString(hook.EventTypes[j]) {
				return Section{}, domainerr.Validation("webhook event type is invalid")
			}
			if _, exists := seenEvents[hook.EventTypes[j]]; exists {
				return Section{}, domainerr.Validation("webhook event types must be unique")
			}
			seenEvents[hook.EventTypes[j]] = struct{}{}
		}
		slices.Sort(hook.EventTypes)
	}
	slices.SortFunc(in.APIContracts, func(a, b APIContract) int {
		return strings.Compare(a.Name+"@"+a.Version, b.Name+"@"+b.Version)
	})
	slices.SortFunc(in.ActionTypes, func(a, b ActionTypeRef) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(in.Webhooks, func(a, b WebhookSubscription) int { return strings.Compare(a.URL, b.URL) })
	return in, nil
}

// canonicalAccessMode is the neutral persisted/read-model vocabulary. The
// historical via_companion value remains accepted at v2 boundaries only.
func canonicalAccessMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AccessModeViaCompanion, AccessModeViaOrchestrator:
		return AccessModeViaOrchestrator
	case AccessModeDirect:
		return AccessModeDirect
	default:
		return ""
	}
}

func accessModeAllowed(configured []string, requested string) bool {
	requested = canonicalAccessMode(requested)
	if requested == "" {
		return false
	}
	for _, mode := range configured {
		if canonicalAccessMode(mode) == requested {
			return true
		}
	}
	return false
}

func sectionHash(section Section) (string, error) {
	body, err := json.Marshal(section)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func validContentHash(value string) bool {
	return contentHashPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}
