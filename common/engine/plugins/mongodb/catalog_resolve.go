package mongodb

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func validateCollectionCatalogPath(path plugin.EngineCatalogPath) error {
	if path.Version != plugin.EngineCatalogPathVersion || path.EngineID == 0 || len(path.Segments) != 3 ||
		path.Segments[0] != (plugin.EngineCatalogSegment{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer}) ||
		path.Segments[1].Term != plugin.EngineCatalogTermDatabase || path.Segments[1].Kind != plugin.EngineCatalogKindNamespace || path.Segments[1].Name == "" ||
		path.Segments[2].Term != plugin.EngineCatalogTermCollection || path.Segments[2].Kind != plugin.EngineCatalogKindCollection || path.Segments[2].Name == "" {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("collection path requires a canonical server/database/collection leaf"))
	}
	if (&MongoDBPlugin{}).IsSystemDatabase(path.Segments[1].Name) {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("collection not visible"))
	}
	return nil
}

// Resolve only the named native collection. Catalog existence checks must never
// fetch documents, sampled schema, statistics or indexes.
func resolveCollectionCatalogEntry(ctx context.Context, database *mongo.Database, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	name := path.Segments[2].Name
	specs, err := database.ListCollectionSpecifications(ctx, bson.D{{Key: "name", Value: name}})
	if err != nil {
		return nil, err
	}
	if len(specs) != 1 || specs[0].Name != name || specs[0].Type != "collection" {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("ordinary collection not found"))
	}
	return &plugin.EngineCatalogEntry{Name: name, Path: path, Term: plugin.EngineCatalogTermCollection,
		Kind: plugin.EngineCatalogKindCollection, Role: plugin.EngineCatalogRoleLeaf}, nil
}
