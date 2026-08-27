package metapath

import (
	"testing"

	"github.com/addp/meta/internal/scanresource"
)

func TestFilterObjectResourcesForDepthKeepsDirectChildren(t *testing.T) {
	t.Parallel()

	resources := []scanresource.StorageResource{
		{NodeType: "prefix", Path: "a"},
		{NodeType: "prefix", Path: "a/b"},
		{NodeType: "object", Path: "file.csv"},
		{NodeType: "object", Path: "a/file.csv"},
		{NodeType: "bucket", Path: ""},
	}

	filtered := FilterObjectResourcesForDepth(resources, "")
	if len(filtered) != 3 {
		t.Fatalf("filtered len = %d, want 3: %#v", len(filtered), filtered)
	}
}

func TestFilterObjectResourcesForDepthUsesBasePath(t *testing.T) {
	t.Parallel()

	resources := []scanresource.StorageResource{
		{NodeType: "prefix", Path: "lake/table"},
		{NodeType: "object", Path: "lake/table/data.parquet"},
		{NodeType: "object", Path: "lake/table/nested/data.parquet"},
	}

	filtered := FilterObjectResourcesForDepth(resources, "lake/table")
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2: %#v", len(filtered), filtered)
	}
}
