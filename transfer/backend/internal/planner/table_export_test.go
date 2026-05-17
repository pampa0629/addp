package planner

import (
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/json"
	_ "github.com/addp/common/format/plugins/pdf"
)

func TestBuildTableExportPlanForNativeTableToCSVFile(t *testing.T) {
	spec := TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 1},
			Resource:       ResourceSpec{Kind: resourceKindNativeTable, Path: map[string]interface{}{"schema": "public", "table": "roads"}},
			DataType:       dataTypeTable,
			Representation: representationNative,
		},
		Target: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 2},
			Resource:       ResourceSpec{Kind: resourceKindFile, Path: "exports/roads.csv"},
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
			Policy:         map[string]interface{}{"write_mode": "overwrite"},
		},
		BatchSize: 500,
	}
	resolver := StaticEngineResolver{
		1: {Type: "postgresql", ConnInfo: engineplugin.ConnectionInfo{"database": "gis"}},
		2: {Type: "nfs", ConnInfo: engineplugin.ConnectionInfo{"server": "127.0.0.1", "export_path": "/data"}},
	}

	result, err := BuildTableExportPlan(spec, resolver)
	if err != nil {
		t.Fatalf("BuildTableExportPlan failed: %v", err)
	}

	if result.SourceEngineType != "postgresql" || result.TargetEngineType != "nfs" {
		t.Fatalf("engine types = %q -> %q, want postgresql -> nfs", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Format != format.FormatCSV || result.Plan.Format != format.FormatCSV {
		t.Fatalf("format = %q / %q, want csv", result.Format, result.Plan.Format)
	}
	if result.Plan.BatchSize != 500 {
		t.Fatalf("batch size = %d, want 500", result.Plan.BatchSize)
	}
	if got := result.Plan.SourcePath.StringPath(); got != "public/roads" {
		t.Fatalf("source path = %q, want public/roads", got)
	}
	if got := result.Plan.TargetPath.StringPath(); got != "exports/roads.csv" {
		t.Fatalf("target path = %q, want exports/roads.csv", got)
	}
	if !result.Plan.TargetWrite.Overwrite {
		t.Fatal("target overwrite = false, want true")
	}
	if result.Plan.WriteOptions == nil {
		t.Fatal("write options is nil")
	}
}

func TestBuildTableExportPlanForObjectTarget(t *testing.T) {
	spec := minimalTableExportSpec()
	spec.Target.Resource = ResourceSpec{Kind: resourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "roads.csv"}}

	result, err := BuildTableExportPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "s3"},
	})
	if err != nil {
		t.Fatalf("BuildTableExportPlan failed: %v", err)
	}
	if got := result.Plan.TargetPath.StringPath(); got != "exports/roads.csv" {
		t.Fatalf("target path = %q, want exports/roads.csv", got)
	}
}

func TestBuildTableExportPlanAllowsJSONTableWriter(t *testing.T) {
	spec := minimalTableExportSpec()
	spec.Target.Format = format.FormatJSON
	spec.Target.Resource = ResourceSpec{Kind: resourceKindFile, Path: "exports/roads.jsonl"}
	spec.Target.Options = map[string]interface{}{"json_mode": "jsonl"}

	result, err := BuildTableExportPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableExportPlan failed: %v", err)
	}
	if result.Format != format.FormatJSON || result.Plan.Format != format.FormatJSON {
		t.Fatalf("format = %q / %q, want json", result.Format, result.Plan.Format)
	}
	if result.Plan.WriteOptions == nil || result.Plan.WriteOptions.ExtraParams["json_mode"] != "jsonl" {
		t.Fatalf("write options = %#v, want json_mode passthrough", result.Plan.WriteOptions)
	}
}

func TestBuildTableExportPlanPassesGeoJSONReadOptions(t *testing.T) {
	spec := minimalTableExportSpec()
	spec.Target.Format = format.FormatJSON
	spec.Target.Resource = ResourceSpec{Kind: resourceKindFile, Path: "exports/roads.geojson"}
	spec.Target.Options = map[string]interface{}{
		"spatial.target_encoding": "geojson",
		"geometry_field":          "geom",
	}

	result, err := BuildTableExportPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableExportPlan failed: %v", err)
	}
	if result.Plan.ReadOptions["spatial.target_encoding"] != "geojson" || result.Plan.ReadOptions["geometry_field"] != "geom" {
		t.Fatalf("read options = %#v, want geojson geometry read options", result.Plan.ReadOptions)
	}
}

