package planner

import (
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	mysqlplugin "github.com/addp/common/engine/plugins/mysql"
	oceanbaseplugin "github.com/addp/common/engine/plugins/oceanbase"
	postgresqlplugin "github.com/addp/common/engine/plugins/postgresql"
)

func TestBuildWatermarkIncrementalPlanRequiresDeclaredCapabilities(t *testing.T) {
	spec := TableExportTaskSpec{
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load: LoadSpec{Mode: loadModeIncremental, ChangeDetection: &ChangeDetectionSpec{
			Type: changeTypeWatermark, Field: "updated_at", TieBreaker: []string{"id"}, Start: "committed", End: "execution_upper_bound",
		}},
		Source:    EndpointSpec{Locator: tableLocator(1, "public", "orders"), DataType: dataTypeTable, Representation: representationNative},
		Target:    EndpointSpec{ParentLocator: schemaLocator(2, "public"), Name: "orders", DataType: dataTypeTable, Representation: representationNative, Policy: map[string]interface{}{"apply_mode": "upsert", "keys": []string{"id"}}},
		BatchSize: 100,
	}
	pgCaps := engineplugin.EngineCapabilities{Storage: &engineplugin.StorageCapabilities{Store: &engineplugin.StoreCapability{
		BoundedWatermarkRead: true,
		TableUpsert:          &engineplugin.TableUpsertCapability{Supported: true, Idempotent: true},
	}}}
	result, err := BuildWatermarkIncrementalPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql", EngineID: 1, Capabilities: &pgCaps},
		2: {Type: "postgresql", EngineID: 2, Capabilities: &pgCaps},
	})
	if err != nil {
		t.Fatalf("BuildWatermarkIncrementalPlan failed: %v", err)
	}
	if result.Plan.WatermarkField != "updated_at" || len(result.Plan.TieBreakers) != 1 || result.Plan.TargetKeys[0] != "id" {
		t.Fatalf("plan = %#v", result.Plan)
	}
}

func TestBuildWatermarkIncrementalPlanAcceptsOceanBaseSourceByCapability(t *testing.T) {
	oceanBaseCapabilities := (&oceanbaseplugin.Plugin{}).Capabilities()
	mysqlCapabilities := (&mysqlplugin.MySQLPlugin{}).Capabilities()
	spec := TableExportTaskSpec{
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load: LoadSpec{Mode: loadModeIncremental, ChangeDetection: &ChangeDetectionSpec{
			Type: changeTypeWatermark, Field: "updated_at", TieBreaker: []string{"id"}, Start: "committed", End: "execution_upper_bound",
		}},
		Source: EndpointSpec{
			Locator: "addp://engine/1/path/business/orders?type=table", DataType: dataTypeTable, Representation: representationNative,
		},
		Target: EndpointSpec{
			ParentLocator: "addp://engine/2/path/business?type=database", Name: "orders", DataType: dataTypeTable,
			Representation: representationNative, Policy: map[string]interface{}{"apply_mode": "upsert", "keys": []string{"id"}},
		},
		BatchSize: 100,
	}

	result, err := BuildWatermarkIncrementalPlan(spec, StaticEngineResolver{
		1: {Type: "oceanbase", EngineID: 1, Capabilities: &oceanBaseCapabilities},
		2: {Type: "mysql", EngineID: 2, Capabilities: &mysqlCapabilities},
	})
	if err != nil {
		t.Fatalf("BuildWatermarkIncrementalPlan failed: %v", err)
	}
	if result.SourceEngineType != "oceanbase" || result.TargetEngineType != "mysql" {
		t.Fatalf("engine types = %s -> %s, want oceanbase -> mysql", result.SourceEngineType, result.TargetEngineType)
	}
}

