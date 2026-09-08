package service

import (
	"strings"
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
	for _, taskType := range []string{"vector_tile_set_generation", "raster_mosaic_generation"} {
		capability := capabilities.CapabilityFor(taskType)
		if capability == nil || !strings.HasPrefix(capability.CreateURL, "/manager/derived-tasks?") {
			t.Fatalf("%s create_url = %q, want unified derived task route", taskType, capability.CreateURL)
		}
	}
}
