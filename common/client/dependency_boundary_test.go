package client

import (
	"os/exec"
	"strings"
	"testing"
)

// HTTP wire contracts must not register drivers or import content parsers.
// Check the complete production graph, not only this package's direct imports.
func TestClientDependencyBoundary(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps", "github.com/addp/common/client").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect client dependency graph: %v\n%s", err, output)
	}
	foundFacts := false
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "github.com/addp/common/dataitem/authorizationfacts" {
			foundFacts = true
		}
		if dependency == "github.com/addp/common/dataitem" ||
			dependency == "github.com/addp/common/format" || strings.HasPrefix(dependency, "github.com/addp/common/format/") ||
			strings.HasPrefix(dependency, "github.com/addp/common/engine/plugins/") {
			t.Errorf("HTTP client imports content implementation: %s", dependency)
		}
	}
	if !foundFacts {
		t.Fatal("the canonical authorization facts wire contract is missing")
	}
}
