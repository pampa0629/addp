package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestQualityPreparationCredentialReachesPrivateEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.env")
	values := map[string]string{
		"ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN": "private-preparer'quoted",
		"ADDP_ONLINE_TEST_USER_ACCESS_TOKEN":       "private-executor",
		"ADDP_ONLINE_READ_USER_ACCESS_TOKEN":       "private-monitor-reader",
		"ADDP_ONLINE_TEST_TENANT_ID":               "2",
	}
	if err := writeEnvironmentFile(path, values); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("identity environment must remain owner-only: %v", err)
	}
	// Exercise the shell handoff, including quoting and export, rather than only
	// checking that the preparer credential was created in the in-memory map.
	command := exec.Command("bash", "-c", `source "$1"; shift; exec bash -c 'test "$ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN" = "$1" && test "$ADDP_ONLINE_TEST_USER_ACCESS_TOKEN" = "$2" && test "$ADDP_ONLINE_READ_USER_ACCESS_TOKEN" = "$3" && test "$ADDP_ONLINE_TEST_TENANT_ID" = 2' _ "$@"`, "_", path,
		values["ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN"], values["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], values["ADDP_ONLINE_READ_USER_ACCESS_TOKEN"])
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := command.Run(); err != nil {
		t.Fatal("Quality identities did not survive the private shell environment handoff")
	}
}

func TestQualityIdentitySeparatesPreparationFromExecution(t *testing.T) {
	permissions, err := suitePermissions("quality-dynamic-binding")
	if err != nil || !needsEngineProvisioner("quality-dynamic-binding") {
		t.Fatalf("Quality Hosted identity profile missing: %v", err)
	}
	sets := []struct {
		name        string
		permissions []string
		allowed     []string
		forbidden   []string
	}{
		{"execution", permissions,
			[]string{"model.materialization.execute", "quality.plan.execute", "monitor.execution.read", "develop.data_read.execute", "develop.data_ddl.execute"},
			[]string{"model.logical_model.create", "model.logical_model.update", "model.dw_layer.create", "meta.scan_task.execute", "system.engine.create", "system.engine_access_grant.create"}},
		{"preparation", qualityPreparationPermissions,
			[]string{"model.logical_model.create", "model.logical_model.update", "model.dw_layer.create", "meta.scan_task.execute", "model.materialization.execute"},
			[]string{"quality.plan.execute", "quality.plan.create", "orchestrator.workflow.execute", "system.engine.create", "system.engine_access_grant.create"}},
	}
	for _, set := range sets {
		t.Run(set.name, func(t *testing.T) {
			seen := map[string]bool{}
			for _, key := range set.permissions {
				if seen[key] {
					t.Fatalf("duplicate permission %s", key)
				}
				seen[key] = true
			}
			for _, key := range set.allowed {
				if !seen[key] {
					t.Errorf("missing permission %s", key)
				}
			}
			for _, key := range set.forbidden {
				if seen[key] {
					t.Errorf("unexpected permission %s", key)
				}
			}
		})
	}
}
