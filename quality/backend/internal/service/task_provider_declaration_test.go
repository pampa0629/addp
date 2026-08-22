package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestQualityTaskProviderDeclaration(t *testing.T) {
	declaration, err := QualityTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if capabilities.CapabilityFor("check") == nil {
		t.Fatalf("capabilities = %#v", capabilities.TaskCapabilities)
	}
}
