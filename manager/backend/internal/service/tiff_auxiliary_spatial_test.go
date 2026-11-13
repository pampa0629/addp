package service

import (
	commonModels "github.com/addp/common/models"
	"testing"
)

// Regression for the Hosted TIFF whose primary has COG layout but whose
// georeferencing is supplied only by its registered PAM and world file.
func TestTIFFCOGLayoutMultiGenerationUsesCommittedSpatialFacts(t *testing.T) {
	for _, located := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_facts", true: "auxiliary_facts"}[located], func(t *testing.T) {
			attrs := map[string]interface{}{
				"item":        map[string]interface{}{"data_type": "media", "format": "tiff", "layout": "multi"},
				"type_info":   map[string]interface{}{"media": map[string]interface{}{"width": 256, "height": 256}},
				"format_info": map[string]interface{}{"tiff": map[string]interface{}{"profile": "cog", "cog_check_level": "heuristic", "is_cloud_optimized": true}},
			}
			if located {
				attrs["capabilities"] = map[string]interface{}{"spatial": map[string]interface{}{"srid": 4326, "extent": []float64{113, 27.9744, 113.0256, 28}}}
			}
			locator := "addp://engine/6/path/images/scene.tif?type=file&item_id=62"
			raster := RasterQuickViewSourceFromAttributes(attrs, locator, 6)
			svc := NewQuickViewService(newTileCacheTaskServiceTestDB(t), nil)
			cap, err := svc.BuildCapabilityFromSource(t.Context(), QuickViewSource{Identity: QuickViewIdentity{TenantID: 2, ItemFingerprint: commonModels.GenerateItemFingerprint(6, "images/scene.tif"), Locator: locator}, EngineID: 6, Raster: raster})
			if err != nil {
				t.Fatal(err)
			}
			generate := false
			for _, action := range cap.AvailableActions {
				if action == QuickViewActionGenerateRasterCOG {
					generate = true
				}
			}
			if generate != located || cap.CanUseQuickView {
				t.Fatalf("located=%v actions=%v quick_view=%v", located, cap.AvailableActions, cap.CanUseQuickView)
			}
			reason := RasterUnavailableReasonMissingSpatialExtent
			if located {
				reason = RasterUnavailableReasonRequiresCOGGeneration
			}
			if cap.UnavailableReason != reason || cap.Raster.Profile != "cog" {
				t.Fatalf("unexpected capability: %#v", cap)
			}
		})
	}
}
