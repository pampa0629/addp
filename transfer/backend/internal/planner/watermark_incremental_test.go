package planner

import (
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
)

func TestBuildWatermarkIncrementalPlanRequiresPostgresCapabilities(t *testing.T) {
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
