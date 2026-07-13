package service

import (
	"testing"

	commonModels "github.com/addp/common/models"
	rastercogref "github.com/addp/manager/internal/cog"
)

func TestNormalizeCADPreviewTaskConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/cad/site.dwg?type=file&item_id=91",
			"source_engine_id": uint(26),
			"item_fingerprint": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"item_id":          uint(91),
			"format":           "DWG",
		},
	}
	got, err := normalizeCADPreviewTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalizeCADPreviewTaskConfig() error = %v", err)
	}
	if got.Source.Format != "dwg" || got.Options.TileSize != 512 || got.Options.MaxZoom != 4 {
		t.Fatalf("normalized config = %#v", got)
	}
	bucket, object, err := rastercogref.ObjectLocation(got.Result.StorageRef, "manager")
	if err != nil {
		t.Fatalf("parse storage_ref: %v", err)
	}
	wantObject := "tenant_7/cad-previews/" + got.Source.ItemFingerprint
	if bucket != "manager" || object != wantObject {
		t.Fatalf("storage_ref = %q, bucket = %q, object = %q", got.Result.StorageRef, bucket, object)
	}
}

func TestNormalizeCADPreviewTaskConfigAcceptsDXF(t *testing.T) {
	got, err := normalizeCADPreviewTaskConfig(commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/cad/site.dxf?type=file",
			"source_engine_id": uint(26),
			"item_fingerprint": "fingerprint",
			"format":           "dxf",
		},
	}, "manager", 7)
	if err != nil {
		t.Fatalf("normalize DXF config: %v", err)
	}
	if got.Source.Format != "dxf" {
		t.Fatalf("source format = %q, want dxf", got.Source.Format)
	}
}

func TestNormalizeCADPreviewTaskConfigRejectsNonCAD(t *testing.T) {
	_, err := normalizeCADPreviewTaskConfig(commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/docs/site.pdf?type=file",
			"source_engine_id": uint(26),
			"item_fingerprint": "fingerprint",
			"format":           "pdf",
		},
	}, "manager", 7)
	if err == nil {
		t.Fatal("expected non-CAD config to be rejected")
	}
}

func TestNormalizeCADPreviewTaskConfigAllowsZeroMaxZoom(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/cad/site.dwg?type=file",
			"source_engine_id": uint(26),
			"item_fingerprint": "fingerprint",
			"format":           "dwg",
		},
		"options": commonModels.JSONMap{"tile_size": 256, "max_zoom": 0},
	}
	got, err := normalizeCADPreviewTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize zero max_zoom: %v", err)
	}
	if got.Options.MaxZoom != 0 {
		t.Fatalf("max_zoom = %d, want 0", got.Options.MaxZoom)
	}
}

func TestNormalizeCADPreviewTaskConfigRejectsExcessiveTileCount(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/cad/site.dwg?type=file",
			"source_engine_id": uint(26),
			"item_fingerprint": "fingerprint",
			"format":           "dwg",
		},
		"options": commonModels.JSONMap{"tile_size": 512, "max_zoom": 8},
	}
	if _, err := normalizeCADPreviewTaskConfig(config, "manager", 7); err == nil {
		t.Fatal("expected zoom 8 tile count to be rejected")
	}
}

func TestValidateCADPreviewArtifactRef(t *testing.T) {
	for _, ref := range []string{"manifest.json", "model-space/0/0/0.webp"} {
		if err := validateCADPreviewArtifactRef(ref); err != nil {
			t.Fatalf("validate %q: %v", ref, err)
		}
	}
	for _, ref := range []string{"", "../manifest.json", "/manifest.json", "dir/../manifest.json", `dir\..\manifest.json`} {
		if err := validateCADPreviewArtifactRef(ref); err == nil {
			t.Fatalf("expected %q to be rejected", ref)
		}
	}
}
