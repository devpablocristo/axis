package learning

import (
	"context"
	"regexp"
	"strings"
)

// CapabilityChecker reports whether a capability_key is a real, active
// capability of the tenant. The eval fails closed if this is unavailable.
type CapabilityChecker interface {
	IsActiveCapability(ctx context.Context, tenantID, capabilityKey string) (bool, error)
}

const (
	EvalPass    = "pass"
	EvalBlocked = "blocked"
)

type EvalCheck struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// EvalReport is the mandatory automatic gate (G4.2) that runs between a pending
// proposal and the human accept. A proposal that does not pass CANNOT be
// installed as a memory, no matter who clicks accept.
type EvalReport struct {
	Passed bool        `json:"passed"`
	Checks []EvalCheck `json:"checks"`
}

// FirstFailure returns the reason of the first blocked check, for error text.
func (r EvalReport) FirstFailure() string {
	for _, c := range r.Checks {
		if c.Status == EvalBlocked {
			return c.Reason
		}
	}
	return ""
}

var (
	// Detection (not redaction): does the text CONTAIN a secret/PII pattern?
	// This is a BEST-EFFORT backstop, not a complete DLP scanner — it cannot
	// catch every secret phrased in prose or every kind of personal data. The
	// analyzer's distilled text never contains these (it is structural), so the
	// gate exists mainly to stop an LLM-authored proposal (PR5) from smuggling
	// obvious credentials or contact data into memory. It fails safe: any match
	// blocks; a miss is possible, so this is defense-in-depth, not the only line.
	secretPattern     = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key|access[_-]?key|authorization|cookie|credential|private[_-]?key)\b\s*[:=]`)
	bearerPattern     = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/-]{8,}`)
	privateKeyPattern = regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	// Common credential-looking tokens (AWS keys, GitHub/Slack/Google tokens).
	tokenPattern = regexp.MustCompile(`(?i)\b(AKIA[0-9A-Z]{12,}|gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_\-]{30,})\b`)
	// Email is the one PII signal safe to flag on structural procedure text.
	// A phone/date regex is deliberately NOT used: the distilled content carries
	// ISO dates (e.g. 2026-07-14) that a digit-run pattern would falsely flag.
	// Broader PII/DLP is out of scope for this gate (see no_pii wording).
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
)

// Evaluate runs every gate and only passes if all pass. It is pure except for
// the capability lookup. Checker nil ⇒ fail closed.
func Evaluate(ctx context.Context, checker CapabilityChecker, proposal Proposal) (EvalReport, error) {
	report := EvalReport{Passed: true}
	add := func(key, reason string, ok bool) {
		status := EvalPass
		if !ok {
			status = EvalBlocked
			report.Passed = false
		}
		report.Checks = append(report.Checks, EvalCheck{Key: key, Status: status, Reason: reason})
	}

	// 1. The procedure must reference a real, active capability of the tenant.
	if checker == nil {
		add("capability_real", "capability checker unavailable", false)
	} else {
		active, err := checker.IsActiveCapability(ctx, proposal.TenantID, proposal.CapabilityKey)
		if err != nil {
			return EvalReport{}, err
		}
		add("capability_real", "capability_key must be an active capability of the tenant", active)
	}

	// 2. Installable as a procedure memory (same limits the memories module
	//    enforces, re-checked here so an LLM-authored proposal cannot slip past).
	title := strings.TrimSpace(proposal.Title)
	content := strings.TrimSpace(proposal.Content)
	installable := title != "" && len([]rune(title)) <= 200 && content != "" && len([]rune(content)) <= 20000
	add("installable", "title and content must be non-empty and within memory size limits", installable)

	// 3. No obvious secrets and 4. no obvious PII in what would become memory
	//    (best-effort screen — see the pattern comments).
	blob := proposal.Title + "\n" + proposal.Content
	noSecrets := !secretPattern.MatchString(blob) && !bearerPattern.MatchString(blob) &&
		!privateKeyPattern.MatchString(blob) && !tokenPattern.MatchString(blob)
	add("no_secrets", "content must not contain secret assignments, keys, or bearer/credential tokens", noSecrets)
	add("no_pii", "content must not contain email-like personal data (best-effort)", !emailPattern.MatchString(blob))

	return report, nil
}
