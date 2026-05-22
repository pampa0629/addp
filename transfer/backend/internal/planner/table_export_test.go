package planner

import (
	"github.com/addp/common/datatype"
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

func TestBuildTableTransferPlanPushesFieldSelectionForNativeSourceProjectMapping(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Transforms = []TransformSpec{{
		Type: "field_mapping",
		Mode: "project",
		Fields: []FieldMappingSpec{
			{Source: "id", Target: "road_id"},
			{Source: "name", Target: "road_name"},
			{Target: "created_by", Default: "transfer"},
		},
	}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	selection, ok := result.Plan.Source.ReadOptions[format.FieldSelectionOptionKey].(*format.FieldSelectionOptions)
	if !ok || selection == nil {
		t.Fatalf("source read options = %#v, want field selection", result.Plan.Source.ReadOptions)
	}
	want := []string{"id", "name"}
	if len(selection.Include) != len(want) {
		t.Fatalf("field selection include = %#v, want %#v", selection.Include, want)
	}
	for i, field := range want {
		if selection.Include[i] != field {
			t.Fatalf("field selection include = %#v, want %#v", selection.Include, want)
		}
	}
}

func TestBuildTableTransferPlanPushesFieldSelectionForEncodedSourceProjectMapping(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Transforms = []TransformSpec{{
		Type: "field_mapping",
		Mode: "project",
		Fields: []FieldMappingSpec{
			{Source: "id", Target: "road_id"},
			{Source: "name", Target: "road_name"},
			{Source: "id", Target: "id_copy"},
			{Target: "created_by", Default: "transfer"},
		},
	}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	selection := result.Plan.Source.ParseOptions.FieldSelection
	if selection == nil {
		t.Fatal("source field selection is nil")
	}
	if selection.EffectiveMissingFieldPolicy() != format.MissingFieldError {
		t.Fatalf("missing field policy = %q, want error", selection.EffectiveMissingFieldPolicy())
	}
	want := []string{"id", "name"}
	if len(selection.Include) != len(want) {
		t.Fatalf("field selection include = %#v, want %#v", selection.Include, want)
	}
	for i, field := range want {
		if selection.Include[i] != field {
			t.Fatalf("field selection include = %#v, want %#v", selection.Include, want)
		}
	}
}

func TestBuildTableTransferPlanDoesNotPushFieldSelectionForPassthroughMapping(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Transforms = []TransformSpec{{
		Type: "field_mapping",
		Mode: "passthrough",
		Fields: []FieldMappingSpec{
			{Source: "id", Target: "road_id"},
			{Target: "created_by", Default: "transfer"},
		},
	}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ParseOptions.FieldSelection != nil {
		t.Fatalf("source field selection = %#v, want nil for passthrough", result.Plan.Source.ParseOptions.FieldSelection)
	}
}

func TestBuildTableTransferPlanDoesNotPushNativeFieldSelectionForPassthroughMapping(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Transforms = []TransformSpec{{
		Type: "field_mapping",
		Mode: "passthrough",
		Fields: []FieldMappingSpec{
			{Source: "id", Target: "road_id"},
			{Target: "created_by", Default: "transfer"},
		},
	}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ReadOptions != nil {
		t.Fatalf("source read options = %#v, want nil for passthrough", result.Plan.Source.ReadOptions)
	}
}

func TestBuildTableTransferPlanForObjectTarget(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "roads.csv"}}

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

func TestBuildTableTransferPlanAppendsTargetExtensionForObjectTarget(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatParquet
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "lake3"}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "minio"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "exports/lake3.parquet" {
		t.Fatalf("target path = %q, want exports/lake3.parquet", got)
	}
}

func TestBuildTableTransferPlanAppendsTargetExtensionForFileTarget(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatCSV
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: map[string]interface{}{"path": "exports/roads"}}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "exports/roads.csv" {
		t.Fatalf("target path = %q, want exports/roads.csv", got)
	}
}

func TestBuildTableTransferPlanRejectsConflictingTargetExtension(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatParquet
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: map[string]interface{}{"path": "exports/roads.csv"}}

	_, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableTransferPlan succeeded, want extension conflict error")
	}
	if !strings.Contains(err.Error(), "target path extension") || !strings.Contains(err.Error(), ".parquet") {
		t.Fatalf("error = %q, want target extension conflict", err)
	}
}

