package planner

import (
	"github.com/addp/common/datatype"
	"strconv"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/geojson"
	_ "github.com/addp/common/format/plugins/json"
	_ "github.com/addp/common/format/plugins/parquet"
	_ "github.com/addp/common/format/plugins/pdf"
	_ "github.com/addp/common/format/plugins/shapefile"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/executor"
)

func TestParseInfraLocatorURIForMinioObject(t *testing.T) {
	loc, err := ParseInfraLocatorURI("addp-infra://minio/manager/tenant_7/import/20260619/upload/roads.shp?type=object")
	if err != nil {
		t.Fatalf("ParseInfraLocatorURI failed: %v", err)
	}
	if loc.Kind != "minio" || loc.Namespace != "manager" || loc.Type != resourcetree.TypeObject {
		t.Fatalf("locator = %#v, want minio manager object locator", loc)
	}
	if got := strings.Join(loc.Path, "/"); got != "tenant_7/import/20260619/upload/roads.shp" {
		t.Fatalf("locator path = %q, want tenant_7/import/20260619/upload/roads.shp", got)
	}

	catalogPath, err := loc.CatalogPath()
	if err != nil {
		t.Fatalf("CatalogPath failed: %v", err)
	}
	if catalogPath.EngineID != 0 {
		t.Fatalf("catalog engine id = %d, want 0 for infra resolver path", catalogPath.EngineID)
	}
	if got := catalogPath.StringPath(); got != "manager/tenant_7/import/20260619/upload/roads.shp" {
		t.Fatalf("catalog path = %q, want manager/tenant_7/import/20260619/upload/roads.shp", got)
	}
}

