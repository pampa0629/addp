package scanflow

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestNormalizedScanRefsAddsPrimaryAndDeduplicates(t *testing.T) {
	t.Parallel()

	refs := NormalizedScanRefs(models.ScanRefGroup{
		Primary: "bucket/roads.shp",
		Refs: []models.ScanRef{
			{Path: "bucket/roads.shp", Role: "main", Required: true},
			{Path: "bucket/roads.dbf", Role: "sidecar", Required: true},
			{Path: " ", Role: "sidecar", Required: true},
		},
	})

	want := []models.ScanRef{
		{Path: "bucket/roads.shp", Role: "main", Required: true},
		{Path: "bucket/roads.dbf", Role: "sidecar", Required: true},
	}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %#v, want %#v", refs, want)
	}
}

func TestFileRefsFromScanRefGroup(t *testing.T) {
	t.Parallel()

	files := FileRefsFromScanRefGroup(7, models.ScanRefGroup{
		Primary: "shp/roads.shp",
		Refs: []models.ScanRef{
			{Path: "shp/roads.dbf", Role: "sidecar", Required: true},
		},
	})
	if len(files) != 2 {
		t.Fatalf("files = %#v", files)
	}
	if files[0].Name != "roads.shp" || files[0].Path != "shp/roads.shp" {
		t.Fatalf("primary file = %#v", files[0])
	}
	if files[1].Name != "roads.dbf" || files[1].Path != "shp/roads.dbf" {
		t.Fatalf("sidecar file = %#v", files[1])
	}
}

func TestFileRefGroupCandidateSet(t *testing.T) {
	t.Parallel()

	candidates := FileRefGroupCandidateSet(7, "shp/roads.shp", models.ScanRefGroup{
		Primary: "shp/roads.shp",
		Refs: []models.ScanRef{
			{Path: "shp/roads.dbf", Role: "sidecar", Required: true},
		},
	})

	if candidates.DirPath != "shp" {
		t.Fatalf("DirPath = %q, want shp", candidates.DirPath)
	}
	if len(candidates.Files) != 2 || candidates.Files[0].Path != "shp/roads.shp" || candidates.Files[1].Path != "shp/roads.dbf" {
		t.Fatalf("candidate files = %#v", candidates.Files)
	}
	if got := candidates.CatalogPathFor("shp/roads.shp").StringPath(); got != "shp/roads.shp" {
		t.Fatalf("catalog path = %q", got)
	}
}

func TestObjectResourcesFromScanRefGroupRejectsCrossBucketRefs(t *testing.T) {
	t.Parallel()

	_, err := ObjectResourcesFromScanRefGroup(7, "bucket-a", models.ScanRefGroup{
		Primary: "bucket-a/roads.shp",
		Refs: []models.ScanRef{
			{Path: "bucket-b/roads.dbf", Role: "sidecar", Required: true},
		},
	})
	if err == nil {
		t.Fatal("ObjectResourcesFromScanRefGroup() should reject cross-bucket refs")
	}
}

func TestObjectRefGroupCandidateSetUsesBucketQualifiedDetectionScope(t *testing.T) {
	t.Parallel()

	resources, err := ObjectResourcesFromScanRefGroup(7, "bucket", models.ScanRefGroup{
		Primary: "bucket/path/roads.shp",
		Refs: []models.ScanRef{
			{Path: "bucket/path/roads.dbf", Role: "sidecar", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("ObjectResourcesFromScanRefGroup() error = %v", err)
	}

	candidates := ObjectRefGroupCandidateSet(7, "bucket", "path/roads.shp", resources)

	if candidates.DirPath != "bucket/path" {
		t.Fatalf("DirPath = %q, want bucket/path", candidates.DirPath)
	}
	if len(candidates.Files) != 2 || candidates.Files[0].Path != "bucket/path/roads.shp" || candidates.Files[1].Path != "bucket/path/roads.dbf" {
		t.Fatalf("candidate files = %#v", candidates.Files)
	}
	if got := candidates.CatalogPathFor("bucket/path/roads.shp").StringPath(); got != "bucket/path/roads.shp" {
		t.Fatalf("catalog path = %q", got)
	}
}

func TestSplitObjectRefPath(t *testing.T) {
	t.Parallel()

	bucket, objectPath, err := SplitObjectRefPath("/bucket/path/roads.shp")
	if err != nil {
		t.Fatalf("SplitObjectRefPath() error = %v", err)
	}
	if bucket != "bucket" || objectPath != "path/roads.shp" {
		t.Fatalf("bucket/object = %q/%q", bucket, objectPath)
	}
}
