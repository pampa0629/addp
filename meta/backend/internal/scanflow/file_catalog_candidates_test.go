package scanflow

import (
	"testing"

	"github.com/addp/meta/internal/metaitem"
)

func TestFileCatalogDirectoryCandidateSet(t *testing.T) {
	files := []metaitem.StorageFileRef{{Path: "lake/roads.shp"}}
	subdirs := []metaitem.StorageDirectoryRef{{Path: "lake/partition"}}
	recursiveFiles := []metaitem.StorageFileRef{{Path: "lake/partition/roads.dbf"}}
	recursiveSubdirs := []metaitem.StorageDirectoryRef{{Path: "lake/partition/day=1"}}

	candidates := FileCatalogDirectoryCandidateSet(7, "lake", files, subdirs, recursiveFiles, recursiveSubdirs)

	if candidates.DirPath != "lake" {
		t.Fatalf("dir path = %q", candidates.DirPath)
	}
	if len(candidates.Files) != 1 || candidates.Files[0].Path != "lake/roads.shp" {
		t.Fatalf("files = %#v", candidates.Files)
	}
	if len(candidates.Subdirs) != 1 || candidates.Subdirs[0].Path != "lake/partition" {
		t.Fatalf("subdirs = %#v", candidates.Subdirs)
	}
	if len(candidates.RecursiveFiles) != 1 || candidates.RecursiveFiles[0].Path != "lake/partition/roads.dbf" {
		t.Fatalf("recursive files = %#v", candidates.RecursiveFiles)
	}
	if len(candidates.RecursiveSubdirs) != 1 || candidates.RecursiveSubdirs[0].Path != "lake/partition/day=1" {
		t.Fatalf("recursive subdirs = %#v", candidates.RecursiveSubdirs)
	}
	if got := candidates.EngineCatalogPathFor("lake/roads.shp").StringPath(); got != "lake/roads.shp" {
		t.Fatalf("catalog path = %q", got)
	}
}
