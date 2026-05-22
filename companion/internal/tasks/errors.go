package tasks

import (
	"errors"
	"fmt"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

// ErrInvalidTaskState indica que la tarea no admite la operación en su estado actual.
var ErrInvalidTaskState = domainerr.Conflict("invalid task state")

// ErrGovernanceSubmit indica que la propuesta a Nexus falló al enviarse. Se
// envuelve con %w para que callers usen errors.Is en vez de string match.
var ErrGovernanceSubmit = errors.New("governance submit failed")

// ErrGovernanceNotApproved indica que una capability con requires_governance=true
// fue invocada sin que la governance en Nexus esté en estado aprobado. Es un
// caso especial de invalid state, distinguible para que el handler lo mapee
// a HTTP 412 (precondition_failed) con detalle del governance_request_id y status.
var ErrGovernanceNotApproved = errors.New("governance not approved")

// GovernanceBlockedError contiene el contexto de un bloqueo por governance no aprobada.
// El handler lo extrae para incluir governance_request_id y governance_status en el body.
type GovernanceBlockedError struct {
	GovernanceRequestID string
	GovernanceStatus    string
	Reason              string
}

func (e *GovernanceBlockedError) Error() string {
	if e.GovernanceRequestID == "" {
		return fmt.Sprintf("%s (status=%s)", ErrGovernanceNotApproved.Error(), e.GovernanceStatus)
	}
	return fmt.Sprintf("%s (governance_request_id=%s status=%s)", ErrGovernanceNotApproved.Error(), e.GovernanceRequestID, e.GovernanceStatus)
}

// Unwrap permite errors.Is(err, ErrGovernanceNotApproved).
func (e *GovernanceBlockedError) Unwrap() error {
	return ErrGovernanceNotApproved
}

// IsGovernanceNotApproved indica que la operación está bloqueada por una governance
// no aprobada en Nexus Governance.
func IsGovernanceNotApproved(err error) bool {
	return errors.Is(err, ErrGovernanceNotApproved)
}

// AsGovernanceBlocked devuelve el detalle estructurado si el error es un bloqueo
// por governance. Útil para que el handler construya el body con context.
func AsGovernanceBlocked(err error) (*GovernanceBlockedError, bool) {
	var blocked *GovernanceBlockedError
	if errors.As(err, &blocked) {
		return blocked, true
	}
	return nil, false
}
