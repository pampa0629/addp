package planner

import (
	"testing"

	"github.com/addp/common/engine/plugins/mysql"
	"github.com/addp/common/engine/plugins/postgresql"
)

func TestParseDatabaseCDCTaskSpecAcceptsFrozenV1Contract(t *testing.T) {
	spec, err := ParseDatabaseCDCTaskSpec(validPostgreSQLCDCConfig())
	if err != nil {
		t.Fatalf("ParseDatabaseCDCTaskSpec() error = %v", err)
	}
	if spec.Load.ChangeDetection.Bootstrap != "initial_snapshot" {
		t.Fatalf("bootstrap = %q", spec.Load.ChangeDetection.Bootstrap)
	}
	sourceKeys, targetKeys, err := DatabaseCDCSourceToTargetKeys(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceKeys) != 1 || sourceKeys[0] != "id" || targetKeys[0] != "id" {
		t.Fatalf("keys = %v -> %v", sourceKeys, targetKeys)
	}
}

func TestParseDatabaseCDCTaskSpecRejectsNonInitialBootstrap(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	config["load"].(map[string]interface{})["change_detection"].(map[string]interface{})["bootstrap"] = "never"
	if _, err := ParseDatabaseCDCTaskSpec(config); err == nil {
		t.Fatal("expected non-initial bootstrap to be rejected")
	}
}

func TestParseDatabaseCDCTaskSpecRejectsUnknownRuntimeResourceFields(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	config["connector_name"] = "user-controlled"
	if _, err := ParseDatabaseCDCTaskSpec(config); err == nil {
		t.Fatal("expected connector_name to be rejected")
	}
}

func TestParseDatabaseCDCTaskSpecRequiresBlockingRecordFailureMode(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	config["runtime"].(map[string]interface{})["record_failure"] = map[string]interface{}{"mode": "dead_letter"}
	if _, err := ParseDatabaseCDCTaskSpec(config); err == nil {
		t.Fatal("PostgreSQL CDC accepted dead_letter mode")
	}
}

func TestDatabaseCDCProviderComesFromResolvedSourceEngine(t *testing.T) {
	spec, err := ParseDatabaseCDCTaskSpec(validPostgreSQLCDCConfig())
	if err != nil {
		t.Fatal(err)
	}
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	bindings, err := ResolveDatabaseCDCBindings(spec, StaticEngineResolver{
		12: {Type: "mysql", EngineID: 12},
		20: {Type: "postgresql", EngineID: 20, Capabilities: &targetCaps},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bindings.SourceType != "mysql" {
		t.Fatalf("source type = %q, want mysql", bindings.SourceType)
	}
}

func TestResolveDatabaseCDCBindingsAcceptsMySQLAtomicTarget(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	config["target"].(map[string]interface{})["parent_locator"] = "addp://engine/20/path/business?type=database"
	spec, err := ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		t.Fatal(err)
	}
	targetCaps := (&mysql.MySQLPlugin{}).Capabilities()
	bindings, err := ResolveDatabaseCDCBindings(spec, StaticEngineResolver{
		12: {Type: "postgresql", EngineID: 12},
		20: {Type: "mysql", EngineID: 20, Capabilities: &targetCaps},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bindings.TargetType != "mysql" {
		t.Fatalf("target type = %q, want mysql", bindings.TargetType)
	}
}

func validPostgreSQLCDCConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
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
