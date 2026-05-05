package parquet

import (
	"context"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
)

func TestLakeTableDetectorDetectsPartitionedDirectoryTree(t *testing.T) {
	t.Parallel()

	d := &LakeTableDetector{}
	files := []plugin.FileEntry{
		{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet", Size: 10},
		{Name: "part-001.parquet", Path: "dataset/dt=2026-05-06/part-001.parquet", Size: 20},
	}
	subdirs := []plugin.DirEntry{
		{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"},
		{Name: "dt=2026-05-06", Path: "dataset/dt=2026-05-06/"},
	}

	if !d.Detect(context.Background(), files, subdirs) {
		t.Fatal("expected partitioned parquet directory to match")
	}
	info, err := ExtractDirectoryTreeInfo(context.Background(), nil, nil, 1, "dataset", files, subdirs)
	if err != nil {
		t.Fatalf("ExtractDirectoryTreeInfo() error = %v", err)
	}
	if info.CompositionType != dataitem.CompositionTypeDirectoryTree {
		t.Fatalf("CompositionType = %q, want %q", info.CompositionType, dataitem.CompositionTypeDirectoryTree)
	}
	if info.EntryPath != "dataset" {
		t.Fatalf("EntryPath = %q, want dataset", info.EntryPath)
	}
	if len(info.ComponentFiles) != 2 {
		t.Fatalf("ComponentFiles len = %d, want 2", len(info.ComponentFiles))
	}
	if mode := info.Attributes["mode"]; mode != "directory_tree" {
		t.Fatalf("mode = %v, want directory_tree", mode)
	}
}

func TestLakeTableDetectorRejectsMixedDirectoryTree(t *testing.T) {
	t.Parallel()

	d := &LakeTableDetector{}
	files := []plugin.FileEntry{
		{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet"},
		{Name: "README.txt", Path: "dataset/README.txt"},
	}
	subdirs := []plugin.DirEntry{{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"}}

	if d.Detect(context.Background(), files, subdirs) {
		t.Fatal("expected mixed directory tree to be rejected")
	}
}