func TestBuildTableTransferPlanAppendsLogicalJSONTargetExtensions(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]interface{}
		want    string
	}{
		{name: "json array", options: map[string]interface{}{"json_mode": "array"}, want: "exports/roads.json"},
		{name: "json lines", options: map[string]interface{}{"json_mode": "jsonl"}, want: "exports/roads.jsonl"},
		{name: "geojson", options: map[string]interface{}{"spatial.target_encoding": "geojson"}, want: "exports/roads.geojson"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalNativeToEncodedSpec()
			spec.Target.Format = format.FormatJSON
			spec.Target.Options = tt.options
			spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: map[string]interface{}{"path": "exports/roads"}}

			result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
				1: {Type: "postgresql"},
				2: {Type: "nfs"},
			})
			if err != nil {
				t.Fatalf("BuildTableTransferPlan failed: %v", err)
			}
			if got := result.Plan.Target.Path.StringPath(); got != tt.want {
				t.Fatalf("target path = %q, want %s", got, tt.want)
			}
		})
	}
}

func TestBuildTableTransferPlanAllowsJSONTableWriter(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatJSON
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "exports/roads.jsonl"}
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
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "exports/roads.geojson"}
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
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "exports/roads.parquet"}

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
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "exports/roads.shp"}
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
			Engine:           EngineRef{Scope: "system", ID: 1},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: map[string]interface{}{"path": "imports/roads.csv"}},
			DataType:         dataTypeTable,
			Representation:   representationEncoded,
			Format:           format.FormatCSV,
			Options:          map[string]interface{}{"header": true, "delimiter": ","},
		},
		Target: EndpointSpec{
			Engine:           EngineRef{Scope: "system", ID: 2},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindNativeTable, Path: map[string]interface{}{"schema": "public", "table": "roads"}},
			DataType:         dataTypeTable,
			Representation:   representationNative,
			Policy:           map[string]interface{}{"write_mode": "append"},
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
	if result.Plan.Target.TableWrite.Method != "" {
		t.Fatalf("write method = %q, want planner to leave native writer default", result.Plan.Target.TableWrite.Method)
	}
}

func TestBuildTableTransferPlanConsumesMetaSingleSourceAttributes(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = ""
	spec.Source.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "imports/stale.csv"}
	spec.Source.Attributes = tableSourceAttributes("single", "csv", "imports/meta_roads.csv", nil, []map[string]interface{}{
		{"name": "id", "type": "bigint", "nullable": false, "is_primary_key": true},
		{"name": "road_name", "type": "string", "nullable": true},
	}, nil)

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.Format != format.FormatCSV {
		t.Fatalf("source format = %q, want csv from Meta attributes", result.Plan.Source.Format)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "imports/meta_roads.csv" {
		t.Fatalf("source path = %q, want Meta storage physical path", got)
	}
	if result.Plan.Source.Schema == nil || len(result.Plan.Source.Schema.Fields) != 2 {
		t.Fatalf("source schema = %#v, want fields from Meta attributes", result.Plan.Source.Schema)
	}
	if result.Plan.Source.Schema.Fields[0].Type != datatype.FieldTypeBigInt || !result.Plan.Source.Schema.Fields[0].PrimaryKey {
		t.Fatalf("first source field = %#v, want standard bigint primary key field", result.Plan.Source.Schema.Fields[0])
	}
}