func TestBuildWatermarkIncrementalPlanAcceptsMySQLSourceCapabilities(t *testing.T) {
	spec := TableExportTaskSpec{
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load: LoadSpec{Mode: loadModeIncremental, ChangeDetection: &ChangeDetectionSpec{
			Type: changeTypeWatermark, Field: "updated_at", TieBreaker: []string{"id"}, Start: "committed", End: "execution_upper_bound",
		}},
		Source:    EndpointSpec{Locator: tableLocator(1, "business", "orders"), DataType: dataTypeTable, Representation: representationNative},
		BatchSize: 100,
	}
	mysqlCapabilities := (&mysqlplugin.MySQLPlugin{}).Capabilities()
	postgresqlCapabilities := (&postgresqlplugin.PostgreSQLPlugin{}).Capabilities()
	tests := []struct {
		name       string
		target     EndpointSpec
		targetType string
		targetCaps *engineplugin.EngineCapabilities
	}{
		{
			name: "PostgreSQL target",
			target: EndpointSpec{
				ParentLocator: schemaLocator(2, "public"), Name: "orders", DataType: dataTypeTable,
				Representation: representationNative, Policy: map[string]interface{}{"apply_mode": "upsert", "keys": []string{"id"}},
			},
			targetType: "postgresql", targetCaps: &postgresqlCapabilities,
		},
		{
			name: "MySQL target",
			target: EndpointSpec{
				ParentLocator: "addp://engine/2/path/business?type=database", Name: "orders", DataType: dataTypeTable,
				Representation: representationNative, Policy: map[string]interface{}{"apply_mode": "upsert", "keys": []string{"id"}},
			},
			targetType: "mysql", targetCaps: &mysqlCapabilities,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			taskSpec := spec
			taskSpec.Target = testCase.target
			result, err := BuildWatermarkIncrementalPlan(taskSpec, StaticEngineResolver{
				1: {Type: "mysql", EngineID: 1, Capabilities: &mysqlCapabilities},
				2: {Type: testCase.targetType, EngineID: 2, Capabilities: testCase.targetCaps},
			})
			if err != nil {
				t.Fatalf("BuildWatermarkIncrementalPlan failed: %v", err)
			}
			if result.SourceEngineType != "mysql" || result.TargetEngineType != testCase.targetType {
				t.Fatalf("engine types = %s -> %s, want mysql -> %s", result.SourceEngineType, result.TargetEngineType, testCase.targetType)
			}
		})
	}
}

func TestBuildWatermarkIncrementalPlanAcceptsMySQLIdempotentTarget(t *testing.T) {
	spec := TableExportTaskSpec{
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load: LoadSpec{Mode: loadModeIncremental, ChangeDetection: &ChangeDetectionSpec{
			Type: changeTypeWatermark, Field: "updated_at", TieBreaker: []string{"id"}, Start: "committed", End: "execution_upper_bound",
		}},
		Source: EndpointSpec{Locator: tableLocator(1, "public", "orders"), DataType: dataTypeTable, Representation: representationNative},
		Target: EndpointSpec{
			ParentLocator: "addp://engine/2/path/business?type=database&node_id=2",
			Name:          "orders", DataType: dataTypeTable, Representation: representationNative,
			Policy: map[string]interface{}{"apply_mode": "upsert", "keys": []string{"id"}},
		},
		BatchSize: 100,
	}
	sourceCaps := engineplugin.EngineCapabilities{Storage: &engineplugin.StorageCapabilities{Store: &engineplugin.StoreCapability{
		BoundedWatermarkRead: true,
	}}}
	targetCaps := engineplugin.EngineCapabilities{Storage: &engineplugin.StorageCapabilities{Store: &engineplugin.StoreCapability{
		TableUpsert: &engineplugin.TableUpsertCapability{Supported: true, Idempotent: true},
	}}}
	result, err := BuildWatermarkIncrementalPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql", EngineID: 1, Capabilities: &sourceCaps},
		2: {Type: "mysql", EngineID: 2, Capabilities: &targetCaps},
	})
	if err != nil {
		t.Fatalf("BuildWatermarkIncrementalPlan failed: %v", err)
	}
	if result.SourceEngineType != "postgresql" || result.TargetEngineType != "mysql" {
		t.Fatalf("engine types = %s -> %s, want postgresql -> mysql", result.SourceEngineType, result.TargetEngineType)
	}
}

func TestParseWatermarkIncrementalRejectsLegacyWriteMode(t *testing.T) {
	config := map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load": map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{
			"type": "watermark", "field": "updated_at", "tie_breaker": []string{"id"}, "start": "committed", "end": "execution_upper_bound",
		}},
		"source": map[string]interface{}{"locator": tableLocator(1, "public", "orders"), "data_type": "table", "representation": "native"},
		"target": map[string]interface{}{"parent_locator": schemaLocator(2, "public"), "name": "orders", "data_type": "table", "representation": "native", "policy": map[string]interface{}{"write_mode": "upsert", "keys": []string{"id"}}},
	}
	if _, err := ParseTableExportTaskSpec(config, 100); err == nil {
		t.Fatal("legacy write_mode config was accepted")
	}
}
