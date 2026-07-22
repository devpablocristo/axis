package virployees

import (
	"encoding/json"
	"strings"

	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
)

func professionalContextFromJobRole(role jobroledomain.JobRole) ProfessionalContext {
	return ProfessionalContext{
		JobRoleID:        role.ID.String(),
		Name:             strings.TrimSpace(role.Name),
		Mission:          strings.TrimSpace(role.Mission),
		Responsibilities: append([]jobroledomain.Responsibility(nil), role.Responsibilities...),
		SuccessCriteria:  append([]jobroledomain.SuccessCriterion(nil), role.SuccessCriteria...),
	}
}

func professionalContextHash(context ProfessionalContext) string {
	raw, _ := json.Marshal(context)
	return runtraces.HashString(string(raw))
}