func TestBuildTableTransferPlanConsumesMetaMultiSourceRefsAndSpatialAttributes(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = ""
	spec.Source.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "imports/stale.shp"}
	spec.Source.Attributes = tableSourceAttributes("multi", "shapefile", "imports/roads.shp", []map[string]interface{}{
		{"path": "imports/roads.shp", "role": "main", "extension": ".shp", "required": true, "primary": true},
		{"path": "imports/roads.shx", "role": "index", "extension": ".shx", "required": true},
		{"path": "imports/roads.dbf", "role": "attributes", "extension": ".dbf", "required": true},
		{"path": "imports/roads.prj", "role": "projection", "extension": ".prj", "required": false},
	}, []map[string]interface{}{
		{"name": "id", "type": "bigint", "nullable": false},
		{"name": "shape", "type": "geometry", "nullable": false},
	}, map[string]interface{}{
		"geometry_columns":        []map[string]interface{}{{"name": "shape", "geometry_type": "MultiPolygon", "srid": 4326, "dimension": 2}},
		"primary_geometry_column": "shape",
		"has_spatial_index":       true,
	})

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.Format != format.FormatShapefile {
		t.Fatalf("source format = %q, want shapefile from Meta attributes", result.Plan.Source.Format)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "imports/roads.shp" {
		t.Fatalf("source path = %q, want primary ref path", got)
	}
	if len(result.Plan.Source.RelatedRefs) != 4 {
		t.Fatalf("source related refs = %#v, want refs restored from Meta attributes", result.Plan.Source.RelatedRefs)
	}
	if result.Plan.Source.RelatedRefs[2].Ref.Path != "imports/roads.dbf" || !result.Plan.Source.RelatedRefs[2].Required {
		t.Fatalf("dbf ref = %#v, want required attributes ref from Meta attributes", result.Plan.Source.RelatedRefs[2])
	}
	if result.Plan.Source.Schema == nil || result.Plan.Source.Schema.SpatialInfo == nil {
		t.Fatalf("source schema = %#v, want spatial info from Meta attributes", result.Plan.Source.Schema)
	}
	if result.Plan.Source.Schema.SpatialInfo.PrimaryGeometryName() != "shape" || result.Plan.Source.Schema.SpatialInfo.PrimaryGeometryType() != "MultiPolygon" {
		t.Fatalf("source spatial info = %#v, want primary geometry column from capabilities.spatial", result.Plan.Source.Schema.SpatialInfo)
	}
	if result.Plan.Source.ParseOptions == nil || result.Plan.Source.ParseOptions.ExtraParams["geometry_field"] != "shape" {
		t.Fatalf("source parse options = %#v, want geometry_field from capabilities.spatial", result.Plan.Source.ParseOptions)
	}
	if result.Plan.Source.ParseOptions.GeometryEncoding != format.GeometryEncodingEWKB {
		t.Fatalf("geometry encoding = %q, want ewkb for spatial encoded import", result.Plan.Source.ParseOptions.GeometryEncoding)
	}
}

func TestBuildTableTransferPlanRejectsIncompleteMetaMultiSourceRefs(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = ""
	spec.Source.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "imports/roads.shp"}
	spec.Source.Attributes = tableSourceAttributes("multi", "shapefile", "imports/roads.shp", []map[string]interface{}{
		{"path": "imports/roads.shp", "role": "main", "extension": ".shp", "required": true, "primary": true},
		{"path": "imports/roads.shx", "role": "index", "extension": ".shx", "required": true},
	}, []map[string]interface{}{{"name": "id", "type": "bigint"}}, nil)

	_, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "postgresql"},
	})
	if err == nil {
		t.Fatal("BuildTableTransferPlan succeeded, want incomplete Meta refs error")
	}
	if !strings.Contains(err.Error(), "missing required refs") || !strings.Contains(err.Error(), ".dbf") {
		t.Fatalf("error = %q, want missing required dbf ref error", err)
	}
}

func TestBuildTableTransferPlanConsumesMetaWholeSourceAttributes(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Format = ""
	spec.Source.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "datasets/stale"}
	spec.Source.Attributes = tableSourceAttributes("whole", "parquet", "datasets/lake_table", nil, []map[string]interface{}{
		{"name": "id", "type": "bigint"},
		{"name": "amount", "type": "decimal", "size": 18, "precision": 2},
	}, nil)

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.Layout != format.FormatLayoutWhole {
		t.Fatalf("source layout = %q, want whole from Meta attributes", result.Plan.Source.Layout)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "datasets/lake_table" {
		t.Fatalf("source path = %q, want whole scope physical path", got)
	}
	if result.Plan.Source.Schema == nil || len(result.Plan.Source.Schema.Fields) != 2 {
		t.Fatalf("source schema = %#v, want fields from Meta attributes", result.Plan.Source.Schema)
	}
}

