package memories

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// ConflictReader is the persistence port used by the curator. The tenant and
// virployee scope is mandatory so a write can never compare against another
// customer's memory corpus.
type ConflictReader interface {
	HasActiveConflict(context.Context, string, uuid.UUID, uuid.UUID, Scope, string, string, string) (bool, error)
}

// MemoryCuratorPort is the single admission gate for human, system, and
// accepted-learning memory writes.
type MemoryCuratorPort interface {
	Curate(context.Context, string, uuid.UUID, uuid.UUID, CreateInput) (CuratedInput, error)
}

type DefaultCurator struct {
	conflicts ConflictReader
	now       func() time.Time
}

func NewDefaultCurator(conflicts ConflictReader) *DefaultCurator {
	return &DefaultCurator{conflicts: conflicts, now: time.Now}
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|client[_-]?secret|password|private[_-]?key|access[_-]?token|refresh[_-]?token)\s*[:=]\s*["']?[^\s"']{8,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
}

var piiPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{name: "email", re: regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)},
	{name: "phone", re: regexp.MustCompile(`(?:\+?\d[\d .()\-]{7,}\d)`)},
	{name: "government_id", re: regexp.MustCompile(`(?i)\b(?:dni|cuit|cuil|ssn)\s*[:#-]?\s*[0-9][0-9 .-]{6,}\b`)},
}

var poisoningPatterns = []struct {
	name    string
	markers []string
}{
	{name: "instruction_override", markers: []string{"ignore previous instructions", "ignore all previous", "system override", "developer message", "ignora las instrucciones", "olvida las instrucciones"}},
	{name: "permanent_rule", markers: []string{"remember this as a permanent rule", "store this instruction forever", "recordá esto como regla permanente", "guarda esta instrucción para siempre"}},
	{name: "approval_bypass", markers: []string{"skip nexus", "bypass approval", "without approval", "sin aprobación", "evita nexus"}},
}

func (c *DefaultCurator) Curate(ctx context.Context, tenant string, virployee, exclude uuid.UUID, input CreateInput) (CuratedInput, error) {
	in, err := normalizeCreate(input)
	if err != nil {
		return CuratedInput{}, err
	}
	combined := in.Title + "\n" + in.Content
	for _, pattern := range secretPatterns {
		if pattern.MatchString(combined) {
			return CuratedInput{}, domainerr.Validation("memory contains secret material and was not persisted")
		}
	}

	curated := CuratedInput{CreateInput: in}
	if in.Provenance == "human" {
		curated.TrustScore = 0.90
		curated.ReviewState = ReviewApproved
	} else if strings.HasPrefix(in.SourceReference, "learning-proposal:") {
		// Learning installation is reached only after an authorized human accepts
		// the proposal, but it still receives less initial trust than a direct
		// human write.
		curated.TrustScore = 0.80
		curated.ReviewState = ReviewApproved
	} else {
		curated.TrustScore = 0.70
		curated.ReviewState = ReviewPending
		curated.ReviewReason = "system_write_requires_human_review"
	}

	for _, candidate := range piiPatterns {
		if candidate.re.MatchString(combined) {
			curated.PIIFlags = append(curated.PIIFlags, candidate.name)
		}
	}
	if len(curated.PIIFlags) > 0 {
		curated.Sensitivity = "sensitive"
	}

	lower := strings.ToLower(combined)
	for _, candidate := range poisoningPatterns {
		for _, marker := range candidate.markers {
			if strings.Contains(lower, marker) {
				curated.PoisoningFlags = append(curated.PoisoningFlags, candidate.name)
				break
			}
		}
	}
	curated.PoisoningFlags = uniqueSorted(curated.PoisoningFlags)
	curated.PIIFlags = uniqueSorted(curated.PIIFlags)
	if len(curated.PoisoningFlags) > 0 {
		curated.TrustScore *= 0.20
		curated.ReviewState = ReviewQuarantined
		curated.ReviewReason = "prompt_poisoning_detected"
	}

	if c.conflicts != nil && oneOf(in.Type, "fact", "preference") {
		conflict, err := c.conflicts.HasActiveConflict(ctx, strings.TrimSpace(tenant), virployee, exclude, in.Scope, in.Title, in.Type, ContentHash(in.Content))
		if err != nil {
			return CuratedInput{}, err
		}
		if conflict {
			curated.ReviewState = ReviewQuarantined
			curated.ReviewReason = "conflicting_memory_requires_review"
		}
	}

	now := c.now().UTC()
	if oneOf(in.Type, "fact", "note") {
		expiresAt := now.Add(90 * 24 * time.Hour)
		decayAt := now.Add(45 * 24 * time.Hour)
		curated.ExpiresAt, curated.DecayAt = &expiresAt, &decayAt
	} else {
		decayAt := now.Add(90 * 24 * time.Hour)
		curated.DecayAt = &decayAt
	}
	return curated, nil
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
