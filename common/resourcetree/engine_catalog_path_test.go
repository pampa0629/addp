package resourcetree

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestProviderCatalogPathFromLocatorTabularTable(t *testing.T) {
	t.Parallel()

	got, err := EngineCatalogPathFromLocator(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), &ResourceLocator{
		EngineID: 7,
		Path:     []string{"public", "users"},
		Type:     TypeTable,
	})
	if err != nil {
		t.Fatalf("EngineCatalogPathFromLocator() error = %v", err)
	}
	want := plugin.EngineCatalogPath{
		Version:  plugin.EngineCatalogPathVersion,
		EngineID: 7,
		Segments: []plugin.EngineCatalogSegment{
			{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
			{Term: plugin.EngineCatalogTermSchema, Kind: plugin.EngineCatalogKindNamespace, Name: "public"},
			{Term: plugin.EngineCatalogTermTable, Kind: plugin.EngineCatalogKindTable, Name: "users"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("path = %#v, want %#v", got, want)
	}
}

func TestProviderCatalogPathFromLocatorDynamicCollection(t *testing.T) {
	t.Parallel()

	got, err := EngineCatalogPathFromLocator(plugin.DynamicSchemaCatalogModel(), &ResourceLocator{
		EngineID: 8,
		Path:     []string{"business", "orders"},
		Type:     TypeCollection,
	})
	if err != nil {
		t.Fatalf("EngineCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.EngineCatalogSegment{
		{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
		{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "business"},
		{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogKindCollection, Name: "orders"},
	})
}

func TestProviderCatalogPathFromLocatorGraph(t *testing.T) {
	t.Parallel()

	got, err := EngineCatalogPathFromLocator(plugin.GraphCatalogModel(), &ResourceLocator{
		EngineID: 9,
		Path:     []string{"neo4j", "graph"},
		Type:     TypeGraph,
	})
	if err != nil {
		t.Fatalf("EngineCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.EngineCatalogSegment{
		{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
		{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "neo4j"},
		{Term: plugin.EngineCatalogTermGraph, Kind: plugin.EngineCatalogKindGraph, Name: "graph"},
	})
}

func TestProviderCatalogPathFromLocatorObject(t *testing.T) {
	t.Parallel()

	got, err := EngineCatalogPathFromLocator(plugin.ObjectCatalogModel(), &ResourceLocator{
		EngineID: 10,
		Path:     []string{"bucket", "a", "b.csv"},
		Type:     TypeObject,
	})
	if err != nil {
		t.Fatalf("EngineCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.EngineCatalogSegment{
		{Term: plugin.EngineCatalogTermService, Kind: plugin.EngineCatalogTermService},
		{Term: plugin.EngineCatalogTermBucket, Kind: plugin.EngineCatalogKindBucket, Name: "bucket"},
		{Term: plugin.EngineCatalogTermPrefix, Kind: plugin.EngineCatalogKindPrefix, Name: "a"},
		{Term: plugin.EngineCatalogTermObject, Kind: plugin.EngineCatalogKindObject, Name: "b.csv"},
	})
}

func TestProviderCatalogPathFromLocatorSingleLevelServiceLeaf(t *testing.T) {
	t.Parallel()
	model := plugin.EngineCatalogModelSpec{
		PathVersion: plugin.EngineCatalogPathVersion,
		RootTerm:    plugin.EngineCatalogTermService,
		Levels: []plugin.EngineCatalogLevelSpec{{
			Term: "topic", Kinds: []string{"topic"}, Role: plugin.EngineCatalogRoleLeaf,
		}},
	}
	got, err := EngineCatalogPathFromLocator(model, &ResourceLocator{
		EngineID: 30, Path: []string{"orders.events"}, Type: ResourceType("topic"),
	})
	if err != nil {
		t.Fatalf("EngineCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.EngineCatalogSegment{
		{Term: plugin.EngineCatalogTermService, Kind: plugin.EngineCatalogTermService},
		{Term: "topic", Kind: "topic", Name: "orders.events"},
	})
}

func TestProviderCatalogPathFromLocatorFile(t *testing.T) {
	t.Parallel()

	got, err := EngineCatalogPathFromLocator(plugin.FileCatalogModel(), &ResourceLocator{
		EngineID: 11,
		Path:     []string{"data", "a.csv"},
		Type:     TypeFile,
	})
	if err != nil {
		t.Fatalf("EngineCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.EngineCatalogSegment{
		{Term: plugin.EngineCatalogTermRoot, Kind: plugin.EngineCatalogTermRoot},
		{Term: plugin.EngineCatalogTermDirectory, Kind: plugin.EngineCatalogKindDirectory, Name: "data"},
		{Term: plugin.EngineCatalogTermFile, Kind: plugin.EngineCatalogKindFile, Name: "a.csv"},
	})
}

func TestProviderCatalogPathFromLocatorRoot(t *testing.T) {
	t.Parallel()

	got, err := EngineCatalogPathFromLocator(plugin.ObjectCatalogModel(), &ResourceLocator{
		EngineID: 12,
		Type:     TypeService,
	})
	if err != nil {
		t.Fatalf("EngineCatalogPathFromLocator() error = %v", err)
	}
	assertCatalogSegments(t, got, []plugin.EngineCatalogSegment{
		{Term: plugin.EngineCatalogTermService, Kind: plugin.EngineCatalogTermService},
	})
}

func TestProviderCatalogPathFromLocatorAcceptsAllRootTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model plugin.EngineCatalogModelSpec
		typ   ResourceType
		want  plugin.EngineCatalogSegment
	}{
		{name: "server", model: plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), typ: TypeServer, want: plugin.EngineCatalogSegment{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer}},
		{name: "service", model: plugin.ObjectCatalogModel(), typ: TypeService, want: plugin.EngineCatalogSegment{Term: plugin.EngineCatalogTermService, Kind: plugin.EngineCatalogTermService}},
		{name: "root", model: plugin.FileCatalogModel(), typ: TypeRoot, want: plugin.EngineCatalogSegment{Term: plugin.EngineCatalogTermRoot, Kind: plugin.EngineCatalogTermRoot}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EngineCatalogPathFromLocator(tt.model, &ResourceLocator{EngineID: 15, Type: tt.typ})
			if err != nil {
				t.Fatalf("EngineCatalogPathFromLocator() error = %v", err)
			}
			assertCatalogSegments(t, got, []plugin.EngineCatalogSegment{tt.want})
		})
	}
}

func TestProviderCatalogPathFromLocatorRejectsMissingLeaf(t *testing.T) {
	t.Parallel()

	_, err := EngineCatalogPathFromLocator(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), &ResourceLocator{
		EngineID: 7,
		Path:     []string{"public"},
		Type:     TypeTable,
	})
	if err == nil {
		t.Fatalf("expected missing leaf error")
	}
}

func assertCatalogSegments(t *testing.T, path plugin.EngineCatalogPath, want []plugin.EngineCatalogSegment) {
	t.Helper()
	if path.Version != plugin.EngineCatalogPathVersion {
		t.Fatalf("path version = %q", path.Version)
	}
	if !reflect.DeepEqual(path.Segments, want) {
		t.Fatalf("segments = %#v, want %#v", path.Segments, want)
	}
}
