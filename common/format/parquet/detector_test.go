package parquet

import (
	"context"
	"testing"

	commonAttrs "github.com/addp/common/attributes"
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
	if mode := commonAttrs.String(info.Attributes, "item", "mode"); mode != "directory_tree" {
		t.Fatalf("mode = %v, want directory_tree", mode)
	}
}

func TestLakeTableDetectorAllowsAuxiliaryFiles(t *testing.T) {
	t.Parallel()

	d := &LakeTableDetector{}
	files := []plugin.FileEntry{
		{Name: "part-000.parquet", Path: "dataset/part-000.parquet", Size: 10},
		{Name: "_SUCCESS", Path: "dataset/_SUCCESS", Size: 0},
		{Name: "._metadata.crc", Path: "dataset/._metadata.crc", Size: 4},
	}

	if !d.Detect(context.Background(), files, nil) {
		t.Fatal("expected parquet directory with auxiliary files to match")
	}
	info, err := ExtractDirectoryTreeInfo(context.Background(), nil, nil, 1, "dataset", files, nil)
	if err != nil {
		t.Fatalf("ExtractDirectoryTreeInfo() error = %v", err)
	}
	if len(info.ComponentFiles) != 1 || info.ComponentFiles[0] != "dataset/part-000.parquet" {
		t.Fatalf("ComponentFiles = %#v, want only parquet data file", info.ComponentFiles)
	}
	if info.SizeBytes == nil || *info.SizeBytes != 10 {
		t.Fatalf("SizeBytes = %v, want 10", info.SizeBytes)
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
