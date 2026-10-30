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

func TestGraphProtectionExecutionPrivateEnvironmentActuallyExportsEveryIdentity(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	data, err := os.ReadFile(filepath.Join(root, "scripts/test/execution-online-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Keys []string `json:"fixture_environment_keys"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.Keys) < 20 {
		t.Fatal("execution identity export contract is incomplete")
	}
	values := map[string]string{"UNRELATED_SECRET": "must-not-export"}
	for _, key := range contract.Keys {
		values[key] = "private-'" + key
	}
	output := filepath.Join(t.TempDir(), "identity.env")
	if err := writeEnvironmentFile(output, values); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("identity output is not private")
	}
	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != len(contract.Keys) {
		t.Fatal("identity export silently omitted or added fields")
	}
	for _, key := range contract.Keys {
		expected := "export " + key + "='private-'\"'\"'" + key + "'"
		if !slices.Contains(lines, expected) {
			t.Fatalf("private identity not exported safely: %s", key)
		}
	}
	if strings.Contains(string(body), "must-not-export") {
		t.Fatal("unregistered secret escaped the allowlist")
	}
}

func TestGraphProtectionNativeCleanupUsersKeepApprovedRolesSeparate(t *testing.T) {
	permissions, err := suitePermissions("cleanup-native-effects")
	if err != nil || !slices.Equal(permissions, managerArtifactPermissions) || needsEngineProvisioner("cleanup-native-effects") {
		t.Fatal("native preparation must reuse Manager's ordinary role, without broad Engine provisioner")
	}
	want := []string{"system.engine.create", "system.engine.read", "system.engine.delete", "system.cleanup.read", "manager.cleanup_result.read", "transfer.cleanup_result.read"}
	if !slices.Equal(cleanupNativeEnginePermissions, want) {
		t.Fatal("Engine lifecycle User permissions changed")
	}
	if !slices.Equal(suiteSourceInitializerPermissions("cleanup-native-effects"), sourceInitializerPermissions) {
		t.Fatal("native source preparation must reuse existing precise authorization initializer")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	data, err := os.ReadFile(filepath.Join(root, "scripts/test/execution-online-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Permissions map[string][]string `json:"native_permissions"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string][]string{"manager": managerArtifactPermissions, "transfer": transferContinuousPermissions, "engine": cleanupNativeEnginePermissions} {
		if !slices.Equal(contract.Permissions[name], expected) {
			t.Fatalf("%s Python/Go identity contract differs", name)
		}
	}
}
