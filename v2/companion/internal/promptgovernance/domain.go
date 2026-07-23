package promptgovernance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

const evaluationFreshness = 24 * time.Hour

type Prompt struct {
	ID          uuid.UUID  `json:"id"`
	OrgID       string     `json:"org_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
}

type PromptVersion struct {
	ID          uuid.UUID `json:"id"`
	OrgID       string    `json:"org_id"`
	PromptID    uuid.UUID `json:"prompt_id"`
	Version     int64     `json:"version"`
	Content     string    `json:"content,omitempty"`
	ContentHash string    `json:"content_hash"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type Simulation struct {
	ID              uuid.UUID `json:"id"`
	PromptVersionID uuid.UUID `json:"prompt_version_id"`
	ContentHash     string    `json:"content_hash"`
	ResultHash      string    `json:"result_hash"`
	Passed          bool      `json:"passed"`
	Findings        any       `json:"findings"`
	CreatedAt       time.Time `json:"created_at"`
}

type EvaluationSuite struct {
	ID           uuid.UUID  `json:"id"`
	OrgID        string     `json:"org_id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	ArtifactType string     `json:"artifact_type"`
	CreatedBy    string     `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
}

type SuiteVersion struct {
	ID         uuid.UUID       `json:"id"`
	SuiteID    uuid.UUID       `json:"suite_id"`
	Version    int64           `json:"version"`
	Dataset    json.RawMessage `json:"dataset"`
	Thresholds json.RawMessage `json:"thresholds"`
	SuiteHash  string          `json:"suite_hash"`
	CreatedBy  string          `json:"created_by"`
	CreatedAt  time.Time       `json:"created_at"`
}

type EvaluationRun struct {
	ID             uuid.UUID       `json:"id"`
	SuiteVersionID uuid.UUID       `json:"suite_version_id"`
	ArtifactType   string          `json:"artifact_type"`
	ArtifactRef    string          `json:"artifact_ref"`
	ArtifactHash   string          `json:"artifact_hash"`
	ProductID      string          `json:"product_id"`
	SnapshotHash   string          `json:"snapshot_hash"`
	Status         string          `json:"status"`
	Passed         bool            `json:"passed"`
	Metrics        json.RawMessage `json:"metrics"`
	ReportHash     string          `json:"report_hash"`
	CreatedBy      string          `json:"created_by"`
	StartedAt      time.Time       `json:"started_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}

type Binding struct {
	ID                uuid.UUID  `json:"id"`
	TargetType        string     `json:"target_type"`
	TargetID          uuid.UUID  `json:"target_id"`
	ProductID         string     `json:"product_id"`
	PromptVersionID   uuid.UUID  `json:"prompt_version_id"`
	Revision          int64      `json:"revision"`
	EvaluationRunID   *uuid.UUID `json:"evaluation_run_id,omitempty"`
	AuthorizationHash string     `json:"authorization_hash"`
	PromotedBy        string     `json:"promoted_by"`
	PromotedAt        time.Time  `json:"promoted_at"`
}

type ResolvedPrompt struct {
	Level       string    `json:"level"`
	TargetID    uuid.UUID `json:"target_id"`
	VersionID   uuid.UUID `json:"version_id"`
	Version     int64     `json:"version"`
	ContentHash string    `json:"content_hash"`
	Content     string    `json:"content,omitempty"`
	ProductID   string    `json:"product_id,omitempty"`
}

type Resolution struct {
	OrgID             string           `json:"org_id"`
	ProductID         string           `json:"product_id"`
	VirployeeID       uuid.UUID        `json:"virployee_id"`
	PromptBundleHash  string           `json:"prompt_bundle_hash"`
	EffectiveContent  string           `json:"effective_content,omitempty"`
	ResolvedVersions  []ResolvedPrompt `json:"resolved_versions"`
	EvaluationUnknown bool             `json:"evaluation_unknown"`
}

func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func hashText(value string) string { return hashBytes([]byte(value)) }

func canonicalHash(value any) string {
	raw, _ := json.Marshal(value)
	return hashBytes(raw)
}

func normalizeTarget(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
