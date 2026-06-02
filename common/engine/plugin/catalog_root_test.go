package plugin

import "testing"

func TestCatalogRootEntryIsStructuralOnly(t *testing.T) {
	models := []CatalogModelSpec{
		TabularCatalogModel(CatalogTermSchema),
		DynamicSchemaCatalogModel(),
		GraphCatalogModel(),
		ObjectCatalogModel(),
		FileCatalogModel(),
	}

	for _, model := range models {
		root := CatalogRootEntry(model, 42, "Demo Engine")
		if root.Name != "Demo Engine" {
			t.Fatalf("%s root name = %q, want engine display name", model.RootTerm, root.Name)
		}
		if root.Term != model.RootTerm || root.Kind != model.RootTerm || root.Role != CatalogRoleBranch {
			t.Fatalf("%s root entry = %#v", model.RootTerm, root)
		}
		if !IsCatalogRootPath(root.Path) {
			t.Fatalf("%s root path is not structural root: %#v", model.RootTerm, root.Path)
		}
		if got := root.Path.StringPath(); got != "" {
			t.Fatalf("%s root StringPath = %q, want empty business path", model.RootTerm, got)
		}
		if business := CatalogBusinessLevels(model); len(business) == 0 || business[0].Term == model.RootTerm {
			t.Fatalf("%s business levels include root: %#v", model.RootTerm, business)
		}
	}
}

func TestCatalogPathStringPathSkipsRootSegment(t *testing.T) {
	tests := []struct {
		name string
		path CatalogPath
		want string
	}{
		{
			name: "tabular",
			path: appendCatalogSegment(
				appendCatalogSegment(CatalogRootPath(TabularCatalogModel(CatalogTermSchema), 7), 7, CatalogTermSchema, CatalogKindNamespace, "public"),
				7,
				CatalogTermTable,
				CatalogKindTable,
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
	withoutRoot := CatalogPathWithoutRoot(path)
	if len(withoutRoot.Segments) != 3 {
		t.Fatalf("segments = %#v", withoutRoot.Segments)
	}
	if withoutRoot.Segments[0].Term != CatalogTermBucket || withoutRoot.Segments[0].Name != "bucket" {
		t.Fatalf("first business segment = %#v", withoutRoot.Segments[0])
	}
	if got := withoutRoot.StringPath(); got != "bucket/dir/a.csv" {
		t.Fatalf("business StringPath = %q", got)
	}
}
