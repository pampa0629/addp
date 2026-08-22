package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestMetaTaskProviderDeclaration(t *testing.T) {
	declaration, err := TaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if capabilities.CapabilityFor("scan") == nil {
		t.Fatalf("capabilities = %#v", capabilities.TaskCapabilities)
	}
}
