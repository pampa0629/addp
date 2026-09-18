package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonauthorization "github.com/addp/common/authorization"
)

func TestFixtureRolesKeepSystemEngineControlPlaneOutOfConsumerIdentity(t *testing.T) {
	if engineProvisionerRoleKey != "tenant.infrastructure_administrator" {
		t.Fatalf("engine provisioner role = %q", engineProvisionerRoleKey)
	}
	if !contains(consumerPermissions, "system.execution_authorization.create") {
		t.Fatal("consumer role must be able to derive a scoped execution authorization")
	}
	for _, permission := range consumerPermissions {
		if strings.HasPrefix(permission, "system.engine.") {
			t.Fatalf("consumer role must not access the System Engine control plane: %s", permission)
		}
	}
}

func TestFixtureAuthorizationContractMatchesPublishedCatalog(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	catalog, err := commonauthorization.LoadRepositoryAuthorizationCatalog(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	permissions := make(map[string]commonauthorization.PermissionDescriptor, len(catalog.Permissions))
	for _, permission := range catalog.Permissions {
		permissions[permission.Key] = permission
	}
	for _, key := range append(append(append([]string{}, consumerPermissions...), metricPermissions...), ontologyPermissions...) {
		permission, exists := permissions[key]
		if !exists {
			t.Fatalf("consumer permission %q is not published", key)
		}
		if permission.Status != "active" || !permission.TenantCustomizable || !contains(permission.AllowedScopeTypes, "tenant") {
			t.Fatalf("consumer permission %q must remain active and tenant-customizable at tenant scope", key)
		}
	}

	for _, role := range catalog.Roles {
		if role.Key != engineProvisionerRoleKey {
			continue
		}
		if role.RoleType != "tenant_builtin" || !contains(role.AllowedScopeTypes, "tenant") || !contains(role.AllowedPrincipalTypes, "user") {
			t.Fatalf("engine provisioner role has incompatible assignment contract: %#v", role)
		}
		for _, required := range []string{"system.engine.create", "system.engine.execute", "system.engine.read"} {
			if !contains(role.Permissions, required) {
				t.Fatalf("engine provisioner role is missing %q", required)
			}
		}
		return
	}
	t.Fatalf("engine provisioner role %q is not published", engineProvisionerRoleKey)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestValidateExternalEnvironment(t *testing.T) {
	valid := []string{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=1"}
	if err := validateExternalEnvironment(valid, filepath.Join(t.TempDir(), "fixture.env")); err != nil {
		t.Fatal(err)
	}
	ownerManaged := []string{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_OWNER_MANAGED=1"}
	if err := validateExternalEnvironment(ownerManaged, filepath.Join(t.TempDir(), "fixture.env")); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]string{
		{"GITHUB_ACTIONS=false", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=1"},
		{"GITHUB_ACTIONS=true", "RUNNER_OS=macOS", "ADDP_ONLINE_HOSTED=1"},
		{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=0"},
	} {
		if err := validateExternalEnvironment(invalid, filepath.Join(t.TempDir(), "fixture.env")); err == nil {
			t.Fatalf("environment %#v was accepted", invalid)
		}
	}
	both := []string{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=1", "ADDP_ONLINE_OWNER_MANAGED=1"}
	if err := validateExternalEnvironment(both, filepath.Join(t.TempDir(), "fixture.env")); err == nil {
		t.Fatal("multiple external profiles were accepted")
	}
	if err := validateExternalEnvironment(valid, "relative.env"); err == nil {
		t.Fatal("relative output path was accepted")
	}
}

func TestWriteEnvironmentFileUsesOwnerOnlyPermissionsAndShellQuoting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.env")
	values := map[string]string{
		"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "addp_at_engine",
		"ADDP_ONLINE_TEST_TENANT_ID":              "42",
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN":      "addp_at_user'quoted",
	}
	if err := writeEnvironmentFile(path, values); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "addp_at_user'\"'\"'quoted") {
		t.Fatalf("fixture environment is not shell quoted: %s", content)
	}
	if strings.Contains(string(content), "password") {
		t.Fatalf("fixture environment leaked a password field: %s", content)
	}
}

func TestSuitePermissionsAreExplicitAndSeparate(t *testing.T) {
	ontology, err := suitePermissions("ontology-revision-lifecycle")
	if err != nil || len(ontology) != 4 || needsEngineProvisioner("ontology-revision-lifecycle") {
		t.Fatalf("ontology requires exactly four permissions and no Engine provisioner: %v, %v", ontology, err)
	}
	for _, key := range []string{"ontology.revision.read", "ontology.revision.update", "ontology.revision.publish", "system.execution_authorization.create"} {
		if !contains(ontology, key) {
			t.Fatalf("missing ontology permission %s", key)
		}
	}
	for _, suite := range []string{"opengauss-consumer-flow", "kingbase-consumer-flow", "metric-service-revision-lifecycle"} {
		if !needsEngineProvisioner(suite) {
			t.Fatalf("Engine suite %s lost its provisioner", suite)
		}
	}
	if _, err := suitePermissions(""); err == nil {
		t.Fatal("missing suite accepted")
	}
	if _, err := suitePermissions("unknown"); err == nil {
		t.Fatal("unknown suite accepted")
	}
	metric, err := suitePermissions("metric-service-revision-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"model.metric_implementation.offline", "service.definition.update", "standard.metric.publish"} {
		if !contains(metric, required) {
			t.Fatalf("metric role missing %s", required)
		}
		if contains(consumerPermissions, required) {
			t.Fatalf("relational role gained %s", required)
		}
	}
	for _, key := range metric {
		if strings.HasPrefix(key, "system.engine.") || key == "model.metric_implementation.delete" {
			t.Fatalf("metric role has unnecessary destructive/control-plane permission %s", key)
		}
	}
}

func TestOntologyEnvironmentDoesNotExportAnEngineCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ontology.env")
	if err := writeEnvironmentFile(path, map[string]string{
		"ADDP_ONLINE_TEST_TENANT_ID": "42", "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "addp_at_fixture",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ENGINE") {
		t.Fatal("Ontology exported an unnecessary Engine credential")
	}
}
