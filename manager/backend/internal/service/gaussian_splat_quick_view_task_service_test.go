package service

import (
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestGaussianSplatQuickViewTaskNormalizesKSplatConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":      "addp://engine/26/path/models/model.ksplat?type=file&item_id=77",
			"source_engine_id":  uint(26),
			"item_fingerprint":  "fp-ksplat",
			"item_id":           uint(77),
			"format":            "ksplat",
			"source_size_bytes": int64(4096),
		},
	}

	cfg, err := normalizeGaussianSplatQuickViewTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize gaussian splat quick view config: %v", err)
	}
	if cfg.Source.Format != "ksplat" {
		t.Fatalf("source format = %q, want ksplat", cfg.Source.Format)
	}
	if cfg.Result.FileName != "model.ksplat" {
		t.Fatalf("result file_name = %q, want model.ksplat", cfg.Result.FileName)
	}
	if !strings.Contains(cfg.Result.StorageRef, "tenant_7/gaussian-splat-quick-view/fp-ksplat/model.ksplat") {
		t.Fatalf("storage_ref = %q, want tenant-scoped KSplat artifact", cfg.Result.StorageRef)
	}
}

func TestGaussianSplatQuickViewTaskRejectsPLYSourceUntilConverterIsAvailable(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/model.ply?type=file&item_id=77",
			"source_engine_id": uint(26),
			"item_fingerprint": "fp-ply",
			"item_id":          uint(77),
			"format":           "ply",
		},
	}

	_, err := normalizeGaussianSplatQuickViewTaskConfig(config, "manager", 7)
	if err == nil {
		t.Fatal("normalize gaussian splat quick view config error is nil, want PLY rejection")
	}
	if got := err.Error(); got != "gaussian splat quick view config.source.format must be ksplat" {
		t.Fatalf("error = %q, want KSplat-only source rejection", got)
	}
}

func TestGaussianSplatQuickViewTaskRejectsSplatSourceUntilConverterIsAvailable(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/model.splat?type=file&item_id=78",
			"source_engine_id": uint(26),
			"item_fingerprint": "fp-splat",
			"item_id":          uint(78),
			"format":           "splat",
		},
	}

	_, err := normalizeGaussianSplatQuickViewTaskConfig(config, "manager", 7)
	if err == nil {
		t.Fatal("normalize gaussian splat quick view config error is nil, want SPLAT rejection")
	}
	if got := err.Error(); got != "gaussian splat quick view config.source.format must be ksplat" {
		t.Fatalf("error = %q, want KSplat-only source rejection", got)
	}
}