func TestBuildTableTransferPlanForInfraObjectToNativeTable(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Locator = infraObjectLocator("manager", "tenant_7/import/20260619/upload/roads.shp")
	spec.Source.Format = format.FormatShapefile
	spec.Source.Options = map[string]interface{}{"encoding": "GBK"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		0: {Type: "minio"},
		2: {Type: "postgresql"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.SourceEngineType != "minio" || result.TargetEngineType != "postgresql" {
		t.Fatalf("engine types = %q -> %q, want minio -> postgresql", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Plan.Source.Kind != executor.TableEndpointEncoded || result.Plan.Target.Kind != executor.TableEndpointNative {
		t.Fatalf("endpoint kinds = %q -> %q, want encoded -> native", result.Plan.Source.Kind, result.Plan.Target.Kind)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "manager/tenant_7/import/20260619/upload/roads.shp" {
		t.Fatalf("source path = %q, want infra manager object path", got)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "public/roads" {
		t.Fatalf("target path = %q, want public/roads", got)
	}
	if result.Plan.Source.ParseOptions == nil || result.Plan.Source.ParseOptions.Encoding != "GBK" {
		t.Fatalf("source parse options = %#v, want GBK encoding", result.Plan.Source.ParseOptions)
	}
}

func TestBuildTableTransferPlanForNativeTableToInfraPrefix(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.ParentLocator = infraPrefixLocator("manager", "tenant_7/export/20260619/export-uuid")
	spec.Target.Name = "roads"
	spec.Target.Format = format.FormatCSV

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		0: {Type: "minio"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.SourceEngineType != "postgresql" || result.TargetEngineType != "minio" {
		t.Fatalf("engine types = %q -> %q, want postgresql -> minio", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Plan.Source.Kind != executor.TableEndpointNative || result.Plan.Target.Kind != executor.TableEndpointEncoded {
		t.Fatalf("endpoint kinds = %q -> %q, want native -> encoded", result.Plan.Source.Kind, result.Plan.Target.Kind)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "manager/tenant_7/export/20260619/export-uuid/roads.csv" {
		t.Fatalf("target path = %q, want infra manager export object path", got)
	}
}

func TestBuildTableTransferPlanForNativeTableToEncodedFile(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.BatchSize = 500
	spec.Target.Policy = map[string]interface{}{"apply_mode": "replace"}
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
	setObjectTarget(&spec, 2, "exports", "roads.csv")

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
	setObjectTarget(&spec, 2, "exports", "lake3")

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
	setFileTarget(&spec, 2, "exports/roads")

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
	setFileTarget(&spec, 2, "exports/roads.csv")

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := minimalNativeToEncodedSpec()
			spec.Target.Format = format.FormatJSON
			spec.Target.Options = tt.options
			setFileTarget(&spec, 2, "exports/roads")

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

func TestBuildTableTransferPlanAppendsGeoJSONTargetExtension(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatGeoJSON
	setFileTarget(&spec, 2, "exports/roads")

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "exports/roads.geojson" {
		t.Fatalf("target path = %q, want exports/roads.geojson", got)
	}
}

func TestBuildTableTransferPlanAllowsJSONTableWriter(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatJSON
	setFileTarget(&spec, 2, "exports/roads.jsonl")
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

func TestBuildTableTransferPlanPassesGeoJSONWriteOptions(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatGeoJSON
	setFileTarget(&spec, 2, "exports/roads.geojson")
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geom",
	}
	sourceSpatial := datatype.NewSingleGeometrySpatialInfo("geom", "Point", 3857, 2)
	sourceSpatial.Extent = &datatype.BoundingBox{1000000, 2000000, 1100000, 2100000}
	spec.Source.Attributes = tableSourceAttributes("single", "table", "public/roads", nil, []map[string]interface{}{
		{"name": "geom", "type": "geometry"},
	}, datatype.SpatialInfoPayload(sourceSpatial))

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {
			Type: "postgresql",
			Capabilities: &engineplugin.EngineCapabilities{
				SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
				EngineType:    "postgresql",
				Storage: &engineplugin.StorageCapabilities{
					Store: &engineplugin.StoreCapability{
						TableReadSpatialTransform: true,
					},
				},
			},
		},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Target.Format != format.FormatGeoJSON {
		t.Fatalf("target format = %q, want geojson", result.Plan.Target.Format)
	}
	if result.Plan.Target.FormatOptions == nil || result.Plan.Target.FormatOptions.ExtraParams["geometry_field"] != "geom" {
		t.Fatalf("write options = %#v, want geojson geometry write options", result.Plan.Target.FormatOptions)
	}
	if result.Plan.Source.ReadOptions["geometry_encoding"] != "geojson" || result.Plan.Source.ReadOptions["geometry_field"] != "geom" {
		t.Fatalf("read options = %#v, want geojson geometry read options", result.Plan.Source.ReadOptions)
	}
	if result.Plan.Source.ReadOptions["geometry_target_srid"] != 4326 || result.Plan.Source.ReadOptions["geometry_transform_policy"] != "required" {
		t.Fatalf("read options = %#v, want required spatial transform hints", result.Plan.Source.ReadOptions)
	}
	if result.Plan.Source.SpatialInfo == nil || result.Plan.Source.SpatialInfo.PrimarySRIDValue() != 4326 ||
		result.Plan.Source.SpatialInfo.PrimaryCRSRef() != "EPSG:4326" {
		t.Fatalf("source spatial info = %#v, want target CRS after source-side transform", result.Plan.Source.SpatialInfo)
	}
	if result.Plan.Source.SpatialInfo.Extent != nil {
		t.Fatalf("source spatial extent = %#v, want cleared extent after source-side transform", result.Plan.Source.SpatialInfo.Extent)
	}
	if len(result.Plan.Transforms) != 0 {
		t.Fatalf("transforms = %#v, want no synthetic spatial transform on native source", result.Plan.Transforms)
	}
}

func TestBuildTableTransferPlanRequestsEWKBForNativeSpatialSourceToShapefile(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatShapefile
	setFileTarget(&spec, 2, "exports/roads.shp")
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geom",
	}
	spec.Source.Attributes = tableSourceAttributes("single", "table", "public/roads", nil, []map[string]interface{}{
		{"name": "geom", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2)))

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ReadOptions[engineplugin.TableReadHintGeometryEncoding] != "ewkb" {
		t.Fatalf("source read options = %#v, want ewkb geometry encoding", result.Plan.Source.ReadOptions)
	}
	if len(result.Plan.Transforms) != 0 {
		t.Fatalf("transforms = %#v, want no reproject for shapefile target", result.Plan.Transforms)
	}
}

func TestBuildTableTransferPlanAllowsNativeGeoJSONExportWhenSourceAlready4326WithoutReadTransform(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatGeoJSON
	setFileTarget(&spec, 2, "exports/roads.geojson")
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geom",
	}
	spec.Source.Attributes = tableSourceAttributes("single", "table", "public/roads", nil, []map[string]interface{}{
		{"name": "geom", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2)))

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: nativeTableSourceBinding("native_table_source"),
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ReadOptions[engineplugin.TableReadHintGeometryEncoding] != "ewkb" {
		t.Fatalf("source read options = %#v, want ewkb geometry encoding", result.Plan.Source.ReadOptions)
	}
	if len(result.Plan.Transforms) != 0 {
		t.Fatalf("transforms = %#v, want no transform for 4326 native source", result.Plan.Transforms)
	}
}

func TestBuildTableTransferPlanRejectsNativeGeoJSONExportWithoutReadTransformWhenCRSDiffers(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatGeoJSON
	setFileTarget(&spec, 2, "exports/roads.geojson")
	spec.Source.Attributes = tableSourceAttributes("single", "table", "public/roads", nil, []map[string]interface{}{
		{"name": "geom", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geom", "Point", 3857, 2)))

	_, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: nativeTableSourceBinding("native_table_source"),
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableTransferPlan succeeded, want missing read_transform error")
	}
	if !strings.Contains(err.Error(), "does not support spatial transform") {
		t.Fatalf("error = %q, want missing spatial transform capability error", err)
	}
}

func TestBuildTableTransferPlanAddsSpatialReprojectTransformForEncodedGeoJSONSource(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Locator = fileLocator(1, "imports/roads.shp")
	spec.Source.Format = format.FormatShapefile
	spec.Source.Attributes = tableSourceAttributes("multi", "shapefile", "imports/roads.shp", []map[string]interface{}{
		{"path": "imports/roads.shp", "role": "main", "extension": ".shp", "required": true, "primary": true},
		{"path": "imports/roads.shx", "role": "index", "extension": ".shx", "required": true},
		{"path": "imports/roads.dbf", "role": "attributes", "extension": ".dbf", "required": true},
	}, []map[string]interface{}{
		{"name": "geom", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geom", "MultiPolygon", 3857, 2)))
	setFileTarget(&spec, 2, "exports/roads.geojson")
	spec.Target.Format = format.FormatGeoJSON
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geom",
	}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ParseOptions == nil || result.Plan.Source.ParseOptions.GeometryEncoding != format.GeometryEncodingEWKB {
		t.Fatalf("source parse options = %#v, want ewkb for spatial batch transform", result.Plan.Source.ParseOptions)
	}
	if len(result.Plan.Transforms) != 1 {
		t.Fatalf("transforms = %#v, want one synthetic spatial transform", result.Plan.Transforms)
	}
	transform := result.Plan.Transforms[0]
	if transform.Type != "spatial_reproject" || transform.SpatialReproject == nil {
		t.Fatalf("transform = %#v, want spatial reproject plan", transform)
	}
	if !transform.SpatialReproject.Reproject || transform.SpatialReproject.GeometryColumn != "geom" {
		t.Fatalf("spatial reproject plan = %#v, want reprojected geom batch", transform.SpatialReproject)
	}
	if transform.SpatialReproject.SourceCRS != "EPSG:3857" || transform.SpatialReproject.TargetCRS != "EPSG:4326" {
		t.Fatalf("spatial reproject plan = %#v, want CRS conversion", transform.SpatialReproject)
	}
}

func TestBuildTableTransferPlanSkipsSpatialReprojectForEncodedGeoJSONSourceAlready4326(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Locator = fileLocator(1, "imports/roads.shp")
	spec.Source.Format = format.FormatShapefile
	spec.Source.Attributes = tableSourceAttributes("multi", "shapefile", "imports/roads.shp", []map[string]interface{}{
		{"path": "imports/roads.shp", "role": "main", "extension": ".shp", "required": true, "primary": true},
		{"path": "imports/roads.shx", "role": "index", "extension": ".shx", "required": true},
		{"path": "imports/roads.dbf", "role": "attributes", "extension": ".dbf", "required": true},
	}, []map[string]interface{}{
		{"name": "geom", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geom", "MultiPolygon", 4326, 2)))
	setFileTarget(&spec, 2, "exports/roads.geojson")
	spec.Target.Format = format.FormatGeoJSON
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geom",
	}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ParseOptions == nil || result.Plan.Source.ParseOptions.GeometryEncoding != format.GeometryEncodingEWKB {
		t.Fatalf("source parse options = %#v, want ewkb for GeoJSON writer", result.Plan.Source.ParseOptions)
	}
	if len(result.Plan.Transforms) != 0 {
		t.Fatalf("transforms = %#v, want no spatial reproject when source CRS already matches GeoJSON CRS", result.Plan.Transforms)
	}
}

func TestBuildTableTransferPlanRequestsEWKBForGeoJSONToShapefile(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Locator = fileLocator(1, "imports/roads.geojson")
	spec.Source.Format = format.FormatGeoJSON
	spec.Source.Attributes = tableSourceAttributes("single", "geojson", "imports/roads.geojson", nil, []map[string]interface{}{
		{"name": "geometry", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geometry", "Point", 4326, 2)))
	setFileTarget(&spec, 2, "exports/roads.shp")
	spec.Target.Format = format.FormatShapefile
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geometry",
	}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ParseOptions == nil || result.Plan.Source.ParseOptions.GeometryEncoding != format.GeometryEncodingEWKB {
		t.Fatalf("source parse options = %#v, want ewkb for spatial encoded transfer", result.Plan.Source.ParseOptions)
	}
	if len(result.Plan.Transforms) != 0 {
		t.Fatalf("transforms = %#v, want no reproject for shapefile target", result.Plan.Transforms)
	}
}

func TestBuildTableTransferPlanKeepsNativeEncodingForGeoJSONToGeoJSON(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Locator = fileLocator(1, "imports/roads.geojson")
	spec.Source.Format = format.FormatGeoJSON
	spec.Source.Attributes = tableSourceAttributes("single", "geojson", "imports/roads.geojson", nil, []map[string]interface{}{
		{"name": "geometry", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geometry", "Point", 4326, 2)))
	setFileTarget(&spec, 2, "exports/roads.geojson")
	spec.Target.Format = format.FormatGeoJSON
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geometry",
	}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ParseOptions == nil || result.Plan.Source.ParseOptions.GeometryEncoding != format.GeometryEncodingGeoJSON {
		t.Fatalf("source parse options = %#v, want native geojson geometry encoding", result.Plan.Source.ParseOptions)
	}
	if len(result.Plan.Transforms) != 0 {
		t.Fatalf("transforms = %#v, want no transform for native GeoJSON passthrough", result.Plan.Transforms)
	}
}

func TestBuildTableTransferPlanRejectsGeoJSONToGeoJSONWhenSourceCRSUnknown(t *testing.T) {
	spatial := datatype.NewSingleGeometrySpatialInfo("geometry", "Point", 0, 2)
	spatial.Extent = &datatype.BoundingBox{12958175.1, 4855942.3, 12958200.2, 4855960.4}

	spec := minimalEncodedToEncodedSpec()
	spec.Source.Locator = fileLocator(1, "imports/projected.geojson")
	spec.Source.Format = format.FormatGeoJSON
	spec.Source.Attributes = tableSourceAttributes("single", "geojson", "imports/projected.geojson", nil, []map[string]interface{}{
		{"name": "geometry", "type": "geometry"},
	}, datatype.SpatialInfoPayload(spatial))
	setFileTarget(&spec, 2, "exports/projected.geojson")
	spec.Target.Format = format.FormatGeoJSON

	_, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableTransferPlan succeeded, want source CRS error")
	}
	if !strings.Contains(err.Error(), "source CRS is required") {
		t.Fatalf("error = %q, want source CRS requirement", err)
	}
}

func TestBuildTableTransferPlanKeepsNativeEncodingForShapefileToShapefile(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Locator = fileLocator(1, "imports/roads.shp")
	spec.Source.Format = format.FormatShapefile
	spec.Source.Attributes = tableSourceAttributes("multi", "shapefile", "imports/roads.shp", []map[string]interface{}{
		{"path": "imports/roads.shp", "role": "main", "extension": ".shp", "required": true, "primary": true},
		{"path": "imports/roads.shx", "role": "index", "extension": ".shx", "required": true},
		{"path": "imports/roads.dbf", "role": "attributes", "extension": ".dbf", "required": true},
	}, []map[string]interface{}{
		{"name": "geom", "type": "geometry"},
	}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2)))
	setFileTarget(&spec, 2, "exports/roads.shp")
	spec.Target.Format = format.FormatShapefile
	spec.Target.Options = map[string]interface{}{
		"geometry_field": "geom",
	}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildTableTransferPlan failed: %v", err)
	}
	if result.Plan.Source.ParseOptions == nil || result.Plan.Source.ParseOptions.GeometryEncoding != format.GeometryEncodingShapefileShape {
		t.Fatalf("source parse options = %#v, want native shapefile shape geometry encoding", result.Plan.Source.ParseOptions)
	}
	if len(result.Plan.Transforms) != 0 {
		t.Fatalf("transforms = %#v, want no transform for native Shapefile passthrough", result.Plan.Transforms)
	}
}

func TestBuildTableTransferPlanAllowsParquetTableWriter(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatParquet
	setFileTarget(&spec, 2, "exports/roads.parquet")

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
	setFileTarget(&spec, 2, "exports/roads.shp")
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
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load:    LoadSpec{Mode: loadModeSnapshot},
		Source: EndpointSpec{
			Locator:        "addp://engine/1/path/imports/roads.csv?type=file",
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
			Options:        map[string]interface{}{"header": true, "delimiter": ","},
		},
		Target: EndpointSpec{
			ParentLocator:  schemaLocator(2, "public"),
			Name:           "roads",
			DataType:       dataTypeTable,
			Representation: representationNative,
			Policy:         map[string]interface{}{"apply_mode": "append"},
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
	spec.Source.Locator = fileLocator(1, "imports/stale.csv")
	spec.Source.Attributes = tableSourceAttributes("single", "csv", "imports/meta_roads.csv", nil, []map[string]interface{}{
		{"name": "id", "type": "bigint", "nullable": false, "primary_key": true},
		{"name": "road_name", "type": "string", "nullable": true},
	}, nil)
	spec.Source.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})["native"] = map[string]interface{}{
		"delimiter": ",",
	}
	spec.Source.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})["name"] = "meta_roads"
	spec.Source.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})["kind"] = "view"
	spec.Source.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})["comment"] = "roads from meta"
	spec.Source.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})["size_bytes"] = int64(2048)
	spec.Source.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})["primary_key"] = []interface{}{"id"}
	spec.Source.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})["fields"].([]map[string]interface{})[0]["native_type"] = "int8"

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
	if result.Plan.Source.TableInfo == nil || len(result.Plan.Source.TableInfo.Fields) != 2 {
		t.Fatalf("source table info = %#v, want fields from Meta attributes", result.Plan.Source.TableInfo)
	}
	if result.Plan.Source.TableInfo.Fields[0].Type != datatype.FieldTypeBigInt || !result.Plan.Source.TableInfo.Fields[0].PrimaryKey {
		t.Fatalf("first source field = %#v, want standard bigint primary key field", result.Plan.Source.TableInfo.Fields[0])
	}
	if result.Plan.Source.TableInfo.Name != "meta_roads" || result.Plan.Source.TableInfo.Kind != "view" || result.Plan.Source.TableInfo.Comment != "roads from meta" {
		t.Fatalf("source table info facts = %#v, want standard table facts", result.Plan.Source.TableInfo)
	}
	if result.Plan.Source.TableInfo.SizeBytes == nil || *result.Plan.Source.TableInfo.SizeBytes != 2048 {
		t.Fatalf("source table info size = %#v, want 2048", result.Plan.Source.TableInfo.SizeBytes)
	}
	if result.Plan.Source.TableInfo.Fields[0].NativeType != "int8" || len(result.Plan.Source.TableInfo.PrimaryKey) != 1 || result.Plan.Source.TableInfo.PrimaryKey[0] != "id" {
		t.Fatalf("source table info field/key facts = %#v / %#v", result.Plan.Source.TableInfo.Fields[0], result.Plan.Source.TableInfo.PrimaryKey)
	}
	if result.Plan.Source.TableInfo.Native["delimiter"] != "," {
		t.Fatalf("source table info native = %#v, want delimiter", result.Plan.Source.TableInfo.Native)
	}
}

func TestBuildTableTransferPlanConsumesMetaMultiSourceRefsAndSpatialAttributes(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = ""
	spec.Source.Locator = fileLocator(1, "imports/stale.shp")
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
	if result.Plan.Source.TableInfo == nil || result.Plan.Source.SpatialInfo == nil {
		t.Fatalf("source table info = %#v, spatial = %#v, want spatial info from Meta attributes", result.Plan.Source.TableInfo, result.Plan.Source.SpatialInfo)
	}
	if result.Plan.Source.SpatialInfo.PrimaryGeometryName() != "shape" || result.Plan.Source.SpatialInfo.PrimaryGeometryType() != "MultiPolygon" {
		t.Fatalf("source spatial info = %#v, want primary geometry column from capabilities.spatial", result.Plan.Source.SpatialInfo)
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
	spec.Source.Locator = fileLocator(1, "imports/roads.shp")
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
	spec.Source.Locator = fileLocator(1, "datasets/stale")
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
	if result.Plan.Source.Layout != format.LayoutWhole {
		t.Fatalf("source layout = %q, want whole from Meta attributes", result.Plan.Source.Layout)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "datasets/lake_table" {
		t.Fatalf("source path = %q, want whole scope physical path", got)
	}
	if result.Plan.Source.TableInfo == nil || len(result.Plan.Source.TableInfo.Fields) != 2 {
		t.Fatalf("source table info = %#v, want fields from Meta attributes", result.Plan.Source.TableInfo)
	}
}

func TestValidateTransformSpecsAcceptsDecimalPrecisionAndScale(t *testing.T) {
	precision := 20
	scale := 10
	err := validateTransformSpecs([]TransformSpec{{
		Type: "field_mapping",
		Fields: []FieldMappingSpec{{
			Source:     "amount",
			Target:     "amount",
			TargetType: "decimal",
			Precision:  &precision,
			Scale:      &scale,
		}},
	}})
	if err != nil {
		t.Fatalf("validateTransformSpecs failed: %v", err)
	}

	fields := buildFieldMappingFields([]FieldMappingSpec{{
		Source:     "amount",
		Target:     "amount",
		TargetType: "decimal",
		Precision:  &precision,
		Scale:      &scale,
	}})
	if len(fields) != 1 || fields[0].Precision == nil || *fields[0].Precision != 20 || fields[0].Scale == nil || *fields[0].Scale != 10 {
		t.Fatalf("field mapping plan = %#v, want precision/scale 20/10", fields)
	}
}

func TestValidateTransformSpecsRejectsIncompleteDecimalPrecision(t *testing.T) {
	precision := 20
	err := validateTransformSpecs([]TransformSpec{{
		Type: "field_mapping",
		Fields: []FieldMappingSpec{{
			Target:     "amount",
			TargetType: "decimal",
			Precision:  &precision,
		}},
	}})
	if err == nil || !strings.Contains(err.Error(), "precision and scale must be provided together") {
		t.Fatalf("validateTransformSpecs error = %v, want precision/scale pair error", err)
	}
}

func TestBuildTableTransferPlanUsesMetaObjectWholePhysicalPathAsScope(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Format = ""
	spec.Source.Locator = objectLocator(1, "manager", "stale")
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
	if result.Plan.Source.Layout != format.LayoutWhole {
		t.Fatalf("source layout = %q, want whole", result.Plan.Source.Layout)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "manager/regression/codex-parquet-whole-20260521" {
		t.Fatalf("source path = %q, want manager/regression/codex-parquet-whole-20260521", got)
	}
}

func TestBuildTableTransferPlanRequestsEWKBForSpatialEncodedImportToNativeTarget(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Source.Format = format.FormatShapefile
	spec.Source.Locator = fileLocator(1, "imports/roads.shp")
	spec.Source.Options = map[string]interface{}{"encoding": "GBK"}

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: nativeTableTargetBinding("native_table_target"),
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
	spec.Source.Locator = fileLocator(1, "imports/roads.csv")

	result, err := BuildTableTransferPlan(spec, StaticEngineResolver{
		1: {Type: "nfs"},
		2: nativeTableTargetBinding("native_table_target"),
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
	spec.Source.Locator = fileLocator(1, "imports/roads.jsonl")
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
	setObjectTarget(&spec, 2, "exports", "roads.jsonl")

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
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load:    LoadSpec{Mode: loadModeSnapshot},
		Source: EndpointSpec{
			Locator:        tableLocator(1, "public", "roads"),
			DataType:       dataTypeTable,
			Representation: representationNative,
		},
		Target: EndpointSpec{
			ParentLocator:  schemaLocator(2, "gis"),
			Name:           "roads_copy",
			DataType:       dataTypeTable,
			Representation: representationNative,
			Policy:         map[string]interface{}{"apply_mode": "replace"},
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
	if !strings.Contains(err.Error(), "source encoded endpoint type") {
		t.Fatalf("error = %q, want encoded source endpoint type error", err)
	}
}

func TestBuildTableTransferPlanSplitsOverwritePolicy(t *testing.T) {
	spec := minimalEncodedToNativeSpec()
	spec.Target.Policy = map[string]interface{}{"apply_mode": "replace"}

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
	spec.Target.Policy = map[string]interface{}{"apply_mode": "append", "write_method": "insert"}

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

func TestParseTableExportTaskSpecRejectsLegacyEngineField(t *testing.T) {
	_, err := ParseTableExportTaskSpec(map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"engine":         map[string]interface{}{"id": 1},
			"locator":        tableLocator(1, "public", "roads"),
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": directoryLocator(2, "exports"),
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         "csv",
		},
	}, 1000)
	if err == nil {
		t.Fatal("ParseTableExportTaskSpec succeeded, want unknown field error")
	}
	if !strings.Contains(err.Error(), "unknown field \"engine\"") {
		t.Fatalf("error = %q, want unknown engine field error", err)
	}
}

func TestParseTableExportTaskSpecRequiresRuntimeBoundary(t *testing.T) {
	config := map[string]interface{}{
		"source": map[string]interface{}{
			"locator":        tableLocator(1, "public", "roads"),
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": directoryLocator(2, "exports"),
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         "csv",
		},
	}

	_, err := ParseTableExportTaskSpec(config, 1000)
	if err == nil {
		t.Fatal("ParseTableExportTaskSpec succeeded, want mode error")
	}
	if !strings.Contains(err.Error(), "runtime.boundary") {
		t.Fatalf("error = %q, want runtime boundary error", err)
	}
}

func TestParseTableExportTaskSpecAppliesFallbackBatchSize(t *testing.T) {
	spec, err := ParseTableExportTaskSpec(map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator":        tableLocator(1, "public", "roads"),
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": directoryLocator(2, "exports"),
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         "csv",
			"policy":         map[string]interface{}{"apply_mode": "replace"},
		},
	}, 2048)
	if err != nil {
		t.Fatalf("ParseTableExportTaskSpec failed: %v", err)
	}
	if spec.BatchSize != 2048 {
		t.Fatalf("batch size = %d, want 2048", spec.BatchSize)
	}
}

func TestParseTableExportTaskSpecPreservesSourceLocatorItemID(t *testing.T) {
	spec, err := ParseTableExportTaskSpec(map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator":        fileLocator(1, "imports/roads.shp") + "&item_id=12",
			"data_type":      "table",
			"representation": "encoded",
		},
		"target": map[string]interface{}{
			"parent_locator": schemaLocator(2, "public"),
			"name":           "roads",
			"data_type":      "table",
			"representation": "native",
			"policy":         map[string]interface{}{"apply_mode": "replace"},
		},
	}, 1000)
	if err != nil {
		t.Fatalf("ParseTableExportTaskSpec failed: %v", err)
	}
	if spec.Source.LocatorItemID() != 12 {
		t.Fatalf("source locator item id = %d, want 12", spec.Source.LocatorItemID())
	}
	if spec.Source.Attributes != nil {
		t.Fatalf("source attributes = %#v, want nil before MetaClient loading", spec.Source.Attributes)
	}
}

func TestParseTableExportTaskSpecRejectsEndpointAttributes(t *testing.T) {
	_, err := ParseTableExportTaskSpec(map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator":        fileLocator(1, "imports/roads.shp"),
			"data_type":      "table",
			"representation": "encoded",
			"attributes":     map[string]interface{}{"item": map[string]interface{}{"format": "shapefile"}},
		},
		"target": map[string]interface{}{
			"parent_locator": schemaLocator(2, "public"),
			"name":           "roads",
			"data_type":      "table",
			"representation": "native",
		},
	}, 1000)
	if err == nil {
		t.Fatal("ParseTableExportTaskSpec succeeded, want endpoint attributes error")
	}
	if !strings.Contains(err.Error(), "source locator item_id") {
		t.Fatalf("error = %q, want source locator item_id guidance", err)
	}
}

func minimalNativeToEncodedSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load:    LoadSpec{Mode: loadModeSnapshot},
		Source: EndpointSpec{
			Locator:        "addp://engine/1/path/public/roads?type=table",
			DataType:       dataTypeTable,
			Representation: representationNative,
		},
		Target: EndpointSpec{
			ParentLocator:  directoryLocator(2, "exports"),
			Name:           "roads.csv",
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
			Policy:         map[string]interface{}{"apply_mode": "replace"},
		},
	}
}

