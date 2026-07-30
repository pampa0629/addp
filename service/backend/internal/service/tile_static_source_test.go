package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
)

func TestNormalizeStaticTileSourceFreezesPMTilesCameraAndSpatialFacts(t *testing.T) {
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/51561" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 51561, "tenant_id": 1, "engine_id": 9,
			"name": "farmland.pmtiles", "full_name": "addp/vector-tiles/farmland.pmtiles",
			"fingerprint": "4a42b5e8db05d2935562760e85afa7a55910ce1ff6f952d8cba943e99a6dd351",
			"attributes": map[string]interface{}{
				"item": map[string]interface{}{"data_type": "media", "format": "pmtiles", "layout": "single"},
				"format_info": map[string]interface{}{"pmtiles": map[string]interface{}{
					"spec_version": 3, "tile_type": "mvt", "tile_compression": "gzip",
					"header_hash": "9b1d2090e99e4c1ac4e132e274c5dbc92fdcc2757021c503d676ff2d82bf3971",
					"min_zoom":    4, "max_zoom": 12,
					"center": []interface{}{111.4499249, 27.3849525, 4},
				}},
				"capabilities": map[string]interface{}{"spatial": map[string]interface{}{
					"srid": 4326, "crs_ref": "EPSG:4326",
					"extent": []interface{}{108.5564817, 24.5258548, 114.343368, 30.2440502},
				}},
			},
		})
	}))
	defer metaServer.Close()

	metaClient := commonClient.NewMetaClient(metaServer.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}))
	service := NewTileServiceService(nil, metaClient, "")
	config := map[string]interface{}{"source": map[string]interface{}{
		"locator": "addp://engine/9/path/addp/vector-tiles/farmland.pmtiles?type=object&item_id=51561",
	}}
	if err := service.normalizeStaticTileSource(1, config); err != nil {
		t.Fatalf("normalizeStaticTileSource() error = %v", err)
	}

	snapshot := config["source_snapshot"].(map[string]interface{})
	if !reflect.DeepEqual(snapshot["center"], []float64{111.4499249, 27.3849525, 4}) {
		t.Fatalf("center = %#v", snapshot["center"])
	}
	spatial := snapshot["spatial"].(map[string]interface{})
	if !reflect.DeepEqual(spatial["extent"], []float64{108.5564817, 24.5258548, 114.343368, 30.2440502}) {
		t.Fatalf("spatial extent = %#v", spatial["extent"])
	}
	if spatial["srid"] != 4326 || spatial["crs_ref"] != "EPSG:4326" {
		t.Fatalf("spatial = %#v", spatial)
	}
}
