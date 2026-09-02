package planner

import (
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/mongodbextendedjson"
)

func TestParseAndBuildEncodedRecordExportPlan(t *testing.T) {
	config := map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator": "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=81", "data_type": "unknown", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/2/path/exports?type=directory", "name": "Persons.ejsonl",
			"data_type": "unknown", "representation": "encoded", "format": "mongodb_extended_jsonl",
			"policy": map[string]interface{}{"apply_mode": "replace"},
		},
	}
	spec, err := ParseEncodedRecordExportTaskSpec(config, 256)
	if err != nil {
		t.Fatalf("ParseEncodedRecordExportTaskSpec() error = %v", err)
	}
	caps := engineplugin.NewDynamicSchemaCapabilities("mongodb")
	caps.Storage.Store.EncodedRecordReadSession = &engineplugin.EncodedRecordReadSessionCapability{Formats: []string{string(format.FormatMongoDBExtendedJSONL)}}
	result, err := BuildEncodedRecordExportPlan(spec, StaticEngineResolver{
		11: {Type: "mongodb", EngineID: 11, Capabilities: &caps},
		2:  {Type: "nfs", EngineID: 2},
	})
	if err != nil {
		t.Fatalf("BuildEncodedRecordExportPlan() error = %v", err)
	}
	if result.Plan.BatchSize != 256 || result.Plan.Format != "mongodb_extended_jsonl" {
		t.Fatalf("plan = %#v", result.Plan)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "Outdoor/Persons" {
		t.Fatalf("source path = %q", got)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "exports/Persons.ejsonl" {
		t.Fatalf("target path = %q", got)
	}
}

func TestEncodedRecordExportRejectsTableSource(t *testing.T) {
	spec := minimalEncodedRecordExportSpec()
	spec.Source.Locator = "addp://engine/11/path/public/people?type=table"
	if err := validateEncodedRecordExportSpec(spec); err == nil {
		t.Fatal("validateEncodedRecordExportSpec() error = nil, want collection error")
	}
}

func TestEncodedRecordSourceFieldsUsesMetaTableSnapshot(t *testing.T) {
	spec := minimalEncodedRecordExportSpec()
	spec.Source.Attributes = map[string]interface{}{
		"type_info": map[string]interface{}{
			"table": datatype.TableInfoPayload(&datatype.TableInfo{
				Name: "Persons",
				Fields: []datatype.FieldInfo{
					{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
					{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
				},
			}),
		},
	}

	fields := EncodedRecordSourceFields(spec)
	if len(fields) != 2 {
		t.Fatalf("fields = %#v, want current Meta fields", fields)
	}
	if fields[1].Name != "userInfo.phone" || fields[1].Type != datatype.FieldTypeString {
		t.Fatalf("protected field = %#v", fields[1])
	}
	fields[1].Name = "mutated"
	if got := EncodedRecordSourceFields(spec)[1].Name; got != "userInfo.phone" {
		t.Fatalf("source fields were not cloned, got %q", got)
	}
}

func minimalEncodedRecordExportSpec() EncodedRecordExportTaskSpec {
	return EncodedRecordExportTaskSpec{
		Runtime: RuntimeSpec{Boundary: runtimeBoundaryBounded}, Load: LoadSpec{Mode: loadModeSnapshot}, BatchSize: 100,
		Source: EndpointSpec{Locator: "addp://engine/11/path/Outdoor/Persons?type=collection", DataType: dataTypeUnknown, Representation: representationNative},
		Target: EndpointSpec{ParentLocator: "addp://engine/2/path/exports?type=directory", Name: "Persons.ejsonl", DataType: dataTypeUnknown, Representation: representationEncoded, Format: format.FormatMongoDBExtendedJSONL, Policy: map[string]interface{}{"apply_mode": "replace"}},
	}
}
