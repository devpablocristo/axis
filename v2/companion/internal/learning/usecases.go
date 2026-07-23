package learning

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// enrichTimeout bounds a single per-candidate LLM rewrite so a slow runtime
// cannot stall a Scan pass on any one candidate (each timeout falls back to the
// deterministic distillation).
const enrichTimeout = 15 * time.Second

type RepositoryPort interface {
	Create(ctx context.Context, orgID string, input NormalizedCreateInput) (Proposal, error)
	List(ctx context.Context, orgID, status string, virployeeID *uuid.UUID) ([]Proposal, error)
	Get(ctx context.Context, orgID string, id uuid.UUID) (Proposal, error)
	Decide(ctx context.Context, orgID string, id uuid.UUID, status, decidedBy string, memoryID *uuid.UUID) (Proposal, error)
	AttachMemory(ctx context.Context, orgID string, id, memoryID uuid.UUID) (Proposal, error)
	Candidates(ctx context.Context, orgID string, minExecutions int) ([]Candidate, error)
	LatestForPair(ctx context.Context, orgID string, virployeeID uuid.UUID, capabilityKey string) (*Proposal, error)
	SuccessfulExecutionTraceIDs(ctx context.Context, orgID string, virployeeID uuid.UUID, capabilityKey string, limit int) ([]string, error)
}

const (
	defaultMinExecutions = 3
	maxSourceTraceIDs    = 20
	// DefaultActorID stamps decisions when the trusted actor header is absent
	// (mirrors the sibling modules).
	DefaultActorID = "system"
)

type UseCases struct {
	repo          RepositoryPort
	minExecutions int
	capabilities  CapabilityChecker
	memory        MemoryInstaller
	authz         Authorizer
	enricher      ProcedureEnricher
	quota         quotas.QuotaPort
	ledger        quotas.UsageLedgerPort
}

func (u *UseCases) SetQuotaPorts(quota quotas.QuotaPort, ledger quotas.UsageLedgerPort) {
	u.quota, u.ledger = quota, ledger
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo, minExecutions: defaultMinExecutions}
}

// SetMinExecutions overrides the default scan threshold (config, gate G4.1:
// thresholds are configuration, never hardcoded per v1).
func (u *UseCases) SetMinExecutions(n int) {
	if n >= 1 {
		u.minExecutions = n
	}
}

// SetCapabilityChecker wires the eval's capability-existence gate.
func (u *UseCases) SetCapabilityChecker(checker CapabilityChecker) {
	u.capabilities = checker
}

// SetMemoryInstaller wires the write port used by Accept to install the
// procedure memory. Without it, Accept fails closed (nothing is installed).
func (u *UseCases) SetMemoryInstaller(installer MemoryInstaller) {
	u.memory = installer
}

// SetAuthorizer wires the per-virployee role gate. Without it, Accept/Dismiss
// fail closed — a decision that installs into (or discards for) a virployee
// must be authorized exactly as a human memory write for that virployee.
func (u *UseCases) SetAuthorizer(authz Authorizer) {
	u.authz = authz
}

// SetProcedureEnricher wires the optional LLM rewriter (PR5). When unset, Scan
// files the deterministic distillation. It only affects the proposal's wording;
// the human Accept gate and eval are unchanged.
func (u *UseCases) SetProcedureEnricher(enricher ProcedureEnricher) {
	u.enricher = enricher
}

// Ingest files a proposal into the inbox as pending. It is the ONLY entry
// point for proposals (analyzer in PR2, LLM enricher in PR5) and never touches
// memories: installation happens exclusively through the human Accept (PR3).
func (u *UseCases) Ingest(ctx context.Context, orgID string, input CreateInput) (Proposal, error) {
	normalized, err := NormalizeCreateInput(input)
	if err != nil {
		return Proposal{}, err
	}
	return u.repo.Create(ctx, orgID, normalized)
}

func (u *UseCases) List(ctx context.Context, orgID, statusFilter string, virployeeID *uuid.UUID) ([]Proposal, error) {
	status, err := NormalizeStatusFilter(statusFilter)
	if err != nil {
		return nil, err
	}
	return u.repo.List(ctx, orgID, status, virployeeID)
}

func (u *UseCases) Get(ctx context.Context, orgID string, id uuid.UUID) (Proposal, error) {
	return u.repo.Get(ctx, orgID, id)
}

// AcceptResult carries the accepted proposal and the eval that gated it.
type AcceptResult struct {
	Proposal Proposal   `json:"proposal"`
	Eval     EvalReport `json:"eval"`
}

