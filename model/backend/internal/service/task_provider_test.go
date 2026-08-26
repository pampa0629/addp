package service

import (
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/taskprovider"
)

func TestModelTaskProviderDeclaration(t *testing.T) {
	declaration, err := ModelTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, err := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if err != nil {
		t.Fatal(err)
	}
	for _, taskType := range []string{commonExecution.TaskTypeMaterializationPrepare, commonExecution.TaskTypeMaterializationPublish} {
		if capabilities.CapabilityFor(taskType) == nil {
			t.Fatalf("missing capability %q", taskType)
		}
	}
}

func TestMaterializationMarkerBindsLogicalTableAndFingerprint(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	marker := materializationMarker(7, fingerprint, "batch-1")
	if !materializationMarkerMatchesOwner(marker, 7, fingerprint) {
		t.Fatal("marker does not match its owner")
	}
	if materializationMarkerMatchesOwner(marker, 8, fingerprint) {
		t.Fatal("marker matched a different logical table")
	}
	if materializationMarkerMatchesOwner(marker, 7, strings.Repeat("b", 64)) {
		t.Fatal("marker matched a different schema fingerprint")
	}
}

func TestMaterializationExecutionContractsHaveNoRequiredInputs(t *testing.T) {
	for _, taskType := range []string{commonExecution.TaskTypeMaterializationPrepare, commonExecution.TaskTypeMaterializationPublish} {
		contract := materializationExecutionContract(taskType)
		raw := map[string]interface{}{
			"input_schema": contract.InputSchema, "input_defaults": contract.InputDefaults,
			"input_ui_schema": contract.InputUISchema, "output_schema": contract.OutputSchema,
		}
		if err := taskprovider.ValidateExecutionContract(raw); err != nil {
			t.Fatalf("%s contract is invalid: %v", taskType, err)
		}
	}
}
