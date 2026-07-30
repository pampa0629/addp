package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

func TestGaussianSplatKSplatTaskNormalizesSupportedSources(t *testing.T) {
	for _, tc := range []struct {
		name        string
		format      string
		locator     string
		fingerprint string
		wantFile    string
	}{
		{
			name:        "ply",
			format:      "ply",
			locator:     "addp://engine/26/path/models/model.ply?type=file&item_id=77",
			fingerprint: "fp-ply",
			wantFile:    "model.ksplat",
		},
		{
			name:        "splat",
			format:      "splat",
			locator:     "addp://engine/26/path/models/model.splat?type=file&item_id=78",
			fingerprint: "fp-splat",
			wantFile:    "model.ksplat",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"item_locator":                tc.locator,
					"source_engine_id":            uint(26),
					"item_fingerprint":            tc.fingerprint,
					"item_id":                     uint(77),
					"format":                      tc.format,
					"source_size_bytes":           int64(4096),
					"sampled_bounds_3d":           commonModels.JSONMap{"min_x": 1.0, "min_y": 2.0, "min_z": 3.0, "max_x": 5.0, "max_y": 6.0, "max_z": 7.0},
					"sampled_bounds_sample_count": int64(2048),
				},
			}

			cfg, err := normalizeGaussianSplatKSplatTaskConfig(config, "manager", 7)
			if err != nil {
				t.Fatalf("normalize gaussian splat KSplat config: %v", err)
			}
			if cfg.Source.Format != tc.format {
				t.Fatalf("source format = %q, want %s", cfg.Source.Format, tc.format)
			}
			if cfg.Result.FileName != tc.wantFile {
				t.Fatalf("result file_name = %q, want %s", cfg.Result.FileName, tc.wantFile)
			}
			if !strings.Contains(cfg.Result.StorageRef, "tenant_7/gaussian-splat-ksplat/"+tc.fingerprint+"/"+tc.wantFile) {
				t.Fatalf("storage_ref = %q, want tenant-scoped KSplat artifact", cfg.Result.StorageRef)
			}
			if cfg.Source.SampledBounds3D == nil || cfg.Source.SampledBounds3D.MinX == nil || *cfg.Source.SampledBounds3D.MinX != 1.0 {
				t.Fatalf("sampled bounds = %#v, want normalized sampled bounds", cfg.Source.SampledBounds3D)
			}
			if cfg.Source.SampledBoundsSampleCount == nil || *cfg.Source.SampledBoundsSampleCount != 2048 {
				t.Fatalf("sampled bounds sample count = %#v, want 2048", cfg.Source.SampledBoundsSampleCount)
			}
			normalizedSource, ok := asJSONMap(config["source"])
			if !ok || normalizedSource["sampled_bounds_3d"] == nil {
				t.Fatalf("normalized source = %#v, want sampled bounds preserved", config["source"])
			}
		})
	}
}

func TestApplyGaussianSplatBoundsOptions(t *testing.T) {
	explicitCenter := []interface{}{9.0, 8.0, 7.0}
	count := int64(4096)
	options := commonModels.JSONMap{
		"scene_center": explicitCenter,
	}
	source := GaussianSplatKSplatSourceConfig{
		Bounds3D: bounds3DFromTaskConfig(commonModels.JSONMap{
			"min_x": 1.0, "min_y": 2.0, "min_z": 3.0,
			"max_x": 4.0, "max_y": 6.0, "max_z": 8.0,
		}),
		SampledBounds3D: bounds3DFromTaskConfig(commonModels.JSONMap{
			"min_x": -1.0, "min_y": -2.0, "min_z": -3.0,
			"max_x": 1.0, "max_y": 2.0, "max_z": 3.0,
		}),
		SampledBoundsSampleCount: &count,
	}

	applyGaussianSplatBoundsOptions(options, source)

	if !reflect.DeepEqual(options["scene_center"], explicitCenter) {
		t.Fatalf("scene_center = %#v, want explicit value preserved", options["scene_center"])
	}
	if options["bounds_3d"] == nil || options["sampled_bounds_3d"] == nil {
		t.Fatalf("options = %#v, want bounds added", options)
	}
	if options["sampled_bounds_sample_count"] != count {
		t.Fatalf("sampled_bounds_sample_count = %#v, want %d", options["sampled_bounds_sample_count"], count)
	}
}

func TestGaussianSplatKSplatTaskEnrichesBoundsFromMetaItem(t *testing.T) {
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/77" {
			t.Fatalf("path = %q, want /api/v1/meta/items/77", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("Meta request must not send legacy internal authentication headers")
		}
		item := commonModels.MetaItem{
			ID:       77,
			TenantID: 7,
			EngineID: 26,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "gaussian_splat",
					"format":    "ply",
					"layout":    "single",
				},
				"type_info": map[string]interface{}{
					"gaussian_splat": map[string]interface{}{
						"representation":              "3d_gaussian_splatting",
						"splat_count":                 1024,
						"sampled_bounds_3d":           map[string]interface{}{"min_x": -80.0, "min_y": -20.0, "min_z": 1515.0, "max_x": -26.0, "max_y": 21.0, "max_z": 1530.0},
						"sampled_bounds_method":       "sampled_binary_vertices",
						"sampled_bounds_sample_count": 8192,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(item)
	}))
	defer metaServer.Close()

	task := &models.GaussianSplatKSplatTask{
		TenantID: 7,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"item_locator":      "addp://engine/26/path/3d/ply/model.ply?type=file&item_id=77",
				"source_engine_id":  uint(26),
				"item_fingerprint":  "fp-ply",
				"item_id":           uint(77),
				"format":            "ply",
				"source_size_bytes": int64(4096),
			},
		},
	}
	svc := NewGaussianSplatKSplatTaskService(nil)
	svc.SetMetaClient(newServiceTestMetaClient(metaServer.URL))

	if err := svc.enrichGaussianSplatKSplatTaskSourceFacts(context.Background(), task); err != nil {
		t.Fatalf("enrich gaussian splat KSplat source facts: %v", err)
	}
	cfg, err := normalizeGaussianSplatKSplatTaskConfig(task.Config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize gaussian splat KSplat config: %v", err)
	}
	if cfg.Source.SampledBounds3D == nil || cfg.Source.SampledBounds3D.MinX == nil || *cfg.Source.SampledBounds3D.MinX != -80.0 {
		t.Fatalf("sampled bounds = %#v, want meta item sampled bounds", cfg.Source.SampledBounds3D)
	}
	if cfg.Source.SampledBoundsSampleCount == nil || *cfg.Source.SampledBoundsSampleCount != 8192 {
		t.Fatalf("sampled bounds sample count = %#v, want 8192", cfg.Source.SampledBoundsSampleCount)
	}
}

func TestGaussianSplatKSplatTaskRejectsUnsupportedSource(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/model.ksplat?type=file&item_id=78",
			"source_engine_id": uint(26),
			"item_fingerprint": "fp-ksplat",
			"item_id":          uint(78),
			"format":           "ksplat",
		},
	}

	_, err := normalizeGaussianSplatKSplatTaskConfig(config, "manager", 7)
	if err == nil {
		t.Fatal("normalize gaussian splat KSplat config error is nil, want unsupported source rejection")
	}
	if got := err.Error(); got != "gaussian splat KSplat config.source.format must be ply or splat" {
		t.Fatalf("error = %q, want supported source format list", got)
	}
}
