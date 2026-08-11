package mongodb

import (
	"fmt"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

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
