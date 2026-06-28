package preview

import "testing"

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
