package memory

import (
	"errors"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

var (
	ErrNotFound        = domainerr.NotFound("not found")
	ErrVersionConflict = errors.New("memory version conflict")
	// ErrQuotaExceeded indica que el scope (org/user/task) alcanzó el tope
	// de entradas vivas. Se devuelve solo en path de inserción (los updates
	// no consumen quota).
	ErrQuotaExceeded   = domainerr.Conflict("memory quota exceeded for scope")
	ErrMemoryConflict  = domainerr.Conflict("memory conflict requires review or supersession")
	ErrMemoryPoisoning = domainerr.Validation("memory input rejected by poisoning detector")
)

// IsNotFound verifica si el error es de entrada no encontrada.
func IsNotFound(err error) bool {
	return domainerr.IsNotFound(err)
}

// IsVersionConflict verifica si el error es de conflicto de versión.
func IsVersionConflict(err error) bool {
	return errors.Is(err, ErrVersionConflict)
}

// IsQuotaExceeded verifica si el error es de quota excedida.
func IsQuotaExceeded(err error) bool {
	return errors.Is(err, ErrQuotaExceeded)
}

func IsMemoryConflict(err error) bool {
	return errors.Is(err, ErrMemoryConflict)
}

func IsMemoryPoisoning(err error) bool {
	return errors.Is(err, ErrMemoryPoisoning)
}

func IsForbidden(err error) bool {
	return domainerr.IsForbidden(err)
}

func IsValidation(err error) bool {
	return domainerr.IsValidation(err)
}
