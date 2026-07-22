package enterpriseops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type Incident struct {
	ID              uuid.UUID       `json:"id"`
	Fingerprint     string          `json:"fingerprint"`
	Source          string          `json:"source"`
	IncidentType    string          `json:"incident_type"`
	ResourceType    string          `json:"resource_type"`
	ResourceID      string          `json:"resource_id"`
	Severity        string          `json:"severity"`
	Status          string          `json:"status"`
	OccurrenceCount int64           `json:"occurrence_count"`
	StateBased      bool            `json:"state_based"`
	FirstSeen       time.Time       `json:"first_seen"`
	LastSeen        time.Time       `json:"last_seen"`
	SuppressUntil   *time.Time      `json:"suppress_until,omitempty"`
	Revision        int64           `json:"revision"`
	Metadata        json.RawMessage `json:"metadata"`
}

type FindingInput struct {
	RunID        string          `json:"run_id"`
	FindingType  string          `json:"finding_type"`
	Severity     string          `json:"severity"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Fingerprint  string          `json:"fingerprint"`
	StateBased   bool            `json:"state_based"`
	Metadata     json.RawMessage `json:"metadata"`
}

type IncidentActionInput struct {
	ReasonCode       string     `json:"reason_code"`
	ExpectedRevision int64      `json:"expected_revision"`
	SuppressUntil    *time.Time `json:"suppress_until,omitempty"`
}

type ReconciliationRun struct {
	ID             uuid.UUID  `json:"id"`
	ProductSurface string     `json:"product_surface"`
	Mode           string     `json:"mode"`
	Trigger        string     `json:"trigger"`
	Status         string     `json:"status"`
	FindingsCount  int        `json:"findings_count"`
	RepairedCount  int        `json:"repaired_count"`
	ReportHash     string     `json:"report_hash"`
	ErrorCode      string     `json:"error_code,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type ReconciliationInput struct {
	Mode string `json:"mode"`
}

type JobView struct {
	Service        string     `json:"service"`
	ID             uuid.UUID  `json:"id"`
	ProductSurface string     `json:"product_surface"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	EffectClass    string     `json:"effect_class"`
	ReplayPolicy   string     `json:"replay_policy"`
	DedupeKeyHash  string     `json:"dedupe_key_hash"`
	PayloadHash    string     `json:"payload_hash"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	RunAfter       time.Time  `json:"run_after"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
	LastErrorCode  string     `json:"last_error_code,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type SLO struct {
	ProductSurface string    `json:"product_surface"`
	MetricKey      string    `json:"metric_key"`
	Comparator     string    `json:"comparator"`
	Target         float64   `json:"target"`
	WindowSeconds  int       `json:"window_seconds"`
	MinimumSamples int       `json:"minimum_samples"`
	Severity       string    `json:"severity"`
	Enabled        bool      `json:"enabled"`
	Revision       int64     `json:"revision"`
	Status         string    `json:"status"`
	Value          *float64  `json:"value,omitempty"`
	SampleCount    int       `json:"sample_count"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PutSLOInput struct {
	ProductSurface   string  `json:"product_surface"`
	MetricKey        string  `json:"metric_key"`
	Comparator       string  `json:"comparator"`
	Target           float64 `json:"target"`
	WindowSeconds    int     `json:"window_seconds"`
	MinimumSamples   int     `json:"minimum_samples"`
	Severity         string  `json:"severity"`
	Enabled          bool    `json:"enabled"`
	ExpectedRevision int64   `json:"expected_revision"`
}

type LegalHold struct {
	ID                uuid.UUID  `json:"id"`
	ScopeType         string     `json:"scope_type"`
	ScopeID           string     `json:"scope_id"`
	ReasonCode        string     `json:"reason_code"`
	ExternalReference string     `json:"external_reference,omitempty"`
	Status            string     `json:"status"`
	Revision          int64      `json:"revision"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	ReleasedBy        string     `json:"released_by,omitempty"`
	ReleasedAt        *time.Time `json:"released_at,omitempty"`
	ReleaseReason     string     `json:"release_reason,omitempty"`
}

type CreateLegalHoldInput struct {
	ScopeType         string `json:"scope_type"`
	ScopeID           string `json:"scope_id"`
	ReasonCode        string `json:"reason_code"`
	ExternalReference string `json:"external_reference"`
}

