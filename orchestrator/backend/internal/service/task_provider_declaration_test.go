package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestOrchestratorTaskProviderDeclaration(t *testing.T) {
	declaration, err := OrchestratorTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	capability := capabilities.CapabilityFor("orchestration")
	if capability == nil || !capability.SupportsSchedule {
		t.Fatalf("capability = %#v", capability)
	}
}
