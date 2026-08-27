package plugin

import "testing"

func TestCatalogRootEntryIsStructuralOnly(t *testing.T) {
	models := []EngineCatalogModelSpec{
		TabularCatalogModel(EngineCatalogTermSchema),
		DynamicSchemaCatalogModel(),
		GraphCatalogModel(),
		ObjectCatalogModel(),
		FileCatalogModel(),
	}

	for _, model := range models {
		root := EngineCatalogRootEntry(model, 42, "Demo Engine")
		if root.Name != "Demo Engine" {
			t.Fatalf("%s root name = %q, want engine display name", model.RootTerm, root.Name)
		}
		if root.Term != model.RootTerm || root.Kind != model.RootTerm || root.Role != EngineCatalogRoleBranch {
			t.Fatalf("%s root entry = %#v", model.RootTerm, root)
		}
		if !IsEngineCatalogRootPath(root.Path) {
			t.Fatalf("%s root path is not structural root: %#v", model.RootTerm, root.Path)
		}
		if got := root.Path.StringPath(); got != "" {
			t.Fatalf("%s root StringPath = %q, want empty business path", model.RootTerm, got)
		}
		if business := EngineCatalogBusinessLevels(model); len(business) == 0 || business[0].Term == model.RootTerm {
			t.Fatalf("%s business levels include root: %#v", model.RootTerm, business)
		}
	}
}

func TestCatalogPathStringPathSkipsRootSegment(t *testing.T) {
	tests := []struct {
		name string
		path EngineCatalogPath
		want string
	}{
		{
			name: "tabular",
			path: appendCatalogSegment(
				appendCatalogSegment(EngineCatalogRootPath(TabularCatalogModel(EngineCatalogTermSchema), 7), 7, EngineCatalogTermSchema, EngineCatalogKindNamespace, "public"),
				7,
				EngineCatalogTermTable,
				EngineCatalogKindTable,
				"roads",
			),
			want: "public/roads",
		},
		{
			name: "object",
			path: ObjectItemPath(7, "addp", "gis/roads.csv"),
			want: "addp/gis/roads.csv",
		},
		{
			name: "file-root",
			path: FileRootPath(7),
			want: "",
		},
		{
			name: "file-item",
			path: FileItemPath(7, "gis/roads.csv"),
			want: "gis/roads.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.path.StringPath(); got != tt.want {
				t.Fatalf("StringPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCatalogPathWithoutRoot(t *testing.T) {
	path := ObjectItemPath(9, "bucket", "dir/a.csv")
	withoutRoot := EngineCatalogPathWithoutRoot(path)
	if len(withoutRoot.Segments) != 3 {
		t.Fatalf("segments = %#v", withoutRoot.Segments)
	}
	if withoutRoot.Segments[0].Term != EngineCatalogTermBucket || withoutRoot.Segments[0].Name != "bucket" {
		t.Fatalf("first business segment = %#v", withoutRoot.Segments[0])
	}
	if got := withoutRoot.StringPath(); got != "bucket/dir/a.csv" {
		t.Fatalf("business StringPath = %q", got)
	}
}

func TestTabularItemPathUsesExplicitServerRoot(t *testing.T) {
	path := TabularItemPath(7, EngineCatalogTermSchema, "public", "roads")
	if len(path.Segments) != 3 {
		t.Fatalf("segments = %#v", path.Segments)
	}
	if path.Segments[0].Term != EngineCatalogTermServer || path.Segments[0].Kind != EngineCatalogTermServer {
		t.Fatalf("root segment = %#v", path.Segments[0])
	}
	if path.Segments[1].Term != EngineCatalogTermSchema || path.Segments[1].Kind != EngineCatalogKindNamespace || path.Segments[1].Name != "public" {
		t.Fatalf("branch segment = %#v", path.Segments[1])
	}
	if path.Segments[2].Term != EngineCatalogTermTable || path.Segments[2].Kind != EngineCatalogKindTable || path.Segments[2].Name != "roads" {
		t.Fatalf("table segment = %#v", path.Segments[2])
	}
	if got := path.StringPath(); got != "public/roads" {
		t.Fatalf("StringPath() = %q, want public/roads", got)
	}
}

func TestBranchLeafCatalogPathUsesModelRoot(t *testing.T) {
	path := EngineCatalogBranchLeafPath(DynamicSchemaCatalogModel(), 7, EngineCatalogTermDatabase, "business", EngineCatalogTermCollection, EngineCatalogKindCollection, "orders")
	if len(path.Segments) != 3 {
		t.Fatalf("segments = %#v", path.Segments)
	}
	if path.Segments[0].Term != EngineCatalogTermServer || path.Segments[0].Kind != EngineCatalogTermServer {
		t.Fatalf("root segment = %#v", path.Segments[0])
	}
	if path.Segments[1].Term != EngineCatalogTermDatabase || path.Segments[1].Kind != EngineCatalogKindNamespace || path.Segments[1].Name != "business" {
		t.Fatalf("branch segment = %#v", path.Segments[1])
	}
	if path.Segments[2].Term != EngineCatalogTermCollection || path.Segments[2].Kind != EngineCatalogKindCollection || path.Segments[2].Name != "orders" {
		t.Fatalf("item segment = %#v", path.Segments[2])
	}
}
