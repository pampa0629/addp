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
	}, plugin.EngineCatalogPath{
		Version:  "v1",
		EngineID: 11,
		Segments: []plugin.EngineCatalogSegment{
			{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "Outdoor"},
			{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogKindCollection, Name: "Persons"},
		},
	}, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeIndexes: true, SampleSize: 100})
	if err != nil {
		t.Fatalf("SampleDynamicSchema() error = %v", err)
	}
	table := plugin.EngineCatalogFactsTableInfo(facts)
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

func TestIntegrationPreparedQueryReadSetAndExecutionUseOutdoorPersonsPlan(t *testing.T) {
	if os.Getenv("ADDP_MONGODB_SCHEMA_E2E") != "1" {
		t.Skip("set ADDP_MONGODB_SCHEMA_E2E=1 to run against Business MongoDB")
	}
	provider := &MongoDBPlugin{}
	prepared, err := provider.PrepareQuery(t.Context(), plugin.ConnectionInfo{
		"host": "localhost", "port": 27017, "user": "admin", "password": "admin_password", "auth_source": "admin", "database": "Outdoor",
	}, plugin.QueryRequest{
		EngineID: 11,
		Language: "mql",
		Query:    `{"find":"Persons","filter":{},"limit":1}`,
		Options:  plugin.QueryOptions{ReadOnly: true, Limit: 1},
	})
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}
	analysis, err := prepared.Analysis(t.Context())
	if err != nil || analysis.SchemaCoverage != plugin.QuerySchemaCoverageUnknown || len(analysis.Diagnostics) != 0 {
		t.Fatalf("PreparedQuery.Analysis() = %#v, error = %v", analysis, err)
	}
	readSet, err := prepared.ReadSet(t.Context())
	if err != nil {
		t.Fatalf("PreparedQuery.ReadSet() error = %v", err)
	}
	if len(readSet.Paths) != 1 || readSet.Paths[0].StringPath() != "Outdoor/Persons" {
		t.Fatalf("read set paths = %#v", readSet.Paths)
	}
	lineage, err := prepared.OutputLineage(t.Context())
	if err != nil {
		t.Fatalf("PreparedQuery.OutputLineage() error = %v", err)
	}
	if len(lineage.Sources) != 1 || !lineage.Sources[0].IdentityOutput || lineage.Sources[0].OpaqueOutput || len(lineage.Sources[0].Fields) == 0 {
		t.Fatalf("output lineage = %#v", lineage)
	}
	result, err := prepared.Execute(t.Context())
	if err != nil {
		t.Fatalf("PreparedQuery.Execute() error = %v", err)
	}
	if len(result.Rows) > 1 {
		t.Fatalf("row count = %d, want at most 1", len(result.Rows))
	}
}