// acceptedEvalReport is the report returned on the idempotent replay of an
// already-accepted proposal: the gate demonstrably passed when it was first
// accepted, so the replay must not report passed=false.
func acceptedEvalReport() EvalReport {
	return EvalReport{Passed: true, Checks: []EvalCheck{{Key: "already_accepted", Status: EvalPass, Reason: "proposal was previously accepted"}}}
}

// Accept runs the mandatory eval (G4.2) and, only if it passes, marks the
// proposal accepted and installs it as a procedure memory (provenance=system).
// This is the single path by which a learned procedure becomes memory, and it
// is authorized exactly as a human memory write for the target virployee — the
// agent can never take it (G4.3).
//
// The pending→accepted CLAIM happens before the install, so a concurrent
// Dismiss (or a second Accept) can never leave a memory installed for a
// proposal that ended dismissed, and only one caller ever installs. If the
// install fails after the claim, the proposal stays accepted with no memory id
// and a retry self-heals (the accepted branch re-installs). Idempotent.
func (u *UseCases) Accept(ctx context.Context, orgID string, id uuid.UUID, actor, role string) (AcceptResult, error) {
	actor = orDefaultActor(actor)
	// Fail closed before any mutation: without these the gate is not enforceable.
	if u.memory == nil || u.authz == nil {
		return AcceptResult{}, domainerr.Validation("learning accept is not fully configured")
	}
	proposal, err := u.repo.Get(ctx, orgID, id)
	if err != nil {
		return AcceptResult{}, err
	}
	if err := u.authz.Authorize(ctx, orgID, proposal.VirployeeID, actor, role); err != nil {
		return AcceptResult{}, err
	}

	switch proposal.Status {
	case StatusAccepted:
		return u.ensureInstalled(ctx, orgID, proposal, actor, acceptedEvalReport())
	case StatusDismissed:
		return AcceptResult{}, domainerr.Validation("cannot accept a dismissed proposal")
	}

	report, err := Evaluate(ctx, u.capabilities, proposal)
	if err != nil {
		return AcceptResult{}, err
	}
	if !report.Passed {
		return AcceptResult{Eval: report}, domainerr.Validation("proposal failed evaluation: " + report.FirstFailure())
	}

	// Claim the transition first (atomic, single winner). Only the winner installs.
	claimed, err := u.repo.Decide(ctx, orgID, id, StatusAccepted, actor, nil)
	if err != nil {
		if domainerr.IsConflict(err) {
			// Lost the race. Resolve against the latest state.
			latest, gErr := u.repo.Get(ctx, orgID, id)
			if gErr != nil {
				return AcceptResult{}, gErr
			}
			if latest.Status == StatusAccepted {
				return u.ensureInstalled(ctx, orgID, latest, actor, acceptedEvalReport())
			}
			return AcceptResult{}, domainerr.Conflict("proposal is no longer pending")
		}
		return AcceptResult{}, err
	}
	return u.ensureInstalled(ctx, orgID, claimed, actor, report)
}

// ensureInstalled installs the procedure memory for an already-accepted
// proposal and pins its id. It is idempotent: if the memory id is already
// pinned it returns immediately, and a memory-dedup conflict is treated as
// already-installed.
func (u *UseCases) ensureInstalled(ctx context.Context, orgID string, proposal Proposal, actor string, report EvalReport) (AcceptResult, error) {
	if proposal.MemoryID != nil {
		return AcceptResult{Proposal: proposal, Eval: report}, nil
	}
	source := "learning-proposal:" + proposal.ID.String()
	memoryID, err := u.memory.InstallProcedure(ctx, orgID, proposal.VirployeeID, actor, source, proposal.Title, proposal.Content)
	if err != nil {
		if domainerr.IsConflict(err) {
			// An equivalent active memory already exists; treat as installed.
			// Its id is unknown, so leave memory_id unset.
			return AcceptResult{Proposal: proposal, Eval: report}, nil
		}
		// Claimed accepted but install failed transiently: a retry re-enters the
		// accepted branch and self-heals.
		return AcceptResult{Proposal: proposal, Eval: report}, err
	}
	if attached, aErr := u.repo.AttachMemory(ctx, orgID, proposal.ID, memoryID); aErr == nil {
		proposal = attached
	} else {
		proposal.MemoryID = &memoryID
	}
	return AcceptResult{Proposal: proposal, Eval: report}, nil
}

