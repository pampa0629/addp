package service

import (
	"context"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModel3DGLBTaskNormalizesSingleOSGBConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":      "addp://engine/26/path/models/tile.osgb?type=item&item_id=77",
			"source_engine_id":  uint(26),
			"item_fingerprint":  "fp-osgb",
			"item_id":           uint(77),
			"source_size_bytes": int64(1024),
		},
	}

	cfg, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize model 3d GLB config: %v", err)
	}
	if cfg.Source.Format != "osgb" {
		t.Fatalf("source format = %q, want osgb", cfg.Source.Format)
	}
	if cfg.Result.FileName != "tile.glb" {
		t.Fatalf("result file_name = %q, want tile.glb", cfg.Result.FileName)
	}
	if !strings.Contains(cfg.Result.StorageRef, "tenant_7/model3d-quick-view/fp-osgb/tile.glb") {
		t.Fatalf("storage_ref = %q, want tenant-scoped GLB artifact", cfg.Result.StorageRef)
	}
	source, ok := asJSONMap(config["source"])
	if !ok || stringFromConfig(source["format"]) != "osgb" {
		t.Fatalf("normalized source = %#v, want osgb format", config["source"])
	}
	result, ok := asJSONMap(config["result"])
	if !ok || stringFromConfig(result["file_name"]) != "tile.glb" {
		t.Fatalf("normalized result = %#v, want GLB result", config["result"])
	}
}

func TestModel3DGLBTaskNormalizesMultiGLTFConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":      "addp://engine/26/path/models/scene.gltf?type=item&item_id=77",
			"source_engine_id":  uint(26),
			"item_fingerprint":  "fp-gltf",
			"item_id":           uint(77),
			"format":            "gltf",
			"source_size_bytes": int64(2048),
		},
	}

	cfg, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize model 3d GLB config: %v", err)
	}
	if cfg.Source.Format != "gltf" {
		t.Fatalf("source format = %q, want gltf", cfg.Source.Format)
	}
	if cfg.Result.FileName != "scene.glb" {
		t.Fatalf("result file_name = %q, want scene.glb", cfg.Result.FileName)
	}
	if !strings.Contains(cfg.Result.StorageRef, "tenant_7/model3d-quick-view/fp-gltf/scene.glb") {
		t.Fatalf("storage_ref = %q, want tenant-scoped GLB artifact", cfg.Result.StorageRef)
	}
}

func TestModel3DGLBTaskNormalizesSingleFBXConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/mesh.fbx?type=item&item_id=77",
			"source_engine_id": uint(26),
			"item_fingerprint": "fp-fbx",
			"item_id":          uint(77),
			"format":           "fbx",
		},
	}

	cfg, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize model 3d GLB config: %v", err)
	}
	if cfg.Source.Format != "fbx" {
		t.Fatalf("source format = %q, want fbx", cfg.Source.Format)
	}
	if cfg.Result.FileName != "mesh.glb" {
		t.Fatalf("result file_name = %q, want mesh.glb", cfg.Result.FileName)
	}
}

func TestModel3DGLBTaskNormalizesSingleOBJConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/mesh.obj?type=item&item_id=77",
			"source_engine_id": uint(26),
			"item_fingerprint": "fp-obj",
			"item_id":          uint(77),
			"format":           "obj",
		},
	}

	cfg, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize model 3d GLB config: %v", err)
	}
	if cfg.Source.Format != "obj" {
		t.Fatalf("source format = %q, want obj", cfg.Source.Format)
	}
	if cfg.Result.FileName != "mesh.glb" {
		t.Fatalf("result file_name = %q, want mesh.glb", cfg.Result.FileName)
	}
}

