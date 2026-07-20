package learning

import (
	"context"
	"strings"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/google/uuid"
)

// MemoryInstaller installs an accepted proposal as a procedure memory. It is
// the ONLY way learning writes to memory, and it is only reached from the human
// Accept path — the golden rule (G4.3): the agent never self-writes procedures.
type MemoryInstaller interface {
	InstallProcedure(ctx context.Context, tenantID string, virployeeID uuid.UUID, actor, source, title, content string) (uuid.UUID, error)
}

// Authorizer enforces the same per-virployee role gate the human memory-write
// paths use. Accept/Dismiss install into (or discard for) a specific virployee,
// so they must be gated exactly as a human memory write for that virployee —
// otherwise CreateSystem would bypass the authorization every other write has.
// memories.UseCases satisfies this directly.
type Authorizer interface {
	Authorize(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string) error
}

// --- capabilities adapter (CapabilityChecker) ---

type capabilityLister interface {
	ListActive(ctx context.Context, tenantID string) ([]capabilitydomain.Capability, error)
}

type capabilitiesChecker struct {
	caps capabilityLister
}

// NewCapabilityChecker adapts the capabilities usecases (ListActive) to the
// eval's CapabilityChecker port.
func NewCapabilityChecker(caps capabilityLister) CapabilityChecker {
	return capabilitiesChecker{caps: caps}
}

func (c capabilitiesChecker) IsActiveCapability(ctx context.Context, tenantID, capabilityKey string) (bool, error) {
	key := strings.TrimSpace(strings.ToLower(capabilityKey))
	list, err := c.caps.ListActive(ctx, tenantID)
	if err != nil {
		return false, err
	}
	for _, capability := range list {
		if strings.ToLower(capability.CapabilityKey) == key {
			return true, nil
		}
	}
	return false, nil
}

// --- memories adapter (MemoryInstaller) ---

type memoryWriter interface {
	CreateSystem(ctx context.Context, tenant string, virployee uuid.UUID, actor, source string, in memories.CreateInput) (memories.Memory, error)
}

type memoriesInstaller struct {
	mem memoryWriter
}

// NewMemoriesInstaller adapts the memories usecases (CreateSystem) to the
// MemoryInstaller port, always writing type=procedure with provenance=system.
func NewMemoriesInstaller(mem memoryWriter) MemoryInstaller {
	return memoriesInstaller{mem: mem}
}

func (m memoriesInstaller) InstallProcedure(ctx context.Context, tenantID string, virployeeID uuid.UUID, actor, source, title, content string) (uuid.UUID, error) {
	created, err := m.mem.CreateSystem(ctx, tenantID, virployeeID, actor, source, memories.CreateInput{
		Title:   title,
		Type:    "procedure",
		Content: content,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return created.ID, nil
}
