package planner

import (
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/json"
	_ "github.com/addp/common/format/plugins/parquet"
	_ "github.com/addp/common/format/plugins/pdf"
	_ "github.com/addp/common/format/plugins/shapefile"
	"github.com/addp/transfer/internal/executor"
)

func TestBuildTableTransferPlanForNativeTableToEncodedFile(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.BatchSize = 500
	spec.Target.Policy = map[string]interface{}{"write_mode": "overwrite"}
	resolver := StaticEngineResolver{
		1: {Type: "postgresql", ConnInfo: engineplugin.ConnectionInfo{"database": "gis"}},
		2: {Type: "nfs", ConnInfo: engineplugin.ConnectionInfo{"server": "127.0.0.1", "export_path": "/data"}},
	}

	result, err := BuildTableTransferPlan(spec, resolver)
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}

	if result.SourceEngineType != "postgresql" || result.TargetEngineType != "nfs" {
		t.Fatalf("engine types = %q -> %q, want postgresql -> nfs", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Plan.Source.Kind != executor.TableEndpointNative || result.Plan.Target.Kind != executor.TableEndpointEncoded {
		t.Fatalf("endpoint kinds = %q -> %q, want native -> encoded", result.Plan.Source.Kind, result.Plan.Target.Kind)
	}
	if result.Plan.BatchSize != 500 {
		t.Fatalf("batch size = %d, want 500", result.Plan.BatchSize)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "public/roads" {
		t.Fatalf("source path = %q, want public/roads", got)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "exports/roads.csv" {
		t.Fatalf("target path = %q, want exports/roads.csv", got)
	}
	if result.Plan.Target.Format != format.FormatCSV {
		t.Fatalf("target format = %q, want csv", result.Plan.Target.Format)
	}
	if !result.Plan.Target.DeleteBeforeWrite {
		t.Fatal("target delete before write = false, want true")
	}
	if result.Plan.Target.ContentWrite.Overwrite {
		t.Fatal("content write overwrite = true, want false; overwrite is planned as delete-before-write")
	}
	if result.Plan.Target.FormatOptions == nil {
		t.Fatal("write options is nil")
	}
}

func TestBuildTableTransferPlanIncludesFieldMappingTransform(t *testing.T) {
	nullable := false
	spec := minimalNativeToEncodedSpec()
	spec.Transforms = []TransformSpec{{
		Type: "field_mapping",
		Mode: "project",
		Fields: []FieldMappingSpec{
			{Source: "id", Target: "road_id", TargetType: "bigint", Nullable: &nullable},
			{Source: "geom", Target: "geometry", TargetType: "geometry", Nullable: &nullable},
			{Target: "created_by", TargetType: "string", Default: "transfer"},
		},
	}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if len(result.Plan.Transforms) != 1 {
		t.Fatalf("transforms = %#v, want one transform", result.Plan.Transforms)
	}
	fieldMapping := result.Plan.Transforms[0].FieldMapping
	if fieldMapping == nil {
		t.Fatal("field mapping transform is nil")
	}
	if fieldMapping.Mode != executor.FieldMappingModeProject {
		t.Fatalf("field mapping mode = %q, want project", fieldMapping.Mode)
	}
	if len(fieldMapping.Fields) != 3 {
		t.Fatalf("field mappings = %#v, want 3 fields", fieldMapping.Fields)
	}
	if fieldMapping.Fields[0].Target != "road_id" || fieldMapping.Fields[0].Nullable {
		t.Fatalf("first field mapping = %#v, want road_id nullable=false", fieldMapping.Fields[0])
	}
	if fieldMapping.Fields[2].Default != "transfer" || !fieldMapping.Fields[2].Nullable {
		t.Fatalf("third field mapping = %#v, want default transfer nullable=true", fieldMapping.Fields[2])
	}
}

func TestBuildTableTransferPlanForObjectTarget(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Resource = ResourceSpec{Kind: resourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "roads.csv"}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "s3"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "exports/roads.csv" {
		t.Fatalf("target path = %q, want exports/roads.csv", got)
	}
}

func TestBuildTableTransferPlanAllowsJSONTableWriter(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatJSON
	spec.Target.Resource = ResourceSpec{Kind: resourceKindFile, Path: "exports/roads.jsonl"}
	spec.Target.Options = map[string]interface{}{"json_mode": "jsonl"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Target.Format != format.FormatJSON {
		t.Fatalf("target format = %q, want json", result.Plan.Target.Format)
	}
	if result.Plan.Target.FormatOptions == nil || result.Plan.Target.FormatOptions.ExtraParams["json_mode"] != "jsonl" {
		t.Fatalf("write options = %#v, want json_mode passthrough", result.Plan.Target.FormatOptions)
	}
}

func TestBuildTableTransferPlanPassesGeoJSONReadOptions(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatJSON
	spec.Target.Resource = ResourceSpec{Kind: resourceKindFile, Path: "exports/roads.geojson"}
	spec.Target.Options = map[string]interface{}{
		"spatial.target_encoding": "geojson",
		"geometry_field":          "geom",
	}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ReadOptions["spatial.target_encoding"] != "geojson" || result.Plan.Source.ReadOptions["geometry_field"] != "geom" {
		t.Fatalf("read options = %#v, want geojson geometry read options", result.Plan.Source.ReadOptions)
	}
}

func TestBuildTableTransferPlanAllowsParquetTableWriter(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatParquet
	spec.Target.Resource = ResourceSpec{Kind: resourceKindFile, Path: "exports/roads.parquet"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Target.Format != format.FormatParquet {
		t.Fatalf("target format = %q, want parquet", result.Plan.Target.Format)
	}
	if result.Plan.Target.FormatOptions == nil {
		t.Fatal("write options is nil")
	}
}

func TestBuildTableTransferPlanAllowsShapefileMultiTableWriter(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatShapefile
	spec.Target.Resource = ResourceSpec{Kind: resourceKindFile, Path: "exports/roads.shp"}
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geom",
		"geometry_type":  "Point",
	}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Target.Format != format.FormatShapefile {
		t.Fatalf("target format = %q, want shapefile", result.Plan.Target.Format)
	}
	if result.Plan.Target.FormatOptions == nil || result.Plan.Target.FormatOptions.ExtraParams["geometry_field"] != "geom" {
		t.Fatalf("write options = %#v, want geometry_field passthrough", result.Plan.Target.FormatOptions)
	}
}

func TestBuildTableTransferPlanForEncodedFileToNativeTable(t *testing.T) {
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

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.SourceEngineType != "nfs" || result.TargetEngineType != "postgresql" {
		t.Fatalf("engine types = %q -> %q, want nfs -> postgresql", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Plan.Source.Kind != executor.TableEndpointEncoded || result.Plan.Target.Kind != executor.TableEndpointNative {
		t.Fatalf("endpoint kinds = %q -> %q, want encoded -> native", result.Plan.Source.Kind, result.Plan.Target.Kind)
	}
	if result.Plan.Source.Format != format.FormatCSV {
		t.Fatalf("source format = %q, want csv", result.Plan.Source.Format)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "imports/roads.csv" {
		t.Fatalf("source path = %q, want imports/roads.csv", got)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "public/roads" {
		t.Fatalf("target path = %q, want public/roads", got)
	}
	if result.Plan.Target.TableWrite.Method != "copy" {
		t.Fatalf("write method = %q, want postgresql import default copy", result.Plan.Target.TableWrite.Method)
	}
}

func TestBuildTableTransferPlanAllowsJSONTableReader(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = format.FormatJSON
	spec.Source.Resource = ResourceSpec{Kind: resourceKindFile, Path: "imports/roads.jsonl"}
	spec.Source.Options = map[string]interface{}{"json_mode": "jsonl"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.Format != format.FormatJSON {
		t.Fatalf("source format = %q, want json", result.Plan.Source.Format)
	}
	if result.Plan.Source.ParseOptions == nil || result.Plan.Source.ParseOptions.ExtraParams["json_mode"] != "jsonl" {
		t.Fatalf("parse options = %#v, want json_mode passthrough", result.Plan.Source.ParseOptions)
	}
}

func TestBuildTableTransferPlanForEncodedObjectToEncodedObject(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Target.Format = format.FormatJSON
	spec.Target.Options = map[string]interface{}{"json_mode": "jsonl"}
	spec.Target.Resource = ResourceSpec{Kind: resourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "roads.jsonl"}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "minio"},
		2: {Type: "minio"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.SourceEngineType != "minio" || result.TargetEngineType != "minio" {
		t.Fatalf("engine types = %q -> %q, want minio -> minio", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Plan.Source.Format != format.FormatCSV || result.Plan.Target.Format != format.FormatJSON {
		t.Fatalf("formats = %q -> %q, want csv -> json", result.Plan.Source.Format, result.Plan.Target.Format)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "imports/roads.csv" {
		t.Fatalf("source path = %q, want imports/roads.csv", got)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "exports/roads.jsonl" {
		t.Fatalf("target path = %q, want exports/roads.jsonl", got)
	}
	if !result.Plan.Target.DeleteBeforeWrite {
		t.Fatal("target delete before write = false, want true")
	}
	if result.Plan.Target.FormatOptions == nil || result.Plan.Target.FormatOptions.ExtraParams["json_mode"] != "jsonl" {
		t.Fatalf("write options = %#v, want json_mode passthrough", result.Plan.Target.FormatOptions)
	}
}

func TestBuildTableTransferPlanForNativeTableToNativeTable(t *testing.T) {
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
			Resource:       ResourceSpec{Kind: resourceKindNativeTable, Path: map[string]interface{}{"schema": "gis", "table": "roads_copy"}},
			DataType:       dataTypeTable,
			Representation: representationNative,
			Policy:         map[string]interface{}{"write_mode": "overwrite"},
		},
		BatchSize: 500,
	}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.SourceEngineType != "postgresql" || result.TargetEngineType != "postgresql" {
		t.Fatalf("engine types = %q -> %q, want postgresql -> postgresql", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Plan.Source.Kind != executor.TableEndpointNative || result.Plan.Target.Kind != executor.TableEndpointNative {
		t.Fatalf("endpoint kinds = %q -> %q, want native -> native", result.Plan.Source.Kind, result.Plan.Target.Kind)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "public/roads" {
		t.Fatalf("source path = %q, want public/roads", got)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "gis/roads_copy" {
		t.Fatalf("target path = %q, want gis/roads_copy", got)
	}
	if !result.Plan.Target.DeleteBeforeWrite {
		t.Fatal("target delete before write = false, want true")
	}
	if result.Plan.Target.TableWrite.Method != "copy" {
		t.Fatalf("write method = %q, want copy", result.Plan.Target.TableWrite.Method)
	}
}

func TestBuildTableTransferPlanRejectsNonTableFormat(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatPDF

	_, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableTransferPlan succeeded, want table writer provider error")
	}
	if !strings.Contains(err.Error(), "table writer provider") {
		t.Fatalf("error = %q, want table writer provider error", err)
	}
}

func TestBuildTableTransferPlanRejectsInvalidShape(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Source.Representation = representationEncoded

	_, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableTransferPlan succeeded, want representation error")
	}
	if !strings.Contains(err.Error(), "source encoded endpoint resource kind") {
		t.Fatalf("error = %q, want encoded source resource kind error", err)
	}
}

func TestBuildTableTransferPlanSplitsOverwritePolicy(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Target.Policy = map[string]interface{}{"write_mode": "overwrite"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if !result.Plan.Target.DeleteBeforeWrite {
		t.Fatal("target delete before write = false, want true")
	}
}

func TestBuildTableTransferPlanKeepsAppendPolicy(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Target.Policy = map[string]interface{}{"write_mode": "append", "write_method": "insert"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Target.DeleteBeforeWrite {
		t.Fatal("target delete before write = true, want false")
	}
	if result.Plan.Target.TableWrite.Method != "insert" {
		t.Fatalf("write method = %q, want explicit insert", result.Plan.Target.TableWrite.Method)
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

func minimalNativeToEncodedSpec() TableExportTaskSpec {
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

func minimalEncodedToNativeSpec() TableExportTaskSpec {
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

func minimalEncodedToEncodedSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 1},
			Resource:       ResourceSpec{Kind: resourceKindObject, Path: map[string]interface{}{"bucket": "imports", "path": "roads.csv"}},
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
		},
		Target: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 2},
			Resource:       ResourceSpec{Kind: resourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "roads.csv"}},
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
		},
	}
}
