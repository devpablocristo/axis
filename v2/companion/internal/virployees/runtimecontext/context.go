package runtimecontext

import (
	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/memories"
	profiletemplatedomain "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

type Context struct {
	Virployee         virployeedomain.Virployee
	JobRole           jobroledomain.JobRole
	ProfileTemplate   profiletemplatedomain.ProfileTemplate
	Capabilities      []capabilitydomain.Capability
	MemoryReferences  []memories.Reference
	MemoryContext     []memories.ContextItem
	MemoryContextHash string
}
