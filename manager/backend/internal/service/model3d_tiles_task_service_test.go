package service

import (
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestModel3DTilesTaskNormalizesInfraResultByTargetFormat(t *testing.T) {
	for _, targetFormat := range []string{"3d_tiles", "s3m"} {
		t.Run(targetFormat, func(t *testing.T) {
			config := commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"item_locator": "addp://engine/26/path/models/osgb?type=item&item_id=77", "source_engine_id": uint(26),
					"item_fingerprint": "fp-osgb-scene", "item_id": uint(77), "source_size_bytes": int64(1024),
				},
				"target_format": targetFormat,
			}
			cfg, err := normalizeModel3DTilesTaskConfig(config, "manager", 7)
			if err != nil {
				t.Fatalf("normalize model3d tiles config: %v", err)
			}
			if cfg.Source.Format != "osgb_scene" || cfg.TargetFormat != targetFormat {
				t.Fatalf("config = %+v", cfg)
			}
			if !strings.Contains(cfg.Result.StorageRef, "tenant_7/model3d-tiles/fp-osgb-scene/"+targetFormat) {
				t.Fatalf("storage_ref = %q", cfg.Result.StorageRef)
			}
		})
	}
}

func TestModel3DTilesTaskRejectsInvalidSourceOrTargetFormat(t *testing.T) {
	base := commonModels.JSONMap{"source": commonModels.JSONMap{"item_locator": "addp://engine/26/path/models/tiles?type=item&item_id=77", "source_engine_id": uint(26), "item_fingerprint": "fp", "format": "3dtiles"}, "target_format": "3d_tiles"}
	if _, err := normalizeModel3DTilesTaskConfig(base, "manager", 7); err == nil || err.Error() != "model 3d tiles config.source.format must be osgb_scene" {
		t.Fatalf("source error = %v", err)
	}
	base["source"].(commonModels.JSONMap)["format"] = "osgb_scene"
	base["target_format"] = "unknown"
	if _, err := normalizeModel3DTilesTaskConfig(base, "manager", 7); err == nil || err.Error() != "model3d tiles config.target_format must be 3d_tiles or s3m" {
		t.Fatalf("target error = %v", err)
	}
}
