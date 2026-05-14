package metapath

import (
	"testing"

	"github.com/addp/common/format"
)

func TestFilterObjectMetasForDepthKeepsDirectChildren(t *testing.T) {
	t.Parallel()

	metas := []format.ObjectMetadata{
		{NodeType: "prefix", Path: "a"},
		{NodeType: "prefix", Path: "a/b"},
		{NodeType: "object", Path: "file.csv"},
		{NodeType: "object", Path: "a/file.csv"},
		{NodeType: "bucket", Path: ""},
	}

	filtered := FilterObjectMetasForDepth(metas, "")
	if len(filtered) != 3 {
		t.Fatalf("filtered len = %d, want 3: %#v", len(filtered), filtered)
	}
}

func TestFilterObjectMetasForDepthUsesBasePath(t *testing.T) {
	t.Parallel()

	metas := []format.ObjectMetadata{
		{NodeType: "prefix", Path: "lake/table"},
		{NodeType: "object", Path: "lake/table/data.parquet"},
		{NodeType: "object", Path: "lake/table/nested/data.parquet"},
	}

	filtered := FilterObjectMetasForDepth(metas, "lake/table")
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2: %#v", len(filtered), filtered)
	}
}
