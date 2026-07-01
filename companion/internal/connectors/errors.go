package connectors

import (
	"errors"

	"github.com/devpablocristo/companion/internal/productlimits"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

var (
	ErrNotFound         = domainerr.NotFound("not found")
	ErrDisabled         = errors.New("connector is disabled")
	ErrUngated          = errors.New("execution requires nexus approval")
	ErrOperationUnknown = errors.New("unknown operation for connector")
	ErrInvalidPayload   = errors.New("invalid connector payload")
	ErrForbidden        = domainerr.Forbidden("connector access forbidden")
	ErrConflict         = domainerr.Conflict("connector execution conflict")
	ErrValidation       = domainerr.Validation("connector validation failed")
)

// IsNotFound verifica si el error es de conector no encontrado.
func IsNotFound(err error) bool {
	return domainerr.IsNotFound(err)
}

// IsUngated verifica si la ejecución no tiene aprobación de Nexus.
func IsUngated(err error) bool {
	return errors.Is(err, ErrUngated)
}

// IsInvalidPayload verifica si el payload no cumple el contrato de operación.
func IsInvalidPayload(err error) bool {
	return errors.Is(err, ErrInvalidPayload)
}

func IsForbidden(err error) bool {
	return domainerr.IsForbidden(err)
}

func IsValidation(err error) bool {
	return domainerr.IsValidation(err)
}

func IsRateLimited(err error) bool {
	return productlimits.IsRateLimited(err)
}

func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict) || domainerr.IsConflict(err)
}
