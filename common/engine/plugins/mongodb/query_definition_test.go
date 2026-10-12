package mongodb

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func savedMongoDefinition(query string) plugin.QueryDefinition {
	path := plugin.EngineCatalogPath{EngineID: 5, Version: "v1", Segments: []plugin.EngineCatalogSegment{
		{Term: "database", Kind: plugin.EngineCatalogRoleBranch, Name: "synthetic"},
		{Term: "collection", Kind: plugin.EngineCatalogRoleLeaf, Name: "source"},
	}}
	return plugin.QueryDefinition{Request: plugin.QueryRequest{EngineID: 5, Language: "mql", Query: query, TargetPath: &path, Options: plugin.QueryOptions{ReadOnly: true}},
		SourcePath: path, SchemaCoverage: plugin.QuerySchemaCoverageSampled,
		Source: &datatype.TableInfo{Fields: []datatype.FieldInfo{
			{Name: "_id", Type: datatype.FieldTypeString},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
			{Name: "payload", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON, Nullable: true},
			{Name: "profile", Type: datatype.FieldTypeJSON, Nullable: true},
			{Name: "profile.title", Path: []string{"profile", "title"}, Type: datatype.FieldTypeString, Nullable: true},
		}},
	}
}

func TestMongoSavedProjectionIsPureAndKeepsPartialShape(t *testing.T) {
	for _, tc := range []struct {
		name, project string
		expected      map[string]datatype.FieldType
	}{
		{"include", `{"name":1,"payload":1,"_id":0}`, map[string]datatype.FieldType{"name": datatype.FieldTypeString, "payload": datatype.FieldTypeArray}},
		{"alias", `{"label":"$name","items":"$payload","_id":0}`, map[string]datatype.FieldType{"label": datatype.FieldTypeString, "items": datatype.FieldTypeArray}},
		{"exclude", `{"payload":0,"profile":0}`, map[string]datatype.FieldType{"_id": datatype.FieldTypeString, "name": datatype.FieldTypeString}},
		{"implicit_id", `{"name":1}`, map[string]datatype.FieldType{"_id": datatype.FieldTypeString, "name": datatype.FieldTypeString}},
		{"object", `{"info":"$profile","_id":0}`, map[string]datatype.FieldType{"info": datatype.FieldTypeJSON}},
		{"nested_alias", `{"label":"$profile.title","_id":0}`, map[string]datatype.FieldType{"label": datatype.FieldTypeString}},
		{"nested_output", `{"result.title":"$profile.title","_id":0}`, map[string]datatype.FieldType{"result": datatype.FieldTypeJSON}},
		{"unobserved", `{"new_field":"$unseen","_id":0}`, map[string]datatype.FieldType{"new_field": datatype.FieldTypeUnknown}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := savedMongoDefinition(`{"aggregate":"source","pipeline":[{"$project":` + tc.project + `}]}`)
			before, _ := json.Marshal(input)
			// Nil receiver cannot reach any database client or runtime method.
			var provider *MongoDBPlugin
			result := provider.CheckQueryDefinition(input)
			if result.Output == nil || result.SchemaCoverage != plugin.QuerySchemaCoverageSampled {
				t.Fatalf("result=%+v", result)
			}
			actual := map[string]datatype.FieldType{}
			for _, field := range result.Output.Fields {
				actual[field.Name] = field.Type
				if field.Name == "items" && field.ElementType != datatype.FieldTypeJSON {
					t.Fatal("object array shape lost")
				}
			}
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Fatalf("fields=%+v want=%+v", actual, tc.expected)
			}
			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("saved facts mutated")
			}
			for _, check := range result.Checks {
				if check.Status == plugin.DefinitionUnsupported {
					t.Fatalf("sample asserted absence: %+v", check)
				}
			}
			if tc.name == "unobserved" && !hasMongoDefinitionCode(result, "query_source_field_not_observed") {
				t.Fatal("missing incomplete coverage diagnosis")
			}
		})
	}
}

func hasMongoDefinitionCode(result plugin.QueryDefinitionReport, code string) bool {
	for _, check := range result.Checks {
		if check.Code == code {
			return true
		}
	}
	return false
}

func TestMongoSavedProjectionUnknownAndDefiniteRejectionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, query, code string
		unsupported       bool
	}{
		{"multiple_stages", `{"aggregate":"source","pipeline":[{"$match":{}},{"$project":{"name":1}}]}`, "query_definition_not_checked", false},
		{"expression", `{"aggregate":"source","pipeline":[{"$project":{"name":{"$ifNull":["$name",null]}}}]}`, "query_definition_not_checked", false},
		{"lookup", `{"aggregate":"source","pipeline":[{"$lookup":{"from":"other"}}]}`, "query_definition_not_checked", false},
		{"find", `{"find":"source","projection":{"name":1}}`, "query_definition_not_checked", false},
		{"write", `{"aggregate":"source","pipeline":[{"$out":"other"}]}`, "query_definition_invalid", true},
		{"mixed", `{"aggregate":"source","pipeline":[{"$project":{"name":1,"payload":0}}]}`, "query_definition_invalid", true},
		{"collision", `{"aggregate":"source","pipeline":[{"$project":{"profile":1,"profile.title":1}}]}`, "query_definition_invalid", true},
		{"different_collection", `{"aggregate":"other","pipeline":[{"$project":{"name":1}}]}`, "query_snapshot_binding_mismatch", true},
		{"syntax", `not json`, "query_definition_invalid", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := (*MongoDBPlugin)(nil).CheckQueryDefinition(savedMongoDefinition(tc.query))
			if result.Output != nil || !hasMongoDefinitionCode(result, tc.code) {
				t.Fatalf("result=%+v", result)
			}
			if (result.Checks[len(result.Checks)-1].Status == plugin.DefinitionUnsupported) != tc.unsupported {
				t.Fatalf("status=%+v", result)
			}
		})
	}
}

func TestMongoSavedProjectionBindingCoverageAndParameters(t *testing.T) {
	input := savedMongoDefinition(`{"aggregate":{"$param":"collection"},"pipeline":[{"$project":{"label":"$name","_id":0}}]}`)
	input.Request.Options.Parameters = map[string]interface{}{"collection": "source"}
	result := (*MongoDBPlugin)(nil).CheckQueryDefinition(input)
	if result.Output == nil || result.Output.GetField("label") == nil {
		t.Fatalf("parameter binding=%+v", result)
	}
	input.Request.Options.Parameters["unused"] = "x"
	if !hasMongoDefinitionCode((*MongoDBPlugin)(nil).CheckQueryDefinition(input), "query_definition_invalid") {
		t.Fatal("unused parameter accepted")
	}
	delete(input.Request.Options.Parameters, "unused")
	for _, coverage := range []string{plugin.QuerySchemaCoverageUnknown, plugin.QuerySchemaCoverageComplete, "invalid"} {
		input.SchemaCoverage = coverage
		result := (*MongoDBPlugin)(nil).CheckQueryDefinition(input)
		if result.SchemaCoverage != plugin.QuerySchemaCoverageUnknown {
			t.Fatal("coverage upgraded")
		}
	}
	input.Source = nil
	result = (*MongoDBPlugin)(nil).CheckQueryDefinition(input)
	if result.Output == nil || result.Output.GetField("label").Type != datatype.FieldTypeUnknown {
		t.Fatal("missing snapshot invented type")
	}
	input.SourcePath.EngineID = 6
	if !hasMongoDefinitionCode((*MongoDBPlugin)(nil).CheckQueryDefinition(input), "query_snapshot_binding_mismatch") {
		t.Fatal("wrong engine snapshot accepted")
	}
}