func TestBuildTableTransferPlanUsesMetaObjectWholePhysicalPathAsScope(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Format = ""
	spec.Source.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindObject, Path: map[string]interface{}{"bucket": "manager", "path": "stale"}}
	spec.Source.Attributes = tableSourceAttributes("whole", "parquet", "regression/codex-parquet-whole-20260521", nil, []map[string]interface{}{
		{"name": "id", "type": "bigint"},
	}, nil)
	spec.Source.Attributes["storage"].(map[string]interface{})["bucket"] = "manager"
	spec.Source.Attributes["storage"].(map[string]interface{})["path"] = "regression/"
	spec.Source.Attributes["storage"].(map[string]interface{})["name"] = "codex-parquet-whole-20260521"

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "minio"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.Layout != format.FormatLayoutWhole {
		t.Fatalf("source layout = %q, want whole", result.Plan.Source.Layout)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "manager/regression/codex-parquet-whole-20260521" {
		t.Fatalf("source path = %q, want manager/regression/codex-parquet-whole-20260521", got)
	}
}

func TestBuildTableTransferPlanRequestsEWKBForSpatialEncodedImportToNativeTarget(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = format.FormatShapefile
	spec.Source.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "imports/roads.shp"}
	spec.Source.Options = map[string]interface{}{"encoding": "GBK"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "native_table_target"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ParseOptions == nil {
		t.Fatal("source parse options is nil")
	}
	if result.Plan.Source.ParseOptions.GeometryEncoding != format.GeometryEncodingEWKB {
		t.Fatalf("geometry encoding = %q, want ewkb", result.Plan.Source.ParseOptions.GeometryEncoding)
	}
	if result.Plan.Source.ParseOptions.Encoding != "GBK" {
		t.Fatalf("encoding = %q, want GBK", result.Plan.Source.ParseOptions.Encoding)
	}
}

func TestBuildTableTransferPlanKeepsDefaultGeometryEncodingForNonSpatialEncodedImport(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = format.FormatCSV
	spec.Source.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "imports/roads.csv"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "native_table_target"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ParseOptions != nil && result.Plan.Source.ParseOptions.GeometryEncoding == format.GeometryEncodingEWKB {
		t.Fatalf("geometry encoding = %q, want no spatial EWKB override", result.Plan.Source.ParseOptions.GeometryEncoding)
	}
}

