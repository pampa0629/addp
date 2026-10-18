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
		graphProtectionOwner,
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
		if !validPolicyBinding(owner, action) {
			t.Fatalf("validPolicyBinding(%q, %q) = false", owner, action)
		}
		if validPolicyBinding(owner, action+"_other") {
			t.Fatalf("validPolicyBinding(%q, mismatched action) = true", owner)
		}
	}
	if !isRequiredOwner(graphProtectionOwner) || validPolicyBinding(graphProtectionOwner, graphReadAction) || validPolicyBinding(graphProtectionOwner, "") {
		t.Fatal("Graph must require installation without editable structured-field policies")
	}
	if isRequiredOwner("workbench") || validPolicyBinding("workbench", serviceExecuteAction) {
		t.Fatal("workbench must consume protected Service results instead of becoming a projection owner")
	}
	if isRequiredOwner("asset") || validPolicyBinding("asset", serviceExecuteAction) {
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

func TestPolicyScopeCatalogueIsDerivedAndImmutable(t *testing.T) {
	scopes := supportedPolicyScopes()
	if len(scopes) != 4 {
		t.Fatalf("policy scopes=%#v", scopes)
	}
	for _, scope := range scopes {
		if !validPolicyBinding(scope.ConsumerOwner, scope.Action) {
			t.Fatalf("unsupported scope %#v", scope)
		}
	}
	scopes[0].Action = "invalid"
	if supportedPolicyScopes()[0].Action == "invalid" {
		t.Fatal("caller mutated platform policy capability")
	}
}
