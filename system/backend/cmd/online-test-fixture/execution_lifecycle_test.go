package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestGraphProtectionExecutionFixtureRolesRemainNarrow(t *testing.T) {
	if !slices.Contains(graphExecutionPermissions, "graph.graph.reconcile") || slices.Contains(graphProtectionPreparerPermissions, "graph.graph.reconcile") || slices.Contains(graphProtectionReaderPermissions, "graph.graph.reconcile") {
		t.Fatal("reconcile must be restricted to the separate lifecycle fixture User")
	}
	permissions, err := suitePermissions("cleanup-execution")
	if err != nil || !slices.Equal(permissions, cleanupExecutionPermissions) || len(permissions) != 12 || needsEngineProvisioner("cleanup-execution") {
		t.Fatalf("Cleanup must have exactly two public capabilities and ten private result reads: %v %v", permissions, err)
	}
	for _, p := range append(append([]string{}, graphExecutionPermissions...), permissions...) {
		if strings.Contains(p, "authorization.execute") || strings.Contains(p, "role.") || strings.Contains(p, "tenant.") || strings.Contains(p, "engine.") {
			t.Fatalf("fixture User obtained runtime, administration or Engine authority: %s", p)
		}
	}
	permissions[0] = "mutated"
	fresh, _ := suitePermissions("cleanup-execution")
	if fresh[0] != "system.cleanup.read" {
		t.Fatal("suite permission slice aliases the shared contract")
	}
}

func TestGraphProtectionExecutionRunnerIdentityContractsMatchFixture(t *testing.T) {
	// Read the machine contract beside the Python runners instead of duplicating
	// their expected permission arrays in this cross-language assertion.
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	data, err := os.ReadFile(filepath.Join(root, "scripts/test/execution-online-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		GraphPermissions   []string `json:"graph_permissions"`
		CleanupPermissions []string `json:"cleanup_permissions"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2][]string{"graph": {contract.GraphPermissions, graphExecutionPermissions}, "cleanup": {contract.CleanupPermissions, cleanupExecutionPermissions}} {
		left, right := slices.Clone(pair[0]), slices.Clone(pair[1])
		slices.Sort(left)
		slices.Sort(right)
		if !slices.Equal(left, right) {
			t.Fatalf("%s runner identity differs from prepared role", name)
		}
	}
}
