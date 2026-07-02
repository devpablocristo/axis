package delegations

import (
	"context"
	"fmt"
	"strings"
	"time"

	domain "github.com/devpablocristo/nexus/internal/delegations/usecases/domain"
	"github.com/google/uuid"
)

type delegationRepository interface {
	Create(ctx context.Context, d domain.Delegation) (domain.Delegation, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Delegation, error)
	ListByAgentID(ctx context.Context, agentID string, orgID *string) ([]domain.Delegation, error)
	ExistsByAgent(ctx context.Context, agentID string) (bool, error)
	List(ctx context.Context) ([]domain.Delegation, error)
	Update(ctx context.Context, d domain.Delegation) (domain.Delegation, error)
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

type Usecases struct {
	repo delegationRepository
}

func NewUsecases(repo delegationRepository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) Create(ctx context.Context, d domain.Delegation) (domain.Delegation, error) {
	if d.OwnerID == "" || d.AgentID == "" {
		return domain.Delegation{}, fmt.Errorf("owner_id and agent_id are required")
	}
	if d.MaxRiskClass == "" {
		d.MaxRiskClass = "high"
	}
	d.Enabled = true
	return u.repo.Create(ctx, d)
}

func (u *Usecases) GetByID(ctx context.Context, id uuid.UUID) (domain.Delegation, error) {
	return u.repo.GetByID(ctx, id)
}

func (u *Usecases) List(ctx context.Context) ([]domain.Delegation, error) {
	return u.repo.List(ctx)
}

// ListByAgentID lista delegaciones del agente filtradas por org. orgID puede
// ser nil para listar sólo las globales.
func (u *Usecases) ListByAgentID(ctx context.Context, agentID string, orgID *string) ([]domain.Delegation, error) {
	return u.repo.ListByAgentID(ctx, agentID, orgID)
}

func (u *Usecases) Update(ctx context.Context, d domain.Delegation) (domain.Delegation, error) {
	return u.repo.Update(ctx, d)
}

func (u *Usecases) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return u.repo.DeleteByID(ctx, id)
}

// CheckDelegation verifica si un agente tiene delegación para una acción.
// Retorna (true, delegation) si tiene, (false, empty) si no.
func (u *Usecases) CheckDelegation(ctx context.Context, agentID, actionType string, orgID *string) (bool, domain.Delegation, error) {
	// Defense-in-depth: además del filtro in-memory de delegationAppliesToOrg,
	// el repo ya excluye delegaciones de otros orgs en SQL.
	delegations, err := u.repo.ListByAgentID(ctx, agentID, orgID)
	if err != nil {
		return false, domain.Delegation{}, fmt.Errorf("list delegations: %w", err)
	}

	if len(delegations) == 0 {
		// Distinguir: ¿el agente NO tiene ninguna delegación (entonces no
		// está restringido), o tiene alguna en otro org (entonces está
		// restringido y este org no le aplica)?
		exists, err := u.repo.ExistsByAgent(ctx, agentID)
		if err != nil {
			return false, domain.Delegation{}, fmt.Errorf("check delegation existence: %w", err)
		}
		if exists {
			// Hay delegaciones del agente en otro(s) org(s) pero no para
			// éste → restringido en este org.
			return false, domain.Delegation{}, nil
		}
		// Sin delegaciones en ningún lado = sin restricciones (compat PoC).
		return true, domain.Delegation{}, nil
	}

	now := time.Now().UTC()
	for _, d := range delegations {
		if !d.Enabled {
			continue
		}
		if !delegationAppliesToOrg(d, orgID) {
			continue
		}
		// Defense-in-depth: el repo SQL ya filtra por expires_at, pero si el
		// repo cambia o un fake en tests devuelve expiradas, acá las cortamos.
		if d.ExpiresAt != nil && !d.ExpiresAt.After(now) {
			continue
		}
		// Si no tiene restricción de action_types, matchea todo
		if len(d.AllowedActionTypes) == 0 {
			return true, d, nil
		}
		for _, at := range d.AllowedActionTypes {
			if at == actionType {
				return true, d, nil
			}
		}
	}

	return false, domain.Delegation{}, nil
}

func delegationAppliesToOrg(d domain.Delegation, orgID *string) bool {
	if d.OrgID == nil {
		return true
	}
	if orgID == nil {
		return false
	}
	return strings.TrimSpace(*d.OrgID) == strings.TrimSpace(*orgID)
}
