package mongodb

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestMongoCollectionFromCatalogPath(t *testing.T) {
	database, collection, ok := mongoCollectionFromCatalogPath(plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: 9,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
			{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: "business"},
			{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Name: "orders"},
		},
	})
	if !ok || database != "business" || collection != "orders" {
		t.Fatalf("mongoCollectionFromCatalogPath() = (%q, %q, %t)", database, collection, ok)
	}
}

func TestMongoDatabaseFromCatalogPathAllowsDatabaseSelection(t *testing.T) {
	database, ok := mongoDatabaseFromCatalogPath(plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: 9,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
			{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: "Outdoor"},
		},
	})
	if !ok || database != "Outdoor" {
		t.Fatalf("mongoDatabaseFromCatalogPath() = (%q, %t)", database, ok)
	}
}

func TestMongoRegistrationSeparatesIdentityFromDefaultDatabase(t *testing.T) {
	provider := &MongoDBPlugin{}
	if got := provider.RequiredFields(); len(got) != 1 || got[0] != "host" {
		t.Fatalf("RequiredFields() = %#v", got)
	}
	want := []string{"host", "port", "user", "auth_source"}
	got := provider.ConnectionIdentityFields()
	if len(got) != len(want) {
		t.Fatalf("ConnectionIdentityFields() = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ConnectionIdentityFields() = %#v", got)
		}
	}
}

func TestGenerateSampleQueryAllowsCatalogDatabaseDifferentFromDefault(t *testing.T) {
	provider := &MongoDBPlugin{}
	path := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: 11,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
			{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: "Outdoor"},
			{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Name: "Persons"},
		},
	}
	query, language := provider.GenerateSampleQuery(t.Context(), plugin.ConnectionInfo{"database": "business"}, plugin.SampleQueryOptions{Path: path})
	if query != `{"find":"Persons","filter":{},"limit":10}` || language != "mql" {
		t.Fatalf("GenerateSampleQuery() = (%q, %q)", query, language)
	}
}

func TestMongoAggregateHasWriteStage(t *testing.T) {
	readPipeline := []interface{}{map[string]interface{}{"$match": map[string]interface{}{"status": "paid"}}}
	if mongoAggregateHasWriteStage(readPipeline) {
		t.Fatal("read-only aggregate was classified as writing")
	}
	writePipeline := []interface{}{map[string]interface{}{"$merge": "summary"}}
	if !mongoAggregateHasWriteStage(writePipeline) {
		t.Fatal("$merge aggregate must be rejected in read-only mode")
	}
}

func TestMongoDBQueryRejectsMissingStructuredParameterBeforeConnecting(t *testing.T) {
	provider := &MongoDBPlugin{}
	_, err := provider.executeQuery(t.Context(), plugin.ConnectionInfo{}, `{"find":"members","filter":{"status":{"$param":"status"}}}`, plugin.QueryOptions{})
	if err == nil || !strings.Contains(err.Error(), `query parameter "status" is not provided`) {
		t.Fatalf("executeQuery() error = %v, want missing parameter error", err)
	}
}

func TestMongoDBQueryRejectsMissingDatabaseWithStableErrorCode(t *testing.T) {
	provider := &MongoDBPlugin{}
	_, err := provider.executeQuery(t.Context(), plugin.ConnectionInfo{"host": "127.0.0.1"}, `{"find":"members","filter":{}}`, plugin.QueryOptions{})
	if err == nil || plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeMongoDBDatabaseRequired {
		t.Fatalf("executeQuery() error code = %q, error = %v", plugin.QueryErrorCodeOf(err), err)
	}
	var queryErr *plugin.QueryError
	if !errors.As(err, &queryErr) || queryErr == nil {
		t.Fatalf("executeQuery() error = %T, want *plugin.QueryError", err)
	}
}
