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

func TestLakeTableDetectorRulesDeclareSingleFileAndDirectoryTree(t *testing.T) {
	t.Parallel()

	d := &LakeTableDetector{}
	rules := d.Rules()
	if len(rules) != 2 {
		t.Fatalf("Rules len = %d, want 2", len(rules))
	}

	seenSingle := false
	seenDirectoryTree := false
	for _, rule := range rules {
		if err := dataitem.ValidateFormatRule(rule); err != nil {
			t.Fatalf("ValidateFormatRule(%s/%s) error = %v", rule.Format, rule.CompositionType, err)
		}
		switch rule.CompositionType {
		case dataitem.CompositionTypeSingleFile:
			seenSingle = true
		case dataitem.CompositionTypeDirectoryTree:
			seenDirectoryTree = true
			if rule.DirectoryTree == nil || !rule.DirectoryTree.ExclusiveOnStrongHit {
				t.Fatalf("directory_tree rule = %#v, want explicit exclusive rule", rule.DirectoryTree)
			}
		}
	}
	if !seenSingle || !seenDirectoryTree {
		t.Fatalf("rules missing single_file=%v directory_tree=%v", seenSingle, seenDirectoryTree)
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

func TestLakeTableDetectorResolvesDirectoryTreeFromRecursiveScope(t *testing.T) {
	t.Parallel()

	d := &LakeTableDetector{}
	result, err := d.ResolveItems(context.Background(), dataitem.DirectoryResolveInput{
		DirPath: "dataset",
		Subdirs: []plugin.DirEntry{
			{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"},
		},
		RecursiveFiles: []plugin.FileEntry{
			{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet", Size: 10},
		},
		RecursiveSubdirs: []plugin.DirEntry{
			{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result == nil || len(result.Items) != 1 {
		t.Fatalf("ResolveItems() = %#v, want one item", result)
	}
	item := result.Items[0]
	if item.CompositionType != dataitem.CompositionTypeDirectoryTree {
		t.Fatalf("CompositionType = %q, want directory_tree", item.CompositionType)
	}
	if item.EntryPath != "dataset" || item.PhysicalPath != "dataset" {
		t.Fatalf("paths = entry %q physical %q, want dataset", item.EntryPath, item.PhysicalPath)
	}
	if !result.Exclusive {
		t.Fatal("expected directory tree result to be exclusive")
	}
	if !result.Claims["dataset/dt=2026-05-05/part-000.parquet"] {
		t.Fatalf("claims = %#v, want recursive parquet component", result.Claims)
	}
}

func TestLakeTableDetectorRejectsSiblingIndependentParquetFiles(t *testing.T) {
	t.Parallel()

	d := &LakeTableDetector{}
	files := []plugin.FileEntry{
		{Name: "sales.parquet", Path: "dataset/sales.parquet"},
		{Name: "customers.parquet", Path: "dataset/customers.parquet"},
	}

	if d.Detect(context.Background(), files, nil) {
		t.Fatal("expected independent sibling parquet files to remain single-file items")
	}
}
