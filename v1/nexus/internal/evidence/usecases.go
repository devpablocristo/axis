package evidence

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	approvaldomain "github.com/devpablocristo/nexus/internal/approvals/usecases/domain"
	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	evidencedomain "github.com/devpablocristo/nexus/internal/evidence/usecases/domain"
	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
)

// EvidenceVersion es la versión del formato de evidence pack.
const EvidenceVersion = "1.0"

// Ports: interfaces que el usecase consume.

// RequestReader obtiene la request completa por ID.
type RequestReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (requestdomain.Request, error)
}

// ApprovalReader obtiene la approval asociada a una request.
type ApprovalReader interface {
	GetByRequestID(ctx context.Context, requestID uuid.UUID) (*approvaldomain.Approval, error)
}

// EventLister lista los audit events de una request.
type EventLister interface {
	ListByRequestID(ctx context.Context, requestID uuid.UUID) ([]auditdomain.RequestEvent, error)
}

// AttestationReader obtiene la attestation de una request (si existe).
type AttestationReader interface {
	GetByRequestID(ctx context.Context, requestID uuid.UUID) (requestdomain.Attestation, error)
}

// PackSigner firma un evidence pack.
type PackSigner interface {
	SignPack(pack *evidencedomain.EvidencePack) error
}

// Usecases orquesta la generación de evidence packs.
type Usecases struct {
	requests     RequestReader
	approvals    ApprovalReader
	events       EventLister
	attestations AttestationReader
	signer       PackSigner
}

// NewUsecases crea el usecase de evidence packs.
func NewUsecases(requests RequestReader, approvals ApprovalReader, events EventLister, signer PackSigner) *Usecases {
	return &Usecases{
		requests:  requests,
		approvals: approvals,
		events:    events,
		signer:    signer,
	}
}

// WithAttestationReader agrega el reader de attestations (opcional).
func (u *Usecases) WithAttestationReader(r AttestationReader) *Usecases {
	u.attestations = r
	return u
}

// Generate construye un evidence pack completo y firmado para una request.
func (u *Usecases) Generate(ctx context.Context, requestID uuid.UUID) (evidencedomain.EvidencePack, error) {
	// 1. Obtener request
	req, err := u.requests.GetByID(ctx, requestID)
	if err != nil {
		return evidencedomain.EvidencePack{}, fmt.Errorf("get request: %w", err)
	}

	// 2. Construir secciones
	pack := evidencedomain.EvidencePack{
		Version:     EvidenceVersion,
		GeneratedAt: time.Now().UTC(),
		Request:     buildRequestEvidence(req),
		PolicyEval:  buildPolicyEvidence(req),
	}

	// 3. Approval (si existe)
	approval, err := u.approvals.GetByRequestID(ctx, requestID)
	if err != nil {
		return evidencedomain.EvidencePack{}, fmt.Errorf("get approval: %w", err)
	}
	if approval != nil {
		ae := buildApprovalEvidence(*approval)
		pack.Approval = &ae
	}

	// 4. Execution (si se ejecutó)
	if req.Status == requestdomain.StatusExecuted || req.Status == requestdomain.StatusFailed {
		ee := buildExecutionEvidence(req)
		pack.Execution = &ee
	}

	// 5. Attestation (si existe)
	if u.attestations != nil {
		att, attErr := u.attestations.GetByRequestID(ctx, requestID)
		if attErr == nil {
			ae := buildAttestationEvidence(att)
			pack.Attestation = &ae
		}
		// Si no existe, simplemente no se incluye (no es error)
	}

	// 6. Timeline (audit events)
	events, err := u.events.ListByRequestID(ctx, requestID)
	if err != nil {
		return evidencedomain.EvidencePack{}, fmt.Errorf("list audit events: %w", err)
	}
	pack.Timeline = buildTimeline(events)

	// 7. Firmar
	if err := u.signer.SignPack(&pack); err != nil {
		return evidencedomain.EvidencePack{}, fmt.Errorf("sign evidence pack: %w", err)
	}

	return pack, nil
}

// --- builders ---