func TestModel3DGLBTaskNormalizesSingleSTLConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/mesh.stl?type=item&item_id=77",
			"source_engine_id": uint(26),
			"item_fingerprint": "fp-stl",
			"item_id":          uint(77),
			"format":           "stl",
		},
	}

	cfg, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize model 3d GLB config: %v", err)
	}
	if cfg.Source.Format != "stl" {
		t.Fatalf("source format = %q, want stl", cfg.Source.Format)
	}
	if cfg.Result.FileName != "mesh.glb" {
		t.Fatalf("result file_name = %q, want mesh.glb", cfg.Result.FileName)
	}
}

func TestModel3DGLBTaskNormalizesSingleIFCConfig(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/building.ifc?type=item&item_id=77",
			"source_engine_id": uint(26),
			"item_fingerprint": "fp-ifc",
			"item_id":          uint(77),
			"format":           "ifc",
		},
	}

	cfg, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
	if err != nil {
		t.Fatalf("normalize model 3d GLB config: %v", err)
	}
	if cfg.Source.Format != "ifc" {
		t.Fatalf("source format = %q, want ifc", cfg.Source.Format)
	}
	if cfg.Result.FileName != "building.glb" {
		t.Fatalf("result file_name = %q, want building.glb", cfg.Result.FileName)
	}
}

func TestModel3DGLBTaskRejectsUnsupportedSource(t *testing.T) {
	config := commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":     "addp://engine/26/path/models/tile.glb?type=item&item_id=77",
			"source_engine_id": uint(26),
			"item_fingerprint": "fp-glb",
			"format":           "glb",
		},
	}

	_, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
	if err == nil {
		t.Fatal("normalize model 3d GLB config error is nil, want unsupported source rejection")
	}
	if got := err.Error(); got != "model 3d GLB config.source.format must be osgb, gltf, fbx, obj, stl or ifc" {
		t.Fatalf("error = %q, want source format rejection", got)
	}
}

func TestModel3DGLBTaskCreateReusesExistingFingerprintTask(t *testing.T) {
	db := newModel3DGLBTaskServiceTestDB(t)
	taskSvc := NewModel3DGLBTaskService(repository.NewModel3DGLBRepository(db), nil)

	first := &models.Model3DGLBTask{
		TenantID: 7,
		Name:     "生成 GLB 快显",
		Enabled:  true,
		Config:   model3DGLBTaskTestConfig("fp-osgb", "tile.osgb"),
	}
	if err := taskSvc.Create(context.Background(), first); err != nil {
		t.Fatalf("create first model 3d GLB task: %v", err)
	}
	if first.ID == 0 {
		t.Fatal("first task ID is zero")
	}

	second := &models.Model3DGLBTask{
		TenantID:    7,
		Name:        "重新生成 GLB 快显",
		Description: "same source item",
		Enabled:     true,
		Config:      model3DGLBTaskTestConfig("fp-osgb", "tile.osgb"),
	}
	if err := taskSvc.Create(context.Background(), second); err != nil {
		t.Fatalf("create duplicate model 3d GLB task: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second task ID = %d, want reused ID %d", second.ID, first.ID)
	}

	tasks, total, err := taskSvc.List(context.Background(), 7, 1, 20)
	if err != nil {
		t.Fatalf("list model 3d GLB tasks: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("tasks total=%d len=%d, want one active task", total, len(tasks))
	}
	if tasks[0].Name != "重新生成 GLB 快显" || tasks[0].Description != "same source item" {
		t.Fatalf("task was not updated from duplicate create: %#v", tasks[0])
	}
}

func model3DGLBTaskTestConfig(fingerprint string, fileName string) commonModels.JSONMap {
	return commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"item_locator":      "addp://engine/26/path/models/" + fileName + "?type=item&item_id=77",
			"source_engine_id":  uint(26),
			"item_fingerprint":  fingerprint,
			"item_id":           uint(77),
			"source_size_bytes": int64(1024),
		},
	}
}

func newModel3DGLBTaskServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model_3d_glb_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN NOT NULL,
		schedule TEXT,
		next_run_at DATETIME,
		last_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT,
		config JSON NOT NULL,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model_3d_glb_tasks table: %v", err)
	}
	return db
}
