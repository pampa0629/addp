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

func TestNormalizedScanRefsPreservesExplicitScopeRole(t *testing.T) {
	t.Parallel()

	refs := NormalizedScanRefs(models.ScanRefGroup{
		Primary: "arcgis/pgeo_roundtrip.gdb",
		Refs: []models.ScanRef{
			{Path: "arcgis/pgeo_roundtrip.gdb", Role: "scope", Required: true},
		},
	})

	want := []models.ScanRef{
		{Path: "arcgis/pgeo_roundtrip.gdb", Role: "scope", Required: true, Primary: true},
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
	if got := candidates.EngineCatalogPathFor("shp/roads.shp").StringPath(); got != "shp/roads.shp" {
		t.Fatalf("catalog path = %q", got)
	}
}

func TestFileRefGroupCandidateSetPreservesScopeDirectory(t *testing.T) {
	t.Parallel()

	candidates := FileRefGroupCandidateSet(7, "arcgis/pgeo_roundtrip.gdb", models.ScanRefGroup{
		Primary: "arcgis/pgeo_roundtrip.gdb",
		Refs: []models.ScanRef{
			{Path: "arcgis/pgeo_roundtrip.gdb", Role: "scope", Required: true},
		},
	})

	if candidates.DirPath != "arcgis" {
		t.Fatalf("DirPath = %q, want arcgis", candidates.DirPath)
	}
	if len(candidates.Files) != 0 {
		t.Fatalf("candidate files = %#v, want none", candidates.Files)
	}
	if len(candidates.Subdirs) != 1 || candidates.Subdirs[0].Path != "arcgis/pgeo_roundtrip.gdb" {
		t.Fatalf("candidate subdirs = %#v, want FileGDB scope directory", candidates.Subdirs)
	}
	if got := candidates.Subdirs[0].EngineCatalogPath.StringPath(); got != "arcgis/pgeo_roundtrip.gdb" {
		t.Fatalf("scope catalog path = %q", got)
	}
	segments := candidates.EngineCatalogPathFor("arcgis/pgeo_roundtrip.gdb").Segments
	if len(segments) == 0 || segments[len(segments)-1].Kind != "directory" {
		t.Fatalf("scope catalog path segments = %#v, want directory leaf", segments)
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
	if got := candidates.EngineCatalogPathFor("path/roads.shp").StringPath(); got != "bucket/path/roads.shp" {
		t.Fatalf("catalog path = %q", got)
	}
}

func TestObjectRefGroupCandidateSetAcceptsBucketQualifiedPath(t *testing.T) {
	t.Parallel()

	candidates := ObjectRefGroupCandidateSet(7, "bucket", "path/roads.shp", nil)
	if got := candidates.EngineCatalogPathFor("bucket/path/roads.shp").StringPath(); got != "bucket/path/roads.shp" {
		t.Fatalf("catalog path = %q, want bucket/path/roads.shp", got)
	}
}