type ReleaseLegalHoldInput struct {
	ReasonCode       string `json:"reason_code"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type Export struct {
	ID           uuid.UUID       `json:"id"`
	ScopeType    string          `json:"scope_type"`
	ScopeID      string          `json:"scope_id"`
	Categories   json.RawMessage `json:"categories"`
	Status       string          `json:"status"`
	Manifest     json.RawMessage `json:"manifest,omitempty"`
	ManifestHash string          `json:"manifest_hash,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	RequestedBy  string          `json:"requested_by"`
	RequestedAt  time.Time       `json:"requested_at"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	ExpiresAt    *time.Time      `json:"expires_at,omitempty"`
}

type CreateExportInput struct {
	ScopeType  string   `json:"scope_type"`
	ScopeID    string   `json:"scope_id"`
	Categories []string `json:"categories"`
}

type DownloadToken struct {
	Token        string    `json:"token"`
	ExportID     uuid.UUID `json:"export_id"`
	ManifestHash string    `json:"manifest_hash"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Overview struct {
	Service     string         `json:"service"`
	Status      string         `json:"status"`
	Incidents   map[string]int `json:"incidents"`
	Jobs        map[string]int `json:"jobs"`
	ActiveHolds int            `json:"active_holds"`
	Exports     map[string]int `json:"exports"`
	GeneratedAt time.Time      `json:"generated_at"`
}

type WorkerControl struct {
	Service        string     `json:"service"`
	ProductSurface string     `json:"product_surface"`
	JobKind        string     `json:"job_kind"`
	State          string     `json:"state"`
	FailureCount   int        `json:"failure_count"`
	OpenedUntil    *time.Time `json:"opened_until,omitempty"`
	Revision       int64      `json:"version"`
	ReasonCode     string     `json:"reason_code"`
	ChangedBy      string     `json:"changed_by"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
type PutWorkerControlInput struct {
	JobKind         string `json:"job_kind"`
	State           string `json:"state"`
	ReasonCode      string `json:"reason_code"`
	ExpectedVersion int64  `json:"expected_version"`
}
type NotificationPolicy struct {
	Enabled          bool      `json:"enabled"`
	WebhookSecretRef string    `json:"webhook_secret_ref"`
	Revision         int64     `json:"revision"`
	ChangedBy        string    `json:"changed_by"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type PutNotificationPolicyInput struct {
	Enabled          bool   `json:"enabled"`
	WebhookSecretRef string `json:"webhook_secret_ref"`
	ExpectedRevision int64  `json:"expected_revision"`
}

func normalizeFinding(in FindingInput) (FindingInput, error) {
	in.RunID = strings.TrimSpace(in.RunID)
	in.FindingType = strings.ToLower(strings.TrimSpace(in.FindingType))
	in.Severity = strings.ToLower(strings.TrimSpace(in.Severity))
	in.ResourceType = strings.ToLower(strings.TrimSpace(in.ResourceType))
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	in.Fingerprint = strings.ToLower(strings.TrimSpace(in.Fingerprint))
	if _, err := uuid.Parse(in.RunID); err != nil || !codePattern.MatchString(in.FindingType) || !codePattern.MatchString(in.ResourceType) ||
		in.ResourceID == "" || len(in.ResourceID) > 512 || strings.ContainsAny(in.ResourceID, "\r\n") ||
		!shaPattern.MatchString(in.Fingerprint) || !oneOf(in.Severity, "info", "warning", "high", "critical") {
		return FindingInput{}, domainerr.Validation("operational finding metadata is invalid")
	}
	if len(in.Metadata) == 0 {
		in.Metadata = json.RawMessage(`{}`)
	}
	if !json.Valid(in.Metadata) || len(in.Metadata) > 4096 {
		return FindingInput{}, domainerr.Validation("finding metadata is invalid")
	}
	var object map[string]any
	if json.Unmarshal(in.Metadata, &object) != nil || object == nil || !safeFindingMetadata(object) {
		return FindingInput{}, domainerr.Validation("finding metadata must be an object")
	}
	return in, nil
}

func safeFindingMetadata(metadata map[string]any) bool {
	allowed := map[string]bool{"check": true, "expected_hash": true, "observed_hash": true, "age_seconds": true, "count": true, "service": true, "job_kind": true, "status": true, "policy_version": true, "assignment_version": true}
	for key, value := range metadata {
		if !allowed[key] {
			return false
		}
		switch item := value.(type) {
		case nil, bool, float64:
		case string:
			if len(item) > 256 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