func TestIntegrationPreparedAggregateOutputLineageUsesOutdoorTransferProjection(t *testing.T) {
	if os.Getenv("ADDP_MONGODB_SCHEMA_E2E") != "1" {
		t.Skip("set ADDP_MONGODB_SCHEMA_E2E=1 to run against Business MongoDB")
	}
	provider := &MongoDBPlugin{}
	prepared, err := provider.PrepareQuery(t.Context(), plugin.ConnectionInfo{
		"host": "localhost", "port": 27017, "user": "admin", "password": "admin_password", "auth_source": "admin", "database": "Outdoor",
	}, plugin.QueryRequest{
		EngineID: 11,
		Language: "mql",
		Query: `{"aggregate":"Persons","pipeline":[
			{"$match":{"_id":{"$type":"string","$ne":""}}},
			{"$project":{"_id":"$_id","_openid":{"$ifNull":["$_openid",null]},"userInfo__nickName":{"$ifNull":["$userInfo.nickName",null]}}},
			{"$sort":{"_id":1}}
		]}`,
		Options: plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}
	lineage, err := prepared.OutputLineage(t.Context())
	if err != nil {
		t.Fatalf("PreparedQuery.OutputLineage() error = %v", err)
	}
	if len(lineage.Sources) != 1 || lineage.Sources[0].OpaqueOutput || lineage.Sources[0].IdentityOutput {
		t.Fatalf("aggregate output lineage = %#v", lineage)
	}
	assertMongoOutputBinding(t, lineage.Sources[0].Bindings, []string{"userInfo", "nickName"}, []string{"userInfo__nickName"}, plugin.QueryOutputTransformationDirect)
	assertNoMongoOutputBinding(t, lineage.Sources[0].Bindings, []string{"userInfo", "phone"})
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

func TestCollectMongoDocumentFieldsPreservesLaterTopLevelFields(t *testing.T) {
	stats := make(map[string]*mongoFieldStat)
	early := bson.M{}
	for index := 0; index < mongoSchemaMaxFields; index++ {
		early[fmt.Sprintf("nested_%03d", index)] = bson.M{"deep": index}
	}
	document := bson.M{"early": early, "leader": bson.M{"personid": "person-1"}}

	collectMongoDocumentFields(stats, nil, map[string]interface{}(document), 0)

	if stats["leader"] == nil || stats["leader.personid"] == nil {
		t.Fatalf("later top-level leader fields were lost: leader=%v personid=%v", stats["leader"], stats["leader.personid"])
	}
}

func TestCollectMongoDocumentSamplesPreservesTopLevelFieldsAcrossDocuments(t *testing.T) {
	first := bson.M{"early": bson.M{}}
	for index := 0; index < mongoSchemaMaxFields+4; index++ {
		first["early"].(bson.M)[fmt.Sprintf("nested_%03d", index)] = index
	}
	second := bson.M{"leader": bson.M{"personid": "person-1"}}

	stats := make(map[string]*mongoFieldStat)
	for _, document := range []bson.M{first, second} {
		collectMongoTopLevelFields(stats, map[string]interface{}(document))
	}
	collectMongoNestedFieldsAcrossDocuments(stats, []map[string]interface{}{
		map[string]interface{}(first),
		map[string]interface{}(second),
	}, nil, 0)

	if stats["leader"] == nil || stats["leader.personid"] == nil {
		t.Fatalf("top-level fields from later documents were lost: leader=%v personid=%v", stats["leader"], stats["leader.personid"])
	}
}

func TestCollectMongoDocumentSamplesCountsUniquePathsAgainstFieldLimit(t *testing.T) {
	documents := make([]map[string]interface{}, 0, mongoSchemaMaxFields)
	for index := 0; index < mongoSchemaMaxFields; index++ {
		documents = append(documents, map[string]interface{}{
			"members": primitive.A{
				bson.M{"userInfo": bson.M{
					"avatarUrl": "avatar",
					"nickName":  "nickname",
					"phone":     "13800000000",
				}},
			},
			"other": bson.M{"bucket": bson.M{
				fmt.Sprintf("field_%03d", index): index,
			}},
		})
	}

	stats := make(map[string]*mongoFieldStat)
	for _, document := range documents {
		collectMongoTopLevelFields(stats, document)
	}
	collectMongoNestedFieldsAcrossDocuments(stats, documents, nil, 0)

	for _, fieldName := range []string{
		"members.userInfo.avatarUrl",
		"members.userInfo.nickName",
		"members.userInfo.phone",
	} {
		stat := stats[fieldName]
		if stat == nil {
			t.Fatalf("repeated earlier paths exhausted the field budget before %q", fieldName)
		}
		if stat.Count != len(documents) {
			t.Fatalf("field %q count = %d, want %d", fieldName, stat.Count, len(documents))
		}
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

func TestLooksLikeMongoDynamicKeyRecognizesIdentifierShapedMapKeys(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "numeric object key", key: "2273304", want: true},
		{name: "array index shaped key", key: "0", want: true},
		{name: "mixed generated token", key: "W7c20d2AWotkXWsD", want: true},
		{name: "mixed case generated token", key: "W-UNgnhEiJmgcRxt", want: true},
		{name: "long generated token", key: "ogNmG5A5iITPD0IuDhUCFN8nhbGE", want: true},
		{name: "ordinary camel case field", key: "agreedDisclaimer", want: false},
		{name: "ordinary field with digit", key: "addressLine2", want: false},
		{name: "ordinary underscored field", key: "member_nickname", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := looksLikeMongoDynamicKey(test.key); got != test.want {
				t.Fatalf("looksLikeMongoDynamicKey(%q) = %v, want %v", test.key, got, test.want)
			}
		})
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
