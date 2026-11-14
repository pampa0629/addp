package service

import (
	"encoding/json"
	commonmodels "github.com/addp/common/models"
	"os"
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestDevelopTaskProviderDeclaration(t *testing.T) {
	declaration, err := DevelopTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if len(capabilities.TaskCapabilities) != 3 || capabilities.CapabilityFor("query") == nil || capabilities.CapabilityFor("workflow") == nil || capabilities.CapabilityFor("script") == nil {
		t.Fatalf("capabilities = %#v", capabilities.TaskCapabilities)
	}
}

func DevelopTaskProviderDeclaration() (*commonmodels.TaskProviderDeclaration, error) {
	data, err := os.ReadFile("../../../module-declaration.json")
	if err != nil {
		return nil, err
	}
	var artifact struct {
		TaskProvider *commonmodels.TaskProviderDeclaration `json:"task_provider"`
	}
	err = json.Unmarshal(data, &artifact)
	return artifact.TaskProvider, err
}
