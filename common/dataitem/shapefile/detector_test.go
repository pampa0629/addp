package shapefile

import (
	"context"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
)

func TestDetectorDetectsCompleteShapefile(t *testing.T) {
	d := &Detector{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "bucket/roads/roads.shp", Size: 10},
		{Name: "roads.shx", Path: "bucket/roads/roads.shx", Size: 20},
		{Name: "roads.dbf", Path: "bucket/roads/roads.dbf", Size: 30},
		{Name: "roads.prj", Path: "bucket/roads/roads.prj", Size: 40},
	}

	if !d.Detect(context.Background(), files, nil) {
		t.Fatal("expected complete shapefile component set to match")
	}

	info, err := d.ExtractItemInfo(context.Background(), nil, nil, 1, "bucket/roads", files)
	if err != nil {
		t.Fatalf("ExtractItemInfo() error = %v", err)
	}
	if info.CompositionType != dataitem.CompositionTypeMultiFile {
		t.Fatalf("CompositionType = %q, want %q", info.CompositionType, dataitem.CompositionTypeMultiFile)
	}
	if info.Format != "shapefile" {
		t.Fatalf("Format = %q, want shapefile", info.Format)
	}
	if got, want := len(info.ComponentFiles), len(files); got != want {
		t.Fatalf("ComponentFiles len = %d, want %d", got, want)
	}
}

func TestDetectorRejectsIncompleteShapefile(t *testing.T) {
	d := &Detector{}
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "bucket/roads/roads.shp"},
		{Name: "roads.dbf", Path: "bucket/roads/roads.dbf"},
	}

	if d.Detect(context.Background(), files, nil) {
		t.Fatal("expected incomplete shapefile component set to be rejected")
	}
}
