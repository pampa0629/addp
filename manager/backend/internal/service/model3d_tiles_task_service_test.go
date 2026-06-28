package service

import (
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestModel3DTilesTaskNormalizesOSGBSceneTo3DTilesConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/osgb?type=item&item_id=77",
			"source_engine_id": uint(26),
		},
		"target": commonModels.JSONMap{
			"storage_locator":  "addp://engine/27/path/models/tiles?type=node",
			"target_engine_id": uint(27),
			"dataset_name":     "white_tower_3dtiles",
		},
	}

	cfg, err := normalizeModel3DTilesTaskConfig(config)
	if err != nil {
		t.Fatalf("normalize model 3d tiles config: %v", err)
	}
	if cfg.Source.Format != "osgb_scene" {
		t.Fatalf("source format = %q, want osgb_scene", cfg.Source.Format)
	}
	if cfg.Target.DatasetName != "white_tower_3dtiles" {
		t.Fatalf("dataset name = %q, want white_tower_3dtiles", cfg.Target.DatasetName)
	}
	tiles, ok := asJSONMap(config["tiles"])
	if !ok {
		t.Fatalf("tiles = %#v, want normalized object", config["tiles"])
	}
	if got := stringFromConfig(tiles["format"]); got != "3dtiles" {
		t.Fatalf("tiles.format = %q, want 3dtiles", got)
	}
}

func TestModel3DTilesTaskRejectsNonOSGBSceneSource(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/tiles?type=item&item_id=77",
			"source_engine_id": uint(26),
			"format":           "3dtiles",
		},
		"target": commonModels.JSONMap{
			"storage_locator":  "addp://engine/27/path/models/tiles?type=node",
			"target_engine_id": uint(27),
		},
	}

	_, err := normalizeModel3DTilesTaskConfig(config)
	if err == nil {
		t.Fatal("normalize model 3d tiles config error is nil, want non-OSGB-scene rejection")
	}
	if got := err.Error(); got != "model 3d tiles config.source.format must be osgb_scene" {
		t.Fatalf("error = %q, want source format rejection", got)
	}
}
