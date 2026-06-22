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
		{Path: "bucket/roads.shp", Role: "main", Required: true, Primary: true},
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

func TestObjectRefGroupCandidateSetUsesBucketRelativeDetectionScope(t *testing.T) {
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
	if len(resources) != 2 || resources[0].Path != "path/roads.shp" || resources[0].FullPath != "bucket/path/roads.shp" {
		t.Fatalf("resources = %#v, want bucket-relative Path and bucket-qualified FullPath", resources)
	}

	candidates := ObjectRefGroupCandidateSet(7, "bucket", "path/roads.shp", resources)

	if candidates.DirPath != "path" {
		t.Fatalf("DirPath = %q, want path", candidates.DirPath)
	}
	if len(candidates.Files) != 2 || candidates.Files[0].Path != "path/roads.shp" || candidates.Files[1].Path != "path/roads.dbf" {
		t.Fatalf("candidate files = %#v", candidates.Files)
	}
	if got := candidates.CatalogPathFor("path/roads.shp").StringPath(); got != "bucket/path/roads.shp" {
		t.Fatalf("catalog path = %q", got)
	}
}

func TestObjectRefGroupCandidateSetAcceptsBucketQualifiedPath(t *testing.T) {
	t.Parallel()

	candidates := ObjectRefGroupCandidateSet(7, "bucket", "path/roads.shp", nil)
	if got := candidates.CatalogPathFor("bucket/path/roads.shp").StringPath(); got != "bucket/path/roads.shp" {
		t.Fatalf("catalog path = %q, want bucket/path/roads.shp", got)
	}
}
