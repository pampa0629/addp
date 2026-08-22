package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestGraphTaskProviderDeclaration(t *testing.T) {
	declaration, err := GraphTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if capabilities.CapabilityFor("kg_build") == nil {
		t.Fatalf("capabilities = %#v", capabilities.TaskCapabilities)
	}
}
