package mongodb

import (
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
