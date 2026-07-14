package planner

import "testing"

func TestParsePostgreSQLCDCTaskSpecAcceptsFrozenV1Contract(t *testing.T) {
	spec, err := ParsePostgreSQLCDCTaskSpec(validPostgreSQLCDCConfig())
	if err != nil {
		t.Fatalf("ParsePostgreSQLCDCTaskSpec() error = %v", err)
	}
	if spec.Load.ChangeDetection.Bootstrap != "initial_snapshot" {
		t.Fatalf("bootstrap = %q", spec.Load.ChangeDetection.Bootstrap)
	}
	sourceKeys, targetKeys, err := PostgreSQLCDCSourceToTargetKeys(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceKeys) != 1 || sourceKeys[0] != "id" || targetKeys[0] != "id" {
		t.Fatalf("keys = %v -> %v", sourceKeys, targetKeys)
	}
}

func TestParsePostgreSQLCDCTaskSpecRejectsNonInitialBootstrap(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	config["load"].(map[string]interface{})["change_detection"].(map[string]interface{})["bootstrap"] = "never"
	if _, err := ParsePostgreSQLCDCTaskSpec(config); err == nil {
		t.Fatal("expected non-initial bootstrap to be rejected")
	}
}

func TestParsePostgreSQLCDCTaskSpecRejectsUnknownRuntimeResourceFields(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	config["connector_name"] = "user-controlled"
	if _, err := ParsePostgreSQLCDCTaskSpec(config); err == nil {
		t.Fatal("expected connector_name to be rejected")
	}
}

func validPostgreSQLCDCConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous"},
		"load": map[string]interface{}{
			"mode":             "incremental",
			"change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"},
		},
		"source": map[string]interface{}{
			"locator": "addp://engine/12/path/public/orders?type=table", "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/20/path/public?type=schema", "name": "orders_cdc",
			"data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{map[string]interface{}{
				"source": "id", "target": "id", "target_type": "bigint", "nullable": false,
			}},
		}},
	}
}
