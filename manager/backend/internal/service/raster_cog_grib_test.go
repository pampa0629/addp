package service

import (
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

func TestGRIBMapCacheIdentityIncludesMessageAndCurrentSourceVersion(t *testing.T) {
	version := "sha256:" + strings.Repeat("a", 64)
	info := map[string]interface{}{"scan_complete": true, "encoding": "GRIB2", "source_version": version}
	attrs := map[string]interface{}{
		"item":        map[string]interface{}{"format": "grib", "layout": "single", "data_type": "container"},
		"format_info": map[string]interface{}{"grib": info},
		"type_info": map[string]interface{}{"container": map[string]interface{}{"resource_count": 1, "children": []map[string]interface{}{
			{"name": "message-1", "format": "grib", "child_kind": "array", "native": map[string]interface{}{"map_profile": GRIBTemperatureMapProfile}},
			{"name": "message-2", "format": "grib", "child_kind": "array", "native": map[string]interface{}{"map_profile": GRIBTemperatureMapProfile}},
			{"name": "message-3", "format": "grib", "child_kind": "array", "native": map[string]interface{}{}},
		}}},
	}
	first := GRIBRasterQuickViewSource(attrs, "message-1")
	second := GRIBRasterQuickViewSource(attrs, "message-2")
	if first == nil || second == nil || GRIBRasterQuickViewSource(attrs, "message-3") != nil || GRIBRasterQuickViewSource(attrs, "message-01") != nil {
		t.Fatal("GRIB map did not enforce the selected native profile")
	}
	result := &models.RasterCOG{ID: 52, Status: models.RasterCOGStatusReady, SourceProfile: GRIBTemperatureMapProfile,
		Metadata: commonModels.JSONMap{"grib_map": commonModels.JSONMap{"message": first.Message, "source_version": first.SourceVersion}}}
	if !rasterCOGFactsMatch(result, QuickViewIdentity{}, first, 14) || rasterCOGFactsMatch(result, QuickViewIdentity{}, second, 14) {
		t.Fatal("raster COG crossed GRIB message identity")
	}
	mapInfo := renderInfoFromRasterCOG(result, first)
	if mapInfo.RasterMessage != first.Message || mapInfo.RasterSourceVersion != version || mapInfo.MapProfile != GRIBTemperatureMapProfile ||
		mapInfo.PreviewURL != "/api/v1/manager/raster_cog/52/content?source_version="+version || mapInfo.ExtentSRID != 0 {
		t.Fatalf("unexpected GRIB map delivery: %+v", mapInfo)
	}
	info["source_version"] = "sha256:" + strings.Repeat("b", 64)
	if rasterCOGFactsMatch(result, QuickViewIdentity{}, GRIBRasterQuickViewSource(attrs, "message-1"), 14) {
		t.Fatal("raster COG crossed committed GRIB source version")
	}
}