// Dismiss discards a pending proposal. Idempotent; an accepted proposal cannot
// be dismissed (it is already a memory — dismissing would not remove it).
// Authorized as a human memory decision for the target virployee.
func (u *UseCases) Dismiss(ctx context.Context, orgID string, id uuid.UUID, actor, role string) (Proposal, error) {
	actor = orDefaultActor(actor)
	if u.authz == nil {
		return Proposal{}, domainerr.Validation("learning dismiss is not fully configured")
	}
	proposal, err := u.repo.Get(ctx, orgID, id)
	if err != nil {
		return Proposal{}, err
	}
	if err := u.authz.Authorize(ctx, orgID, proposal.VirployeeID, actor, role); err != nil {
		return Proposal{}, err
	}
	switch proposal.Status {
	case StatusDismissed:
		return proposal, nil
	case StatusAccepted:
		return Proposal{}, domainerr.Validation("cannot dismiss an accepted proposal")
	}
	return u.repo.Decide(ctx, orgID, id, StatusDismissed, actor, nil)
}

func orDefaultActor(actor string) string {
	if actor = strings.TrimSpace(actor); actor != "" {
		return actor
	}
	return DefaultActorID
}

// ProposalRef is a slim pointer to a filed proposal; the inbox List endpoint
// serves the full objects.
type ProposalRef struct {
	ID            uuid.UUID `json:"id"`
	VirployeeID   uuid.UUID `json:"virployee_id"`
	CapabilityKey string    `json:"capability_key"`
	Title         string    `json:"title"`
}

// ScanResult summarizes one analyzer pass.
type ScanResult struct {
	Threshold  int           `json:"threshold"`
	Candidates int           `json:"candidates"`
	Proposed   int           `json:"proposed"`
	Skipped    int           `json:"skipped"`
	Proposals  []ProposalRef `json:"proposals"`
}

// Scan runs the organization-scoped analyzer: find (virployee, capability) pairs
// with enough successful executions and file a proposal for each, unless the
// pair already has a pending/accepted proposal (or a dismissal with no new
// evidence since). Deterministic — no LLM involved. The per-call override can
// only RAISE the configured threshold: thresholds are governance configuration
// (gate G4.1) and a caller must not be able to lower the floor and flood the
// review inbox with one-run "learnings".
func (u *UseCases) Scan(ctx context.Context, orgID string, minExecutions int) (ScanResult, error) {
	if minExecutions < 0 {
		return ScanResult{}, domainerr.Validation("min_executions must be at least 1")
	}
	threshold := u.minExecutions
	if minExecutions > threshold {
		threshold = minExecutions
	}

	candidates, err := u.repo.Candidates(ctx, orgID, threshold)
	if err != nil {
		return ScanResult{}, err
	}
	result := ScanResult{Threshold: threshold, Candidates: len(candidates), Proposals: []ProposalRef{}}
	for _, candidate := range candidates {
		virployeeID, err := uuid.Parse(candidate.VirployeeID)
		if err != nil {
			result.Skipped++
			continue
		}
		latest, err := u.repo.LatestForPair(ctx, orgID, virployeeID, candidate.CapabilityKey)
		if err != nil {
			return ScanResult{}, err
		}
		if !ShouldPropose(latest, candidate.Succeeded) {
			result.Skipped++
			continue
		}
		sources, err := u.repo.SuccessfulExecutionTraceIDs(ctx, orgID, virployeeID, candidate.CapabilityKey, maxSourceTraceIDs)
		if err != nil {
			return ScanResult{}, err
		}
		title, content := Distill(candidate)
		evidence := BuildEvidence(candidate)
		title, content, proposedBy := u.applyEnrichment(ctx, orgID, candidate, title, content, evidence)
		proposal, err := u.Ingest(ctx, orgID, CreateInput{
			VirployeeID:        virployeeID,
			CapabilityKey:      candidate.CapabilityKey,
			Title:              title,
			Content:            content,
			Evidence:           evidence,
			SourceTraceIDs:     sources,
			ProposedBy:         proposedBy,
			SucceededWatermark: candidate.Succeeded,
		})
		if err != nil {
			// One bad candidate must not sink the whole pass: the pending-unique
			// index may race a concurrent scan (conflict), and a malformed
			// candidate (e.g. a legacy capability key from a future executor)
			// fails normalization — both are per-candidate skips, not failures.
			if domainerr.IsConflict(err) || domainerr.IsValidation(err) {
				result.Skipped++
				continue
			}
			return ScanResult{}, err
		}
		result.Proposed++
		result.Proposals = append(result.Proposals, ProposalRef{
			ID:            proposal.ID,
			VirployeeID:   proposal.VirployeeID,
			CapabilityKey: proposal.CapabilityKey,
			Title:         proposal.Title,
		})
	}
	return result, nil
}

