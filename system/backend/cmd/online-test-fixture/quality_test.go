package main

import "testing"

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
