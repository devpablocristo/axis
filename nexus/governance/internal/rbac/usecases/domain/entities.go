package domain

import (
	"time"

	"github.com/google/uuid"
)

// Role enumera los roles internos de governance que se pueden asignar a un
// par (org_id, user_id) externo. org_id y user_id son referencias opacas
// hacia el SaaS consumidor (pymes/etc), no FK.
type Role string

const (
	RolePolicyAdmin Role = "policy_admin"
	RoleApprover    Role = "approver"
	RoleAuditor     Role = "auditor"
	RoleDelegate    Role = "delegate"
)

func (r Role) Valid() bool {
	switch r {
	case RolePolicyAdmin, RoleApprover, RoleAuditor, RoleDelegate:
		return true
	}
	return false
}

// Assignment representa la asignación de un rol de governance a un usuario
// externo dentro del scope de una organización externa.
type Assignment struct {
	ID        uuid.UUID
	OrgID     string
	UserID    string
	Role      Role
	GrantedBy string
	GrantedAt time.Time
	RevokedAt *time.Time
}