// applyEnrichment optionally rewrites the wording of a distilled procedure via
// the LLM (PR5). It is FAIL-SAFE: with no enricher wired, a timeout/error, or an
// unusable rewrite, it returns the deterministic distillation unchanged and
// ProposedByAnalyzer. Only a clean, usable rewrite is used (ProposedByLLM), with
// the model/prompt recorded in evidence. It only shapes the proposal's wording;
// the human Accept gate and eval are unchanged.
func (u *UseCases) applyEnrichment(ctx context.Context, orgID string, candidate Candidate, title, content string, evidence map[string]any) (string, string, string) {
	if u.enricher == nil {
		return title, content, ProposedByAnalyzer
	}
	idempotencyKey := candidate.VirployeeID + ":" + candidate.CapabilityKey + ":" + strconv.FormatInt(candidate.Succeeded, 10)
	reservedUnits := int64(len(title)+len(content)+3)/4 + 2048
	if u.quota != nil {
		if _, err := u.quota.Consume(ctx, quotas.ConsumeRequest{
			Key:            quotas.Key{OrgID: orgID, ProductSurface: quotas.ProductSurfaceFromContext(ctx), Area: quotas.AreaLLM},
			IdempotencyKey: idempotencyKey, SubjectType: "learning_candidate", SubjectID: candidate.VirployeeID, Units: reservedUnits,
		}); err != nil {
			slog.WarnContext(ctx, "learning_enrich_quota_exceeded_fallback_deterministic")
			return title, content, ProposedByAnalyzer
		}
	}
	ectx, cancel := context.WithTimeout(ctx, enrichTimeout)
	defer cancel()
	out, err := u.enricher.Enrich(ectx, EnrichInput{
		CapabilityKey: candidate.CapabilityKey,
		Title:         title,
		Content:       content,
	})
	if err != nil {
		slog.WarnContext(ctx, "learning_enrich_failed_fallback_deterministic",
			"capability_key", candidate.CapabilityKey, "error", err.Error())
		return title, content, ProposedByAnalyzer
	}
	if !out.Enriched || !usableEnrichment(out, candidate.CapabilityKey) {
		if out.Enriched {
			// The model answered but the rewrite was rejected (off-topic, over
			// size, or tripped the secret/PII screen): a systematic misfire is
			// visible here instead of silently always falling back.
			slog.InfoContext(ctx, "learning_enrich_rejected_fallback_deterministic",
				"capability_key", candidate.CapabilityKey)
		}
		return title, content, ProposedByAnalyzer
	}
	if u.ledger != nil {
		_ = u.ledger.RecordUsage(ctx, quotas.Usage{
			Key:            quotas.Key{OrgID: orgID, ProductSurface: quotas.ProductSurfaceFromContext(ctx), Area: quotas.AreaLLM},
			IdempotencyKey: idempotencyKey + ":actual", SubjectType: "learning_candidate", SubjectID: candidate.VirployeeID,
			Units: out.InputTokens + out.OutputTokens, Model: out.ModelID, EstimatedCostMicroUSD: out.EstimatedCostMicroUSD,
			Metadata: map[string]any{"input_tokens": out.InputTokens, "output_tokens": out.OutputTokens, "estimated": true},
		})
	}
	evidence["enriched_by_model"] = out.ModelID
	evidence["enrich_prompt_version"] = out.PromptVersion
	return out.Title, out.Content, ProposedByLLM
}

// usableEnrichment decides whether an LLM rewrite is safe to file instead of the
// deterministic distillation: within the memory size limits, free of obvious
// secrets/PII (so poisoned model output never reaches the DB or the inbox — the
// Accept eval also screens, but only after storage), and still anchored to the
// capability_key. The anchor makes a degenerate rewrite (e.g. an Echo reply that
// never mentions the capability) fall back automatically, without coupling to
// any provider's internals. It is a usability heuristic, not the security gate.
func usableEnrichment(out EnrichOutput, capabilityKey string) bool {
	key := strings.TrimSpace(strings.ToLower(capabilityKey))
	if key == "" {
		return false
	}
	title := strings.TrimSpace(out.Title)
	content := strings.TrimSpace(out.Content)
	if title == "" || len([]rune(title)) > 200 {
		return false
	}
	if content == "" || len([]rune(content)) > 20000 {
		return false
	}
	if blob := out.Title + "\n" + out.Content; containsSecret(blob) || containsPII(blob) {
		return false
	}
	return strings.Contains(strings.ToLower(content), key)
}