func nativeTableTargetBinding(engineType string) EngineBinding {
	return EngineBinding{
		Type: engineType,
		Capabilities: &engineplugin.EngineCapabilities{
			SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
			EngineType:    engineType,
			EngineFamily:  "tabular",
			Storage: &engineplugin.StorageCapabilities{
				Store: &engineplugin.StoreCapability{
					TableSpatialEncoding: &engineplugin.NativeTableSpatialEncodingCapability{
						GeometryWriteEncodings: []string{string(format.GeometryEncodingEWKB)},
					},
				},
			},
		},
	}
}

func nativeTableSourceBinding(engineType string) EngineBinding {
	return EngineBinding{
		Type: engineType,
		Capabilities: &engineplugin.EngineCapabilities{
			SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
			EngineType:    engineType,
			EngineFamily:  "tabular",
			Storage: &engineplugin.StorageCapabilities{
				Store: &engineplugin.StoreCapability{
					TableSpatialEncoding: &engineplugin.NativeTableSpatialEncodingCapability{
						GeometryReadEncodings: []string{string(format.GeometryEncodingEWKB)},
					},
				},
			},
		},
	}
}

func tableLocator(engineID uint, schema, table string) string {
	return "addp://engine/" + strconv.Itoa(int(engineID)) + "/path/" + schema + "/" + table + "?type=table"
}