func TestBuildTableImportPlanForCSVFileToNativeTable(t *testing.T) {
	spec := TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 1},
			Resource:       ResourceSpec{Kind: resourceKindFile, Path: map[string]interface{}{"path": "imports/roads.csv"}},
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
			Options:        map[string]interface{}{"header": true, "delimiter": ","},
		},
		Target: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 2},
			Resource:       ResourceSpec{Kind: resourceKindNativeTable, Path: map[string]interface{}{"schema": "public", "table": "roads"}},
			DataType:       dataTypeTable,
			Representation: representationNative,
			Policy:         map[string]interface{}{"write_mode": "append"},
		},
		BatchSize: 500,
	}

	result, err := BuildTableImportPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableImportPlan failed: %v", err)
	}
	if result.SourceEngineType != "nfs" || result.TargetEngineType != "postgresql" {
		t.Fatalf("engine types = %q -> %q, want nfs -> postgresql", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Format != format.FormatCSV || result.Plan.Format != format.FormatCSV {
		t.Fatalf("format = %q / %q, want csv", result.Format, result.Plan.Format)
	}
	if got := result.Plan.SourcePath.StringPath(); got != "imports/roads.csv" {
		t.Fatalf("source path = %q, want imports/roads.csv", got)
	}
	if got := result.Plan.TargetPath.StringPath(); got != "public/roads" {
		t.Fatalf("target path = %q, want public/roads", got)
	}
	if result.Plan.TargetWrite.Mode != "append" {
		t.Fatalf("write mode = %q, want append", result.Plan.TargetWrite.Mode)
	}
	if result.Plan.TargetWrite.Method != "copy" {
		t.Fatalf("write method = %q, want postgresql import default copy", result.Plan.TargetWrite.Method)
	}
}

func TestBuildTableImportPlanAllowsJSONTableReader(t *testing.T) {
	spec := minimalTableImportSpec()
	spec.Source.Format = format.FormatJSON
	spec.Source.Resource = ResourceSpec{Kind: resourceKindFile, Path: "imports/roads.jsonl"}
	spec.Source.Options = map[string]interface{}{"json_mode": "jsonl"}

	result, err := BuildTableImportPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableImportPlan failed: %v", err)
	}
	if result.Format != format.FormatJSON || result.Plan.Format != format.FormatJSON {
		t.Fatalf("format = %q / %q, want json", result.Format, result.Plan.Format)
	}
	if result.Plan.ParseOptions == nil || result.Plan.ParseOptions.ExtraParams["json_mode"] != "jsonl" {
		t.Fatalf("parse options = %#v, want json_mode passthrough", result.Plan.ParseOptions)
	}
}

func TestBuildTableExportPlanRejectsNonTableFormat(t *testing.T) {
	spec := minimalTableExportSpec()
	spec.Target.Format = format.FormatPDF

	_, err := BuildTableExportPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableExportPlan succeeded, want table writer provider error")
	}
	if !strings.Contains(err.Error(), "table writer provider") {
		t.Fatalf("error = %q, want table writer provider error", err)
	}
}

func TestBuildTableExportPlanRejectsEncodedSource(t *testing.T) {
	spec := minimalTableExportSpec()
	spec.Source.Representation = representationEncoded

	_, err := BuildTableExportPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableExportPlan succeeded, want representation error")
	}
	if !strings.Contains(err.Error(), "source encoded endpoint resource kind") {
		t.Fatalf("error = %q, want encoded source resource kind error", err)
	}
}

func TestBuildTableImportPlanSplitsTruncateInsertPolicy(t *testing.T) {
	spec := minimalTableImportSpec()
	spec.Target.Policy = map[string]interface{}{"write_mode": "truncate_insert"}

	result, err := BuildTableImportPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableImportPlan failed: %v", err)
	}
	if result.Plan.TargetPrepare.Mode != "truncate_insert" {
		t.Fatalf("prepare mode = %q, want truncate_insert", result.Plan.TargetPrepare.Mode)
	}
	if result.Plan.TargetWrite.Mode != "append" {
		t.Fatalf("write mode = %q, want append after truncate", result.Plan.TargetWrite.Mode)
	}
}

