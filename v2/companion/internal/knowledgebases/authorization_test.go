package knowledgebases

import (
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

func TestAuthorizeReadAllowsOrganizationMembers(t *testing.T) {
	for _, role := range []string{"owner", "admin", "member", " MEMBER "} {
		t.Run(role, func(t *testing.T) {
			organization, err := authorizeRead(" organization-a ", role)
			if err != nil {
				t.Fatalf("expected read access for %q, got %v", role, err)
			}
			if organization != "organization-a" {
				t.Fatalf("expected normalized organization, got %q", organization)
			}
		})
	}
}

func TestAuthorizeReadFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name         string
		organization string
		role         string
		validation   bool
	}{
		{name: "missing organization", role: "member", validation: true},
		{name: "missing role", organization: "organization-a"},
		{name: "unknown role", organization: "organization-a", role: "viewer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authorizeRead(tc.organization, tc.role)
			if tc.validation {
				if !domainerr.IsKind(err, domainerr.KindValidation) {
					t.Fatalf("expected validation error, got %v", err)
				}
				return
			}
			if !domainerr.IsForbidden(err) {
				t.Fatalf("expected forbidden error, got %v", err)
			}
		})
	}
}

func TestAuthorizeManagementStillRejectsMembers(t *testing.T) {
	if _, err := authorize("organization-a", "member"); !domainerr.IsForbidden(err) {
		t.Fatalf("expected member management to remain forbidden, got %v", err)
	}
}
