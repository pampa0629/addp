package metaitem

import (
	"context"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
)

func TestLakeTableDetectorDetectsPartitionedWholeScope(t *testing.T) {
	t.Parallel()

	d := &lakeTableItemDetector{}
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
	info, err := extractLakeTableWholeScopeInfo(context.Background(), nil, nil, 1, "dataset", files, subdirs)
	if err != nil {
		t.Fatalf("extractLakeTableWholeScopeInfo() error = %v", err)
	}
	if info.Organization != dataitem.OrganizationWhole {
		t.Fatalf("Organization = %q, want %q", info.Organization, dataitem.OrganizationWhole)
	}
	if info.EntryPath != "dataset" {
		t.Fatalf("EntryPath = %q, want dataset", info.EntryPath)
	}
	if len(info.ComponentFiles) != 2 {
		t.Fatalf("ComponentFiles len = %d, want 2", len(info.ComponentFiles))
	}
	if mode := commonJSON.String(info.Attributes, "format_info.parquet", "mode"); mode != "whole" {
		t.Fatalf("mode = %v, want whole", mode)
	}
}

func TestLakeTableDetectorRulesDeclareSingleFileAndWholeScope(t *testing.T) {
	t.Parallel()

	d := &lakeTableItemDetector{}
	rules := d.Rules()
	if len(rules) != 2 {
		t.Fatalf("Rules len = %d, want 2", len(rules))
	}

	seenSingle := false
	seenWhole := false
	for _, rule := range rules {
		if err := dataitem.ValidateFormatRule(rule); err != nil {
			t.Fatalf("ValidateFormatRule(%s/%s) error = %v", rule.Format, rule.Organization, err)
		}
		switch rule.Organization {
		case dataitem.OrganizationSingle:
			seenSingle = true
		case dataitem.OrganizationWhole:
			seenWhole = true
			if rule.WholeScope == nil || !rule.WholeScope.ExclusiveOnStrongHit {
				t.Fatalf("whole scope rule = %#v, want explicit exclusive rule", rule.WholeScope)
			}
		}
	}
	if !seenSingle || !seenWhole {
		t.Fatalf("rules missing single=%v whole=%v", seenSingle, seenWhole)
	}
}

func TestLakeTableDetectorAllowsAuxiliaryFiles(t *testing.T) {
	t.Parallel()

	d := &lakeTableItemDetector{}
	files := []plugin.FileEntry{
		{Name: "part-000.parquet", Path: "dataset/part-000.parquet", Size: 10},
		{Name: "_SUCCESS", Path: "dataset/_SUCCESS", Size: 0},
		{Name: "._metadata.crc", Path: "dataset/._metadata.crc", Size: 4},
	}

	if !d.Detect(context.Background(), files, nil) {
		t.Fatal("expected parquet directory with auxiliary files to match")
	}
	info, err := extractLakeTableWholeScopeInfo(context.Background(), nil, nil, 1, "dataset", files, nil)
	if err != nil {
		t.Fatalf("extractLakeTableWholeScopeInfo() error = %v", err)
	}
	if len(info.ComponentFiles) != 1 || info.ComponentFiles[0] != "dataset/part-000.parquet" {
		t.Fatalf("ComponentFiles = %#v, want only parquet data file", info.ComponentFiles)
	}
	if info.SizeBytes == nil || *info.SizeBytes != 10 {
		t.Fatalf("SizeBytes = %v, want 10", info.SizeBytes)
	}
}

func TestLakeTableDetectorRejectsMixedWholeScope(t *testing.T) {
	t.Parallel()

	d := &lakeTableItemDetector{}
	files := []plugin.FileEntry{
		{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet"},
		{Name: "README.txt", Path: "dataset/README.txt"},
	}
	subdirs := []plugin.DirEntry{{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"}}

	if d.Detect(context.Background(), files, subdirs) {
		t.Fatal("expected mixed whole scope to be rejected")
	}
}

func TestLakeTableDetectorResolvesWholeScopeFromRecursiveScope(t *testing.T) {
	t.Parallel()

	d := &lakeTableItemDetector{}
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
	if item.Organization != dataitem.OrganizationWhole {
		t.Fatalf("Organization = %q, want whole", item.Organization)
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

	d := &lakeTableItemDetector{}
	files := []plugin.FileEntry{
		{Name: "sales.parquet", Path: "dataset/sales.parquet"},
		{Name: "customers.parquet", Path: "dataset/customers.parquet"},
	}

	if d.Detect(context.Background(), files, nil) {
		t.Fatal("expected independent sibling parquet files to remain single-file items")
	}
}
