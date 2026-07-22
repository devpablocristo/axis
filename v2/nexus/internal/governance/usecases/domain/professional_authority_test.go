package domain

import (
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

func TestNormalizeCheckInputRequiresDelegationMetadataWhenAuthorityRequiresIt(t *testing.T) {
	base := CheckInput{
		RequesterID: "virployee-a", ActionType: "records.update", BindingHash: "binding-a",
		AuthorityBindingHash: "authority-a", PolicyRevisionHash: "policies-a",
		DelegationRequired: true,
	}
	if _, err := NormalizeCheckInput(base); !domainerr.IsValidation(err) {
		t.Fatalf("missing delegation must fail closed, got %v", err)
	}
	base.DelegationID = "delegation-a"
	base.DelegationRevision = 3
	out, err := NormalizeCheckInput(base)
	if err != nil {
		t.Fatal(err)
	}
	if !out.DelegationRequired || out.DelegationID != "delegation-a" || out.DelegationRevision != 3 {
		t.Fatalf("delegation binding was lost: %+v", out)
	}
}

func TestNormalizeCheckInputRejectsUnboundAuthorityMetadata(t *testing.T) {
	_, err := NormalizeCheckInput(CheckInput{
		RequesterID: "virployee-a", ActionType: "records.update",
		DelegationID: "delegation-a", DelegationRevision: 1,
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("unbound delegation metadata must be rejected, got %v", err)
	}
}