func TestBuildTableImportPlanKeepsCreateIfNotExistsPolicy(t *testing.T) {
	spec := minimalTableImportSpec()
	spec.Target.Policy = map[string]interface{}{"write_mode": "create_if_not_exists", "write_method": "insert"}

	result, err := BuildTableImportPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableImportPlan failed: %v", err)
	}
	if result.Plan.TargetPrepare.Mode != "create_if_not_exists" {
		t.Fatalf("prepare mode = %q, want create_if_not_exists", result.Plan.TargetPrepare.Mode)
	}
	if result.Plan.TargetWrite.Mode != "append" {
		t.Fatalf("write mode = %q, want append after create_if_not_exists", result.Plan.TargetWrite.Mode)
	}
	if result.Plan.TargetWrite.Method != "insert" {
		t.Fatalf("write method = %q, want explicit insert", result.Plan.TargetWrite.Method)
	}
}

func TestParseTableExportTaskSpecRejectsLegacyConfig(t *testing.T) {
	_, err := ParseTableExportTaskSpec(map[string]interface{}{
		"connector_type": "postgresql",
		"source_config":  map[string]interface{}{"table": "roads"},
	}, 1000)
	if err == nil {
		t.Fatal("ParseTableExportTaskSpec succeeded, want legacy config error")
	}
	if !strings.Contains(err.Error(), "legacy transfer task config") {
		t.Fatalf("error = %q, want legacy config error", err)
	}
}

func TestParseTableExportTaskSpecRequiresMode(t *testing.T) {
	config := map[string]interface{}{
		"source": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 1},
			"resource":       map[string]interface{}{"kind": "native_table", "path": map[string]interface{}{"schema": "public", "table": "roads"}},
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 2},
			"resource":       map[string]interface{}{"kind": "file", "path": map[string]interface{}{"path": "exports/roads.csv"}},
			"data_type":      "table",
			"representation": "encoded",
			"format":         "csv",
		},
	}

	_, err := ParseTableExportTaskSpec(config, 1000)
	if err == nil {
		t.Fatal("ParseTableExportTaskSpec succeeded, want mode error")
	}
	if !strings.Contains(err.Error(), "mode is required") {
		t.Fatalf("error = %q, want mode error", err)
	}
}

func TestParseTableExportTaskSpecAppliesFallbackBatchSize(t *testing.T) {
	spec, err := ParseTableExportTaskSpec(map[string]interface{}{
		"mode": "batch",
		"source": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 1},
			"resource":       map[string]interface{}{"kind": "native_table", "path": map[string]interface{}{"schema": "public", "table": "roads"}},
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 2},
			"resource":       map[string]interface{}{"kind": "file", "path": map[string]interface{}{"path": "exports/roads.csv"}},
			"data_type":      "table",
			"representation": "encoded",
			"format":         "csv",
		},
	}, 2048)
	if err != nil {
		t.Fatalf("ParseTableExportTaskSpec failed: %v", err)
	}
	if spec.BatchSize != 2048 {
		t.Fatalf("batch size = %d, want 2048", spec.BatchSize)
	}
}

func minimalTableExportSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 1},
			Resource:       ResourceSpec{Kind: resourceKindNativeTable, Path: map[string]interface{}{"name": "public.roads"}},
			DataType:       dataTypeTable,
			Representation: representationNative,
		},
		Target: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 2},
			Resource:       ResourceSpec{Kind: resourceKindFile, Path: map[string]interface{}{"path": "exports/roads.csv"}},
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
		},
	}
}

func minimalTableImportSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 1},
			Resource:       ResourceSpec{Kind: resourceKindFile, Path: map[string]interface{}{"path": "imports/roads.csv"}},
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
		},
		Target: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 2},
			Resource:       ResourceSpec{Kind: resourceKindNativeTable, Path: map[string]interface{}{"schema": "public", "table": "roads"}},
			DataType:       dataTypeTable,
			Representation: representationNative,
		},
	}
}
