package tasks

import (
	"errors"
	"fmt"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

// ErrInvalidTaskState indica que la tarea no admite la operación en su estado actual.
var ErrInvalidTaskState = domainerr.Conflict("invalid task state")

// ErrNexusSubmit indica que la propuesta a Nexus falló al enviarse. Se
// envuelve con %w para que callers usen errors.Is en vez de string match.
var ErrNexusSubmit = errors.New("nexus submit failed")

// ErrNexusNotApproved indica que una capability con requires_nexus_approval=true
// fue invocada sin que la aprobación en Nexus esté resuelta. Es un
// caso especial de invalid state, distinguible para que el handler lo mapee
// a HTTP 412 (precondition_failed) con detalle del nexus_request_id y status.
var ErrNexusNotApproved = errors.New("nexus not approved")

// NexusBlockedError contiene el contexto de un bloqueo por Nexus.
// El handler lo extrae para incluir nexus_request_id y nexus_status en el body.
type NexusBlockedError struct {
	NexusRequestID string
	NexusStatus    string
	Reason         string
}

func (e *NexusBlockedError) Error() string {
	if e.NexusRequestID == "" {
		return fmt.Sprintf("%s (status=%s)", ErrNexusNotApproved.Error(), e.NexusStatus)
	}
	return fmt.Sprintf("%s (nexus_request_id=%s status=%s)", ErrNexusNotApproved.Error(), e.NexusRequestID, e.NexusStatus)
}

// Unwrap permite errors.Is(err, ErrNexusNotApproved).
func (e *NexusBlockedError) Unwrap() error {
	return ErrNexusNotApproved
}

// IsNexusNotApproved indica que la operación está bloqueada por Nexus.
func IsNexusNotApproved(err error) bool {
	return errors.Is(err, ErrNexusNotApproved)
}

// AsNexusBlocked devuelve el detalle estructurado si el error es un bloqueo
// por Nexus. Útil para que el handler construya el body con context.
func AsNexusBlocked(err error) (*NexusBlockedError, bool) {
	var blocked *NexusBlockedError
	if errors.As(err, &blocked) {
		return blocked, true
	}
	return nil, false
}
