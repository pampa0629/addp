package service

import (
	"testing"

	"github.com/addp/common/taskprovider"
)

func TestManagerTaskProviderDeclaration(t *testing.T) {
	declaration, err := ManagerTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, _ := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	for _, taskType := range []string{"vector_tile_set_generation", "vector_tile_cache_generation", "embedding"} {
		if capabilities.CapabilityFor(taskType) == nil {
			t.Fatalf("missing task type %s", taskType)
		}
	}
}