func buildRequestEvidence(req requestdomain.Request) evidencedomain.RequestEvidence {
	orgID := ""
	if req.OrgID != nil {
		orgID = strings.TrimSpace(*req.OrgID)
	}
	return evidencedomain.RequestEvidence{
		ID:    req.ID.String(),
		OrgID: orgID,
		Requester: evidencedomain.Requester{
			Type: string(req.RequesterType),
			ID:   req.RequesterID,
			Name: req.RequesterName,
		},
		Action: evidencedomain.Action{
			Type:           req.ActionType,
			TargetSystem:   req.TargetSystem,
			TargetResource: req.TargetResource,
		},
		ActionBinding: req.ActionBinding,
		BindingHash:   req.BindingHash,
		Params:        sanitizeEvidenceMap(req.Params),
		Reason:        req.Reason,
		Context:       req.Context,
		AISummary:     req.AISummary,
		CreatedAt:     req.CreatedAt.Format(time.RFC3339),
	}
}

func buildPolicyEvidence(req requestdomain.Request) evidencedomain.PolicyEvidence {
	pe := evidencedomain.PolicyEvidence{
		RiskLevel:      string(req.RiskLevel),
		Decision:       string(req.Decision),
		DecisionReason: req.DecisionReason,
	}
	if req.PolicyID != nil {
		s := req.PolicyID.String()
		pe.PolicyID = &s
	}
	if req.EvaluatedAt != nil {
		pe.EvaluatedAt = req.EvaluatedAt.Format(time.RFC3339)
	}
	return pe
}

func buildApprovalEvidence(a approvaldomain.Approval) evidencedomain.ApprovalEvidence {
	decisions := make([]evidencedomain.ApprovalDecision, 0, len(a.Decisions))
	for _, d := range a.Decisions {
		decisions = append(decisions, evidencedomain.ApprovalDecision{
			ApproverID: d.ApproverID,
			Action:     d.Action,
			Note:       d.Note,
			DecidedAt:  d.DecidedAt.Format(time.RFC3339),
		})
	}
	ae := evidencedomain.ApprovalEvidence{
		ID:                a.ID.String(),
		Status:            string(a.Status),
		BreakGlass:        a.BreakGlass,
		RequiredApprovals: a.RequiredApprovals,
		Decisions:         decisions,
		FinalDecidedBy:    a.DecidedBy,
	}
	if a.DecidedAt != nil {
		ae.DecidedAt = a.DecidedAt.Format(time.RFC3339)
	}
	return ae
}

func buildExecutionEvidence(req requestdomain.Request) evidencedomain.ExecutionEvidence {
	ee := evidencedomain.ExecutionEvidence{
		Status: string(req.Status),
		Result: sanitizeEvidenceMap(req.ExecutionResult),
		Error:  req.ErrorMessage,
	}
	if req.ExecutedAt != nil {
		ee.ExecutedAt = req.ExecutedAt.Format(time.RFC3339)
	}
	return ee
}

func buildAttestationEvidence(a requestdomain.Attestation) evidencedomain.AttestationEvidence {
	return evidencedomain.AttestationEvidence{
		ID:                a.ID.String(),
		Status:            a.Status,
		ProviderRefs:      sanitizeEvidenceMap(a.ProviderRefs),
		Signature:         a.Signature,
		Attester:          a.Attester,
		Metadata:          sanitizeEvidenceMap(a.Metadata),
		CreatedAt:         a.CreatedAt.Format(time.RFC3339),
		Verified:          a.Verified,
		VerificationError: a.VerificationError,
	}
}

func buildTimeline(events []auditdomain.RequestEvent) []evidencedomain.TimelineEvent {
	timeline := make([]evidencedomain.TimelineEvent, 0, len(events))
	for _, e := range events {
		timeline = append(timeline, evidencedomain.TimelineEvent{
			Event:   e.EventType,
			Actor:   e.ActorID,
			At:      e.CreatedAt.Format(time.RFC3339),
			Summary: e.Summary,
			Data:    sanitizeEvidenceMap(e.Data),
		})
	}
	return timeline
}

func sanitizeEvidenceMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		if isSensitiveEvidenceKey(key) {
			out[key] = "***"
			continue
		}
		out[key] = sanitizeEvidenceValue(value)
	}
	return out
}

func sanitizeEvidenceValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return sanitizeEvidenceMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sanitizeEvidenceValue(item)
		}
		return out
	default:
		return value
	}
}

func isSensitiveEvidenceKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "private_key", "client_secret":
		return true
	default:
		return false
	}
}
