package runtraces

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/google/uuid"
)

const InputPreviewLimit = 500

type Operation string

const (
	OperationDryRun             Operation = "dry_run"
	OperationExecutionGate      Operation = "execution_gate"
	OperationSimulatedExecution Operation = "simulated_execution"
	OperationExecution          Operation = "execution"
)

type Trace struct {
	ID                uuid.UUID
	OrgID             string
	VirployeeID       uuid.UUID
	Operation         Operation
	InputHash         string
	InputPreview      string
	Intent            map[string]any
	CapabilityID      string
	CapabilityKey     string
	DryRunDecision    string
	GateDecision      string
	GateChecks        []GateCheck
	GovernanceResult  *GovernanceResult
	NexusResult       *GovernanceResult
	ExecutionResult   *ExecutionResult
	BindingHash       string
	MemoryReferences  []memories.Reference
	MemoryContextHash string
	CreatedAt         time.Time
}

type CreateInput struct {
	VirployeeID       uuid.UUID
	Operation         Operation
	Input             string
	Intent            map[string]any
	CapabilityID      string
	CapabilityKey     string
	DryRunDecision    string
	GateDecision      string
	GateChecks        []GateCheck
	GovernanceResult  *GovernanceResult
	NexusResult       *GovernanceResult
	ExecutionResult   *ExecutionResult
	BindingHash       string
	MemoryReferences  []memories.Reference
	MemoryContextHash string
	InputHash         string
	InputPreview      string
}

type GateCheck struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type GovernanceResult struct {
	CheckID              string `json:"check_id,omitempty"`
	Available            bool   `json:"available"`
	Decision             string `json:"decision,omitempty"`
	RiskLevel            string `json:"risk_level,omitempty"`
	Status               string `json:"status,omitempty"`
	DecisionReason       string `json:"decision_reason,omitempty"`
	WouldRequireApproval bool   `json:"would_require_approval,omitempty"`
	BindingHash          string `json:"binding_hash,omitempty"`
	ApprovalID           string `json:"approval_id,omitempty"`
	ApprovalStatus       string `json:"approval_status,omitempty"`
	Error                string `json:"error,omitempty"`
}

// NexusResult is the legacy name retained while v2 API consumers migrate to
// the provider-neutral governance_result field.
type NexusResult = GovernanceResult

type ExecutionResult struct {
	Status                 string `json:"status,omitempty"`
	Mode                   string `json:"mode,omitempty"`
	ApprovalID             string `json:"approval_id,omitempty"`
	ApprovalStatus         string `json:"approval_status,omitempty"`
	BindingHash            string `json:"binding_hash,omitempty"`
	Message                string `json:"message,omitempty"`
	ExternalEffects        bool   `json:"external_effects"`
	ResourceID             string `json:"resource_id,omitempty"`
	DurationMS             int64  `json:"duration_ms,omitempty"`
	GovernanceReportStatus string `json:"governance_report_status,omitempty"`
	NexusReportStatus      string `json:"nexus_report_status,omitempty"`
}

func HashString(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func BindingHash(binding map[string]any) (string, error) {
	if len(binding) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(canonicalValue(binding))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func InputPreview(input string) string {
	return truncate(RedactText(strings.TrimSpace(input)), InputPreviewLimit)
}

func RedactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = RedactValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, RedactValue(item))
		}
		return out
	case string:
		return RedactText(typed)
	default:
		return value
	}
}

func RedactText(value string) string {
	value = secretAssignmentPattern.ReplaceAllString(value, "$1=[REDACTED]")
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	return value
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "_")
	for _, sensitive := range sensitiveKeys {
		if key == sensitive || strings.Contains(key, sensitive) {
			return true
		}
	}
	return false
}

func canonicalValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(typed))
		for _, key := range keys {
			out[key] = canonicalValue(typed[key])
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, canonicalValue(item))
		}
		return out
	default:
		return value
	}
}

var sensitiveKeys = []string{
	"password",
	"secret",
	"token",
	"api_key",
	"authorization",
	"cookie",
	"credential",
}

var (
	secretAssignmentPattern = regexp.MustCompile(`(?i)\b(password|secret|token|api_key|authorization|cookie|credential)\b\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;]+)`)
	bearerPattern           = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/-]+=*`)
)
