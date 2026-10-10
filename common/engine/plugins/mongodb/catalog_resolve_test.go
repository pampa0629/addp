package mongodb

import (
	"context"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func collectionCatalogTestPath() plugin.EngineCatalogPath {
	return plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 3, Segments: []plugin.EngineCatalogSegment{
		{Term: "server", Kind: "server"}, {Term: "database", Kind: "namespace", Name: "Outdoor"}, {Term: "collection", Kind: "collection", Name: "Persons"},
	}}
}

func TestResolveCollectionUsesExactMetadataWithoutSampling(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	for _, tc := range []struct {
		name, nativeType, returnedName string
		missing, denied, accept        bool
	}{
		{name: "ordinary collection", nativeType: "collection", returnedName: "Persons", accept: true},
		{name: "view", nativeType: "view", returnedName: "Persons"},
		{name: "timeseries", nativeType: "timeseries", returnedName: "Persons"},
		{name: "missing", missing: true},
		{name: "different name", nativeType: "collection", returnedName: "Other"},
		{name: "permission failure", denied: true},
	} {
		mt.Run(tc.name, func(mt *mtest.T) {
			path := collectionCatalogTestPath()
			var documents []bson.D
			if !tc.missing {
				documents = append(documents, bson.D{{Key: "name", Value: tc.returnedName}, {Key: "type", Value: tc.nativeType}})
			}
			if tc.denied {
				mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 13, Message: "unauthorized"}))
			} else {
				mt.AddMockResponses(mtest.CreateCursorResponse(0, mt.DB.Name()+".$cmd.listCollections", mtest.FirstBatch, documents...))
			}
			entry, err := resolveCollectionCatalogEntry(context.Background(), mt.DB, path)
			if tc.accept {
				if err != nil || entry == nil || !reflect.DeepEqual(entry.Path, path) || entry.Role != "leaf" || entry.Kind != "collection" || entry.Table != nil {
					mt.Fatalf("entry=%+v err=%v", entry, err)
				}
			} else if err == nil || entry != nil {
				mt.Fatalf("unexpected acceptance: %+v %v", entry, err)
			} else if !tc.denied && !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
				mt.Fatalf("wrong error: %v", err)
			}
			events := mt.GetAllStartedEvents()
			if len(events) != 1 || events[0].CommandName != "listCollections" {
				mt.Fatalf("unexpected source reads: %+v", events)
			}
			filter := events[0].Command.Lookup("filter").Document()
			if filter.Lookup("name").StringValue() != "Persons" || len(filter) != len(bsonMustMarshal(t, bson.D{{Key: "name", Value: "Persons"}})) {
				mt.Fatalf("not an exact name filter: %v", filter)
			}
		})
	}
}

func bsonMustMarshal(t *testing.T, value interface{}) []byte {
	t.Helper()
	result, err := bson.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCollectionResolveRejectsInvalidPathsBeforeConnecting(t *testing.T) {
	for _, mutate := range []func(*plugin.EngineCatalogPath){
		func(p *plugin.EngineCatalogPath) { p.Version = "invalid" },
		func(p *plugin.EngineCatalogPath) { p.EngineID = 0 },
		func(p *plugin.EngineCatalogPath) { p.Segments[0].Name = "forged" },
		func(p *plugin.EngineCatalogPath) { p.Segments[1].Kind = "table" },
		func(p *plugin.EngineCatalogPath) { p.Segments[1].Name = "admin" },
		func(p *plugin.EngineCatalogPath) { p.Segments[2].Name = "" },
		func(p *plugin.EngineCatalogPath) { p.Segments[2].Kind = "view" },
		func(p *plugin.EngineCatalogPath) { p.Segments = append(p.Segments, p.Segments[2]) },
	} {
		path := collectionCatalogTestPath()
		mutate(&path)
		if entry, err := (&MongoDBPlugin{}).ResolvePath(context.Background(), nil, path); err == nil || entry != nil {
			t.Fatalf("accepted invalid path: %+v", path)
		}
	}
}
