package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFixtureRolesKeepEngineProvisioningOutOfConsumerIdentity(t *testing.T) {
	wantProvisioner := []string{
		"system.engine.create",
		"system.engine.execute",
		"system.engine.read",
	}
	if !reflect.DeepEqual(engineProvisionerPermissions, wantProvisioner) {
		t.Fatalf("engine provisioner permissions = %#v, want %#v", engineProvisionerPermissions, wantProvisioner)
	}
	for _, permission := range consumerPermissions {
		if permission == "system.engine.create" || permission == "system.engine.execute" {
			t.Fatalf("consumer role must not provision engines: %s", permission)
		}
	}
}

func TestValidateHostedEnvironment(t *testing.T) {
	valid := []string{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=1"}
	if err := validateHostedEnvironment(valid, filepath.Join(t.TempDir(), "fixture.env")); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]string{
		{"GITHUB_ACTIONS=false", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=1"},
		{"GITHUB_ACTIONS=true", "RUNNER_OS=macOS", "ADDP_ONLINE_HOSTED=1"},
		{"GITHUB_ACTIONS=true", "RUNNER_OS=Linux", "ADDP_ONLINE_HOSTED=0"},
	} {
		if err := validateHostedEnvironment(invalid, filepath.Join(t.TempDir(), "fixture.env")); err == nil {
			t.Fatalf("environment %#v was accepted", invalid)
		}
	}
	if err := validateHostedEnvironment(valid, "relative.env"); err == nil {
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