func schemaLocator(engineID uint, schema string) string {
	return "addp://engine/" + strconv.Itoa(int(engineID)) + "/path/" + schema + "?type=schema"
}

func fileLocator(engineID uint, path string) string {
	return "addp://engine/" + strconv.Itoa(int(engineID)) + "/path/" + path + "?type=file"
}

func directoryLocator(engineID uint, path string) string {
	return "addp://engine/" + strconv.Itoa(int(engineID)) + "/path/" + path + "?type=directory"
}

func objectLocator(engineID uint, bucket, path string) string {
	return "addp://engine/" + strconv.Itoa(int(engineID)) + "/path/" + bucket + "/" + path + "?type=object"
}

func bucketLocator(engineID uint, bucket string) string {
	return "addp://engine/" + strconv.Itoa(int(engineID)) + "/path/" + bucket + "?type=bucket"
}

func infraObjectLocator(bucket, path string) string {
	return "addp-infra://minio/" + bucket + "/" + strings.Trim(path, "/") + "?type=object"
}

func infraPrefixLocator(bucket, prefix string) string {
	return "addp-infra://minio/" + bucket + "/" + strings.Trim(prefix, "/") + "?type=prefix"
}

func setFileTarget(spec *TableExportTaskSpec, engineID uint, fullPath string) {
	dir, name := splitTestPath(fullPath)
	spec.Target.Locator = ""
	spec.Target.ParentLocator = directoryLocator(engineID, dir)
	spec.Target.Name = name
}

