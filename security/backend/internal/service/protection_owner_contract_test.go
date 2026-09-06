package service

import (
	"reflect"
	"testing"
)

func TestRequiredProtectionOwnerContractIsCompleteAndStable(t *testing.T) {
	wantOwners := []string{
		developProtectionOwner,
		managerProtectionOwner,
		serviceProtectionOwner,
		transferProtectionOwner,
	}
	if owners := allRequiredProtectionOwners(); !reflect.DeepEqual(owners, wantOwners) {
		t.Fatalf("allRequiredProtectionOwners() = %#v, want %#v", owners, wantOwners)
	}

	wantActions := map[string]string{
		developProtectionOwner:  developQueryAction,
		managerProtectionOwner:  managerPreviewAction,
		serviceProtectionOwner:  serviceExecuteAction,
		transferProtectionOwner: transferExportAction,
	}
	for owner, action := range wantActions {
		if !isRequiredOwner(owner) {
			t.Fatalf("isRequiredOwner(%q) = false", owner)
		}
		if !validExemptionBinding(owner, action) {
			t.Fatalf("validExemptionBinding(%q, %q) = false", owner, action)
		}
		if validExemptionBinding(owner, action+"_other") {
			t.Fatalf("validExemptionBinding(%q, mismatched action) = true", owner)
		}
	}
	if isRequiredOwner("workbench") || validExemptionBinding("workbench", serviceExecuteAction) {
		t.Fatal("workbench must consume protected Service results instead of becoming a projection owner")
	}
	if isRequiredOwner("asset") || validExemptionBinding("asset", serviceExecuteAction) {
		t.Fatal("asset must not become a data-plane projection owner")
	}
}

func TestRequiredProtectionOwnerNamesCannotMutateContract(t *testing.T) {
	owners := allRequiredProtectionOwners()
	owners[0] = "mutated"
	if got := allRequiredProtectionOwners()[0]; got != developProtectionOwner {
		t.Fatalf("owner contract was mutated through returned slice: %q", got)
	}
}
