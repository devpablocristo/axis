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
	SchemaVersion           = "axis.product-integration.v2"
	FunctionalSchemaVersion = "axis.product-integration.v3"
)

var (
	codePattern = regexp.MustCompile(`^[a-z][a-z0-9._:-]{0,127}$`)
	hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type APIContract struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type CapabilityRef struct {
	ID           string `json:"id,omitempty"`
	Key          string `json:"key,omitempty"`
	Version      string `json:"version"`
	ManifestHash string `json:"manifest_hash"`
}

type EventContract struct {
	Type       string          `json:"type"`
	Version    string          `json:"version"`
	Schema     json.RawMessage `json:"schema"`
	SchemaHash string          `json:"schema_hash"`
}

type WebhookSubscription struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
	SecretRef  string   `json:"secret_ref"`
}

type Section struct {
	SchemaVersion string                `json:"schema_version"`
	APIContracts  []APIContract         `json:"api_contracts"`
	VirployeeIDs  []uuid.UUID           `json:"virployee_ids,omitempty"`
	PoolIDs       []uuid.UUID           `json:"pool_ids,omitempty"`
	Capabilities  []CapabilityRef       `json:"capabilities,omitempty"`
	Events        []EventContract       `json:"events,omitempty"`
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

type Version struct {
	ID                  uuid.UUID `json:"id"`
	IntegrationID       uuid.UUID `json:"integration_id"`
	Revision            int64     `json:"revision"`
	SourceIntegrationID uuid.UUID `json:"source_integration_id"`
	SourceVersionID     uuid.UUID `json:"source_version_id"`
	SourceRevision      int64     `json:"source_revision"`
	ContractHash        string    `json:"contract_hash"`
	Section             Section   `json:"section"`
	ContentHash         string    `json:"content_hash"`
	Status              string    `json:"status"`
	CreatedBy           string    `json:"created_by"`
	CreatedAt           time.Time `json:"created_at"`
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

type RuntimeContext struct {
	OrgID               string
	ProductID           uuid.UUID
	ProductSurface      string
	IntegrationID       uuid.UUID
	IntegrationRevision int64
	IntegrationHash     string
	AccessMode          string
}

type ServedProduct struct {
	Service             string     `json:"service"`
	OrgID               string     `json:"org_id"`
	ProductID           uuid.UUID  `json:"product_id"`
	ProductSurface      string     `json:"product_surface"`
	IntegrationID       uuid.UUID  `json:"integration_id"`
	IntegrationRevision int64      `json:"integration_revision,omitempty"`
	IntegrationHash     string     `json:"integration_hash,omitempty"`
	Area                string     `json:"area"`
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

func normalizeSection(in Section) (Section, error) {
	in.SchemaVersion = strings.TrimSpace(in.SchemaVersion)
	if (in.SchemaVersion != SchemaVersion && in.SchemaVersion != FunctionalSchemaVersion) ||
		len(in.APIContracts) == 0 || len(in.APIContracts) > 32 {
		return Section{}, domainerr.Validation("Companion product integration schema or APIs are invalid")
	}
	seen := map[string]struct{}{}
	for i := range in.APIContracts {
		in.APIContracts[i].Name = strings.ToLower(strings.TrimSpace(in.APIContracts[i].Name))
		in.APIContracts[i].Version = strings.ToLower(strings.TrimSpace(in.APIContracts[i].Version))
		key := in.APIContracts[i].Name + "@" + in.APIContracts[i].Version
		if !codePattern.MatchString(in.APIContracts[i].Name) || in.APIContracts[i].Version != "v1" {
			return Section{}, domainerr.Validation("Companion API contract is unsupported")
		}
		if _, ok := seen[key]; ok {
			return Section{}, domainerr.Validation("Companion API contracts must be unique")
		}
		seen[key] = struct{}{}
	}
	for _, id := range append(slices.Clone(in.VirployeeIDs), in.PoolIDs...) {
		if id == uuid.Nil {
			return Section{}, domainerr.Validation("Companion entrypoint ID is invalid")
		}
	}
	for i := range in.Capabilities {
		ref := &in.Capabilities[i]
		ref.ID = strings.TrimSpace(ref.ID)
		ref.Key = strings.ToLower(strings.TrimSpace(ref.Key))
		ref.Version = strings.TrimSpace(ref.Version)
		ref.ManifestHash = strings.ToLower(strings.TrimSpace(ref.ManifestHash))
		capabilityID, idErr := uuid.Parse(ref.ID)
		validIdentity := codePattern.MatchString(ref.Key)
		if in.SchemaVersion == FunctionalSchemaVersion {
			validIdentity = idErr == nil && capabilityID != uuid.Nil &&
				(ref.Key == "" || codePattern.MatchString(ref.Key))
		} else if ref.ID != "" {
			validIdentity = validIdentity && idErr == nil && capabilityID != uuid.Nil
		}
		if !validIdentity || ref.Version == "" || !hashPattern.MatchString(ref.ManifestHash) {
			return Section{}, domainerr.Validation("Companion capability reference is invalid")
		}
		if idErr == nil && capabilityID != uuid.Nil {
			ref.ID = capabilityID.String()
		}
	}
	for i := range in.Events {
		event := &in.Events[i]
		event.Type = strings.ToLower(strings.TrimSpace(event.Type))
		event.Version = strings.ToLower(strings.TrimSpace(event.Version))
		event.SchemaHash = strings.ToLower(strings.TrimSpace(event.SchemaHash))
		sum := sha256.Sum256(event.Schema)
		if !codePattern.MatchString(event.Type) || event.Version != "v1" || !json.Valid(event.Schema) ||
			hex.EncodeToString(sum[:]) != event.SchemaHash {
			return Section{}, domainerr.Validation("Companion event contract is invalid")
		}
	}
	for i := range in.Webhooks {
		hook := &in.Webhooks[i]
		parsed, err := url.Parse(strings.TrimSpace(hook.URL))
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
			!strings.HasPrefix(strings.TrimSpace(hook.SecretRef), "secret://") {
			return Section{}, domainerr.Validation("Companion webhook configuration is unsafe")
		}
	}
	slices.SortFunc(in.APIContracts, func(a, b APIContract) int {
		return strings.Compare(a.Name+"@"+a.Version, b.Name+"@"+b.Version)
	})
	slices.SortFunc(in.Capabilities, func(a, b CapabilityRef) int {
		aIdentity, bIdentity := a.ID, b.ID
		if aIdentity == "" {
			aIdentity = a.Key
		}
		if bIdentity == "" {
			bIdentity = b.Key
		}
		return strings.Compare(aIdentity+"@"+a.Version, bIdentity+"@"+b.Version)
	})
	return in, nil
}

func sectionHash(section Section) (string, error) {
	raw, err := json.Marshal(section)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