func setObjectTarget(spec *TableExportTaskSpec, engineID uint, bucket, fullPath string) {
	prefix, name := splitTestPath(fullPath)
	spec.Target.Locator = ""
	if prefix == "" {
		spec.Target.ParentLocator = bucketLocator(engineID, bucket)
	} else {
		spec.Target.ParentLocator = "addp://engine/" + strconv.Itoa(int(engineID)) + "/path/" + bucket + "/" + prefix + "?type=prefix"
	}
	spec.Target.Name = name
}

func splitTestPath(fullPath string) (string, string) {
	parts := strings.Split(strings.Trim(fullPath, "/"), "/")
	if len(parts) == 0 {
		return "", ""
	}
	name := parts[len(parts)-1]
	if len(parts) == 1 {
		return "", name
	}
	return strings.Join(parts[:len(parts)-1], "/"), name
}

func minimalEncodedToNativeSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load:    LoadSpec{Mode: loadModeSnapshot},
		Source: EndpointSpec{
			Locator:        "addp://engine/1/path/imports/roads.csv?type=file",
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
		},
		Target: EndpointSpec{
			ParentLocator:  schemaLocator(2, "public"),
			Name:           "roads",
			DataType:       dataTypeTable,
			Representation: representationNative,
			Policy:         map[string]interface{}{"apply_mode": "replace"},
		},
	}
}

func minimalEncodedToEncodedSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded},
		Load:    LoadSpec{Mode: loadModeSnapshot},
		Source: EndpointSpec{
			Locator:        "addp://engine/1/path/imports/roads.csv?type=object",
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
		},
		Target: EndpointSpec{
			ParentLocator:  bucketLocator(2, "exports"),
			Name:           "roads.csv",
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
			Policy:         map[string]interface{}{"apply_mode": "replace"},
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
