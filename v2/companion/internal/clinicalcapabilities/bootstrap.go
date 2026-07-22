package clinicalcapabilities

import (
	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/quotas"
)

type Definition struct {
	CapabilityKey         string          `json:"capability_key"`
	Name                  string          `json:"name"`
	Description           string          `json:"description"`
	RequiredAutonomy      string          `json:"required_autonomy"`
	RiskClass             string          `json:"risk_class"`
	SideEffectClass       string          `json:"side_effect_class"`
	RequiresNexusApproval bool            `json:"requires_nexus_approval"`
	EvidenceRequired      bool            `json:"evidence_required"`
	Manifest              domain.Manifest `json:"manifest"`
	JobRoleNames          []string        `json:"job_role_names"`
}

func Definitions() []Definition {
	return []Definition{
		{
			CapabilityKey: RecordsSearchKey, Name: "Clinical records search",
			Description:      "Evidence-bound search over an authorized clinical repository generation.",
			RequiredAutonomy: "A0", RiskClass: "medium", SideEffectClass: "read",
			RequiresNexusApproval: false, EvidenceRequired: true,
			Manifest: domain.Manifest{
				Version: "1.0.0", ProductSurface: "medmory", InputSchema: SearchInputSchema(), OutputSchema: SearchOutputSchema(),
				RequiredScopes: []string{"assist:run", "documents:read"},
				Idempotency:    domain.IdempotencyContract{Mode: "required", KeyFields: []string{"tenant_id", "subject_id", "case_id", "repository_generation", "query", "cursor"}},
				RollbackMode:   "none", TimeoutMS: 30000, Retry: domain.RetryContract{MaxAttempts: 1, BackoffMS: 1000},
				Postconditions: []string{"result references remain bound to the authorized repository generation"},
				QuotaAreas:     []string{quotas.AreaInbound, quotas.AreaEmbeddings}, CostClass: "medium",
			},
			JobRoleNames: []string{"Medical Historian"},
		},
		{
			CapabilityKey: TimelineBuildKey, Name: "Clinical timeline build",
			Description:      "Regenerable, evidence-bound clinical timeline projection.",
			RequiredAutonomy: "A1", RiskClass: "medium", SideEffectClass: "read",
			RequiresNexusApproval: false, EvidenceRequired: true,
			Manifest: domain.Manifest{
				Version: "1.0.0", ProductSurface: "medmory", InputSchema: TimelineInputSchema(), OutputSchema: TimelineOutputSchema(),
				RequiredScopes: []string{"assist:run", "documents:read"},
				Idempotency:    domain.IdempotencyContract{Mode: "required", KeyFields: []string{"tenant_id", "subject_id", "case_id", "repository_generation", "date_from", "date_to", "order", "max_events", "focus"}},
				RollbackMode:   "none", TimeoutMS: 120000, Retry: domain.RetryContract{MaxAttempts: 1, BackoffMS: 1000},
				Postconditions: []string{"every emitted event has at least one canonical authorized reference"},
				QuotaAreas:     []string{quotas.AreaInbound, quotas.AreaLLM}, CostClass: "medium",
			},
			JobRoleNames: []string{"Medical Historian", "Study Analyst", "Care Coordinator"},
		},
	}
}
