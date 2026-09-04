package preview

import (
	"testing"

	"github.com/addp/manager/internal/models"
)

func TestThreeDTilesManifestObjectPathUsesRelativeManifestRef(t *testing.T) {
	t.Parallel()
	attrs := map[string]interface{}{
		"format_info": map[string]interface{}{
			"3dtiles": map[string]interface{}{
				"manifest_ref": "tileset.json",
			},
		},
	}

	got := threeDTilesManifestObjectPath("addp", "3d/mars3d-qx-dyt-3dtiles", attrs)
	if got != "3d/mars3d-qx-dyt-3dtiles/tileset.json" {
		t.Fatalf("manifest path = %q, want scope-relative tileset path", got)
	}
}

func TestThreeDTilesManifestObjectPathKeepsCompleteRef(t *testing.T) {
	t.Parallel()
	attrs := map[string]interface{}{
		"item": map[string]interface{}{
			"refs": []interface{}{
				map[string]interface{}{
					"path":    "3d/city/tileset.json",
					"role":    "manifest",
					"primary": true,
				},
			},
		},
	}

	got := threeDTilesManifestObjectPath("addp", "3d/city", attrs)
	if got != "3d/city/tileset.json" {
		t.Fatalf("manifest path = %q, want complete ref unchanged", got)
	}
}

func TestThreeDTilesManifestObjectPathStripsBucketPrefix(t *testing.T) {
	t.Parallel()
	attrs := map[string]interface{}{
		"item": map[string]interface{}{
			"refs": []interface{}{
				map[string]interface{}{
					"path":    "addp/3d/city/tileset.json",
					"role":    "manifest",
					"primary": true,
				},
			},
		},
	}

	got := threeDTilesManifestObjectPath("addp", "3d/city", attrs)
	if got != "3d/city/tileset.json" {
		t.Fatalf("manifest path = %q, want bucketless object path", got)
	}
}

func TestS3MManifestObjectPathUsesNestedConfigRef(t *testing.T) {
	t.Parallel()
	attrs := map[string]interface{}{
		"format_info": map[string]interface{}{
			"s3m": map[string]interface{}{
				"manifest_ref": "config/city.scp",
			},
		},
	}
	got := s3mManifestObjectPath("addp", "3d/city", attrs)
	if got != "3d/city/config/city.scp" {
		t.Fatalf("manifest path = %q, want nested S3M config path", got)
	}
}

func TestBuildStorageAssetURLPreservesPathSegments(t *testing.T) {
	t.Parallel()
	got := buildStorageAssetURL("addp://engine/26/path/addp/三维模型?type=object&item_id=88", 26, "addp/三维模型/config/city.scp")
	want := "/api/v1/manager/storage-assets/26/items/88/addp/%E4%B8%89%E7%BB%B4%E6%A8%A1%E5%9E%8B/config/city.scp"
	if got != want {
		t.Fatalf("asset URL = %q, want %q", got, want)
	}
}

func TestStorageContentURLsRequireMatchingDataItemLocator(t *testing.T) {
	t.Parallel()
	valid := "addp://engine/26/path/addp/report.pdf?type=object&item_id=88"
	if got := buildStorageStreamURL(valid, 26, "addp/report.pdf"); got == "" {
		t.Fatal("buildStorageStreamURL() rejected a matching DataItem locator")
	}
	for _, locator := range []string{
		"addp://engine/26/path/addp/report.pdf?type=object",
		"addp://engine/27/path/addp/report.pdf?type=object&item_id=88",
		"addp://engine/26/path/addp?type=bucket&item_id=88",
	} {
		if got := buildStorageStreamURL(locator, 26, "addp/report.pdf"); got != "" {
			t.Fatalf("buildStorageStreamURL(%q) = %q, want no URL", locator, got)
		}
	}
}

func TestApplyS3MScenePreviewBuildsControlledManifestURL(t *testing.T) {
	t.Parallel()
	object := &models.ObjectPreview{}
	attrs := map[string]interface{}{
		"item": map[string]interface{}{"format": "s3m", "data_type": "model_3d", "layout": "whole"},
		"format_info": map[string]interface{}{
			"s3m": map[string]interface{}{
				"manifest_ref": "config/scene.scp", "manifest_encoding": "xml", "tile_extension": ".s3m",
			},
		},
	}
	if !applyS3MScenePreview(attrs, object, "addp://engine/26/path/addp/3d/city?type=object&item_id=89", 26, "addp", "3d/city") {
		t.Fatal("applyS3MScenePreview() = false, want S3M preview")
	}
	wantURL := "/api/v1/manager/storage-assets/26/items/89/addp/3d/city/config/scene.scp"
	if object.Content == nil || object.Content.FrontendRenderer != "s3m" || object.Content.URL != wantURL {
		t.Fatalf("object = %#v, want controlled S3M URL", object)
	}
}
