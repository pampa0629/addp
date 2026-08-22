package mongodb

import (
	"fmt"
	"os"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestIntegrationSampleDynamicSchemaPersistsPersonsNestedFacts(t *testing.T) {
	if os.Getenv("ADDP_MONGODB_SCHEMA_E2E") != "1" {
		t.Skip("set ADDP_MONGODB_SCHEMA_E2E=1 to run against Business MongoDB")
	}

	provider := &MongoDBPlugin{}
	facts, err := provider.SampleDynamicSchema(t.Context(), plugin.ConnectionInfo{
		"host": "localhost", "port": 27017, "user": "admin", "password": "admin_password", "auth_source": "admin",
	}, plugin.CatalogPath{
		Version:  "v1",
		EngineID: 11,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: "Outdoor"},
			{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Name: "Persons"},
		},
	}, plugin.CatalogFactsOptions{IncludeStatistics: true, IncludeIndexes: true, SampleSize: 100})
	if err != nil {
		t.Fatalf("SampleDynamicSchema() error = %v", err)
	}
	table := plugin.CatalogFactsTableInfo(facts)
	if table == nil {
		t.Fatal("SampleDynamicSchema() returned no table facts")
	}
	assertSampledField := func(name string, fieldType datatype.FieldType) *datatype.FieldInfo {
		t.Helper()
		field := table.GetField(name)
		if field == nil || field.Type != fieldType {
			t.Fatalf("field %q = %#v, want type %q", name, field, fieldType)
		}
		return field
	}
	if scalarField := assertSampledField("userInfo.nickName", datatype.FieldTypeString); scalarField.ElementType != "" {
		t.Fatalf("userInfo.nickName element type = %q, want omitted", scalarField.ElementType)
	}
	arrayField := assertSampledField("entriedOutdoors", datatype.FieldTypeArray)
	if arrayField.ElementType != datatype.FieldTypeJSON {
		t.Fatalf("entriedOutdoors element type = %q, want json", arrayField.ElementType)
	}
	assertSampledField("entriedOutdoors.title", datatype.FieldTypeString)
}

func TestMapMongoArrayElementTypeOmitsUnknownSample(t *testing.T) {
	if got := mapMongoArrayElementType(""); got != "" {
		t.Fatalf("empty element type = %q, want omitted", got)
	}
	if got := mapMongoArrayElementType("null"); got != "" {
		t.Fatalf("null element type = %q, want omitted", got)
	}
	if got := mapMongoArrayElementType("object"); got != datatype.FieldTypeJSON {
		t.Fatalf("object element type = %q, want json", got)
	}
}

func TestCollectMongoFieldStatsIncludesNestedObjectAndArrayPaths(t *testing.T) {
	stats := make(map[string]*mongoFieldStat)
	document := bson.M{
		"title": bson.M{"whole": "活动标题"},
		"members": primitive.A{
			bson.M{"userInfo": bson.M{"nickName": "PiPi"}, "entryInfo": bson.M{"status": "领队"}},
			bson.M{"userInfo": bson.M{"nickName": "小朱"}},
		},
	}
	for key, value := range document {
		collectMongoFieldStats(stats, []string{key}, value, 1)
	}

	assertMongoFieldPath(t, stats, "title", "object", []string{"title"})
	assertMongoFieldPath(t, stats, "title.whole", "string", []string{"title", "whole"})
	assertMongoFieldPath(t, stats, "members", "array", []string{"members"})
	if stats["members"].ElementType != "object" {
		t.Fatalf("members element type = %q, want object", stats["members"].ElementType)
	}
	assertMongoFieldPath(t, stats, "members.userInfo.nickName", "string", []string{"members", "userInfo", "nickName"})
	assertMongoFieldPath(t, stats, "members.entryInfo.status", "string", []string{"members", "entryInfo", "status"})
}

func TestCollectMongoFieldStatsUsesStableMapOrderAtFieldLimit(t *testing.T) {
	document := bson.M{}
	for index := mongoSchemaMaxFields + 4; index >= 0; index-- {
		document[fmt.Sprintf("field_%03d", index)] = index
	}

	stats := make(map[string]*mongoFieldStat)
	collectMongoDocumentFields(stats, nil, document, 0)

	if len(stats) != mongoSchemaMaxFields {
		t.Fatalf("field count = %d, want %d", len(stats), mongoSchemaMaxFields)
	}
	if stats["field_000"] == nil || stats["field_199"] == nil {
		t.Fatalf("stable field prefix missing: %#v", stats)
	}
	if stats["field_200"] != nil || stats["field_204"] != nil {
		t.Fatalf("fields after deterministic limit must be excluded: %#v", stats)
	}
}

func TestCollectMongoDocumentFieldsSkipsGeneratedRecordKeys(t *testing.T) {
	stats := make(map[string]*mongoFieldStat)
	document := bson.M{
		"members": bson.M{
			"ogNmG5A5iITPD0IuDhUCFN8nhbGE": bson.M{
				"userInfo": bson.M{"nickName": "PiPi"},
			},
		},
	}
	collectMongoDocumentFields(stats, nil, map[string]interface{}(document), 0)

	if stats["members"] == nil {
		t.Fatal("members object should remain a schema field")
	}
	if stats["members.ogNmG5A5iITPD0IuDhUCFN8nhbGE.userInfo.nickName"] != nil {
		t.Fatal("generated record key must not become a query field")
	}
}

func assertMongoFieldPath(t *testing.T, stats map[string]*mongoFieldStat, name, fieldType string, path []string) {
	t.Helper()
	stat := stats[name]
	if stat == nil || stat.Type != fieldType {
		t.Fatalf("field %s = %#v, want type %s", name, stat, fieldType)
	}
	if len(stat.Path) != len(path) {
		t.Fatalf("field %s path = %#v, want %#v", name, stat.Path, path)
	}
	for index := range path {
		if stat.Path[index] != path[index] {
			t.Fatalf("field %s path = %#v, want %#v", name, stat.Path, path)
		}
	}
}
