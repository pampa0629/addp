package scanprocessor

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

func TestKnownJPEGRefreshRemovesOldGPSOnlyOnSuccessfulDescription(t *testing.T) {
	var content bytes.Buffer
	if err := jpeg.Encode(&content, image.NewRGBA(image.Rect(0, 0, 7, 5)), nil); err != nil {
		t.Fatal(err)
	}
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "GPS removed", false: "description failed"}[valid], func(t *testing.T) {
			db := openObjectCatalogProcessorTestDB(t)
			repo := metaRepo.NewScanRepository(db)
			parent, err := repo.UpsertNode(1, 7, nil, "bucket", "addp", strPtr("addp"), scanresource.ObjectBucketNodeAttributes("addp"))
			if err != nil {
				t.Fatal(err)
			}
			size := int64(content.Len())
			item, err := repo.UpsertItemWithDepth(1, 7, parent, "object", "gps.jpg", "addp/gps.jpg", models.JSONMap{
				"item":      map[string]interface{}{"layout": "single", "data_type": "media", "format": "jpeg"},
				"storage":   map[string]interface{}{"physical_path": "addp/gps.jpg"},
				"type_info": map[string]interface{}{"media": map[string]interface{}{"kind": "image", "width": 2, "height": 3}},
				"format_info": map[string]interface{}{"jpeg": map[string]interface{}{
					"exif_status": "parsed", "exif": map[string]interface{}{"gps": map[string]interface{}{"map_datum": "WGS-84"}},
				}},
				"capabilities": map[string]interface{}{
					"spatial":    map[string]interface{}{"capture_location": map[string]interface{}{"latitude": 0, "longitude": -120.25, "datum": "WGS-84", "srid": 4326}},
					"extraction": map[string]interface{}{"status": "ready"},
				},
			}, nil, &size, nil, models.ScannedDepthDeep)
			if err != nil {
				t.Fatal(err)
			}
			detected := scanflow.KnownItemDetectedItem(item, scanflow.KnownItemDescriptorFromAttributes(item.Attributes))
			data := content.String()
			if !valid {
				data = "broken JPEG"
			}
			_, err = New(repo, nil, nil).Process(context.Background(), KnownItemInput(
				&commonModels.Engine{ID: 7}, 1, parent, *item, item.Attributes, detected,
				staticObjectContentReader{content: data}, nil,
				func(string) plugin.EngineCatalogPath { return plugin.ObjectItemPath(7, "addp", "gps.jpg") },
				"addp/gps.jpg", "addp", "gps.jpg", size,
			))
			if (err == nil) != valid {
				t.Fatalf("refresh error = %v, valid=%v", err, valid)
			}
			var stored models.MetaItem
			if err := db.First(&stored, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.FullName != item.FullName || commonJSON.String(stored.Attributes, "storage", "physical_path") != "addp/gps.jpg" || commonJSON.String(stored.Attributes, "capabilities.extraction", "status") != "ready" {
				t.Fatalf("refresh changed identity or unrelated facts: %#v", stored)
			}
			if (len(commonJSON.Section(stored.Attributes, "capabilities.spatial.capture_location")) == 0) != valid ||
				(len(commonJSON.Section(stored.Attributes, "format_info.jpeg.exif.gps")) == 0) != valid {
				t.Fatalf("persisted stale GPS or erased facts on failure: %#v", stored.Attributes)
			}
			wantWidth := int64(2)
			if valid {
				wantWidth = 7
			}
			if commonJSON.Int64(stored.Attributes, "type_info.media", "width") != wantWidth {
				t.Fatalf("persisted wrong media snapshot: %#v", stored.Attributes)
			}
		})
	}
}
