package tasks

import (
	"net/http/httptest"
	"testing"

	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

func TestCanAccessTaskOrg_DeniesBlankTaskOrgForTenantPrincipal(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/v1/tasks/task-1", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", scopeCompanionTasksRead)
	req.Header.Set("X-Org-ID", "org-a")

	if canAccessTaskOrg(req, domain.Task{}) {
		t.Fatal("expected tenant principal to be denied access to blank-org task")
	}
}

func TestCanAccessTaskOrg_AllowsMatchingTenant(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/v1/tasks/task-1", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", scopeCompanionTasksRead)
	req.Header.Set("X-Org-ID", "org-a")

	if !canAccessTaskOrg(req, domain.Task{OrgID: "org-a"}) {
		t.Fatal("expected tenant principal to access matching task org")
	}
}