func TestBuildTableTransferPlanAllowsJSONTableReader(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = format.FormatJSON
	spec.Source.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: "imports/roads.jsonl"}
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
	spec.Target.EndpointResource = EndpointResourceSpec{Kind: EndpointResourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "roads.jsonl"}}

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
			Engine:           EngineRef{Scope: "system", ID: 1},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindNativeTable, Path: map[string]interface{}{"schema": "public", "table": "roads"}},
			DataType:         dataTypeTable,
			Representation:   representationNative,
		},
		Target: EndpointSpec{
			Engine:           EngineRef{Scope: "system", ID: 2},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindNativeTable, Path: map[string]interface{}{"schema": "gis", "table": "roads_copy"}},
			DataType:         dataTypeTable,
			Representation:   representationNative,
			Policy:           map[string]interface{}{"write_mode": "overwrite"},
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
	if result.Plan.Target.TableWrite.Method != "" {
		t.Fatalf("write method = %q, want planner to leave native writer default", result.Plan.Target.TableWrite.Method)
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

func TestParseTableExportTaskSpecPreservesSourceAttributes(t *testing.T) {
	spec, err := ParseTableExportTaskSpec(map[string]interface{}{
		"mode": "batch",
		"source": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 1},
			"resource":       map[string]interface{}{"kind": "file", "path": map[string]interface{}{"path": "imports/roads.shp"}},
			"data_type":      "table",
			"representation": "encoded",
			"attributes": tableSourceAttributes("multi", "shapefile", "imports/roads.shp", []map[string]interface{}{
				{"path": "imports/roads.shp", "role": "main", "extension": ".shp", "required": true, "primary": true},
				{"path": "imports/roads.shx", "role": "index", "extension": ".shx", "required": true},
				{"path": "imports/roads.dbf", "role": "attributes", "extension": ".dbf", "required": true},
			}, []map[string]interface{}{
				{"name": "shape", "type": "geometry"},
			}, map[string]interface{}{
				"primary_geometry_column": "shape",
				"geometry_columns":        []map[string]interface{}{{"name": "shape", "geometry_type": "Point", "srid": 4326}},
			}),
		},
		"target": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 2},
			"resource":       map[string]interface{}{"kind": "native_table", "path": map[string]interface{}{"schema": "public", "table": "roads"}},
			"data_type":      "table",
			"representation": "native",
		},
	}, 1000)
	if err != nil {
		t.Fatalf("ParseTableExportTaskSpec failed: %v", err)
	}
	if spec.Source.Format != "" {
		t.Fatalf("source format = %q, want format restored later from attributes", spec.Source.Format)
	}
	if spec.Source.Attributes == nil {
		t.Fatal("source attributes are nil")
	}
	itemAttrs, ok := spec.Source.Attributes["item"].(map[string]interface{})
	if !ok || itemAttrs["format"] != "shapefile" {
		t.Fatalf("source item attrs = %#v, want shapefile attributes preserved", spec.Source.Attributes["item"])
	}
}

func minimalNativeToEncodedSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:           EngineRef{Scope: "system", ID: 1},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindNativeTable, Path: map[string]interface{}{"name": "public.roads"}},
			DataType:         dataTypeTable,
			Representation:   representationNative,
		},
		Target: EndpointSpec{
			Engine:           EngineRef{Scope: "system", ID: 2},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: map[string]interface{}{"path": "exports/roads.csv"}},
			DataType:         dataTypeTable,
			Representation:   representationEncoded,
			Format:           format.FormatCSV,
		},
	}
}

func minimalEncodedToNativeSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:           EngineRef{Scope: "system", ID: 1},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindFile, Path: map[string]interface{}{"path": "imports/roads.csv"}},
			DataType:         dataTypeTable,
			Representation:   representationEncoded,
			Format:           format.FormatCSV,
		},
		Target: EndpointSpec{
			Engine:           EngineRef{Scope: "system", ID: 2},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindNativeTable, Path: map[string]interface{}{"schema": "public", "table": "roads"}},
			DataType:         dataTypeTable,
			Representation:   representationNative,
		},
	}
}

func minimalEncodedToEncodedSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:           EngineRef{Scope: "system", ID: 1},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindObject, Path: map[string]interface{}{"bucket": "imports", "path": "roads.csv"}},
			DataType:         dataTypeTable,
			Representation:   representationEncoded,
			Format:           format.FormatCSV,
		},
		Target: EndpointSpec{
			Engine:           EngineRef{Scope: "system", ID: 2},
			EndpointResource: EndpointResourceSpec{Kind: EndpointResourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "roads.csv"}},
			DataType:         dataTypeTable,
			Representation:   representationEncoded,
			Format:           format.FormatCSV,
		},
	}
}

func tableSourceAttributes(layout, formatName, physicalPath string, refs []map[string]interface{}, fields []map[string]interface{}, spatial map[string]interface{}) map[string]interface{} {
	attrs := map[string]interface{}{
		"storage": map[string]interface{}{
			"physical_path": physicalPath,
		},
		"item": map[string]interface{}{
			"layout":    layout,
			"data_type": "table",
			"format":    formatName,
		},
		"type_info": map[string]interface{}{
			"table": map[string]interface{}{
				"fields": fields,
			},
		},
	}
	if refs != nil {
		attrs["item"].(map[string]interface{})["refs"] = refs
	}
	if spatial != nil {
		attrs["capabilities"] = map[string]interface{}{"spatial": spatial}
	}
	return attrs
}
