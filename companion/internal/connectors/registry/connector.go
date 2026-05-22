package registry

import (
	"context"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// Connector interfaz que implementa cada conector a sistema externo.
type Connector interface {
	// ID identificador único del conector.
	ID() string
	// Kind tipo de conector (pymes, whatsapp, mock, etc.).
	Kind() string
	// Capabilities lista operaciones y su contrato v1 (mode, risk, governance, schema, evidence).
	Capabilities() []domain.Capability
	// Validate verifica que la spec es válida para este conector.
	Validate(spec domain.ExecutionSpec) error
	// Execute ejecuta la operación. Solo debe llamarse tras aprobación de Governance.
	Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error)
}

// Refresher es opcional: connectors que descubren su catálogo dinámicamente
// (ej: PontiConnector que pega a /api/v1/capabilities) lo implementan para
// soportar refresh manual on-demand. Los que no lo implementan son no-op.
type Refresher interface {
	Refresh(ctx context.Context) error
}

// Registry registro de conectores disponibles.
type Registry struct {
	connectors map[string]Connector
}

// NewRegistry crea un nuevo registro.
func NewRegistry() *Registry {
	return &Registry{connectors: make(map[string]Connector)}
}

// Register registra un conector.
func (r *Registry) Register(c Connector) {
	r.connectors[c.ID()] = c
}

// Get obtiene un conector por ID.
func (r *Registry) Get(id string) (Connector, bool) {
	c, ok := r.connectors[id]
	return c, ok
}

// List lista todos los conectores registrados.
func (r *Registry) List() []Connector {
	out := make([]Connector, 0, len(r.connectors))
	for _, c := range r.connectors {
		out = append(out, c)
	}
	return out
}

// RefreshResult reporta el resultado por connector de un refresh batch.
// Empty Error indica éxito; conectores que no implementan Refresher se
// omiten.
type RefreshResult struct {
	ConnectorID string
	Refreshed   bool
	Error       string
}

// Refresh invoca Refresh() en cada connector que implementa Refresher.
// Connectors estáticos (mock, pymes con manifest hardcoded) se ignoran.
// El error agregado nunca se devuelve — los fallos individuales van en la
// lista para que el caller decida qué hacer (ej: reportar al admin).
func (r *Registry) Refresh(ctx context.Context) []RefreshResult {
	out := make([]RefreshResult, 0, len(r.connectors))
	for id, c := range r.connectors {
		ref, ok := c.(Refresher)
		if !ok {
			continue
		}
		err := ref.Refresh(ctx)
		res := RefreshResult{ConnectorID: id, Refreshed: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		out = append(out, res)
	}
	return out
}
