package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModel3DGLBExecuteRequiresVerifiedUserOrigin(t *testing.T) {
	for _, name := range []string{"missing", "other tenant", "service", "incomplete", "spoofed source", "scheduled", "valid"} {
		t.Run(name, func(t *testing.T) {
			db := newModel3DGLBTaskServiceTestDB(t)
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE TABLE manager.model_3d_glb (id INTEGER PRIMARY KEY, tenant_id INTEGER, task_id INTEGER, status TEXT, deleted_at DATETIME)").Error; err != nil {
				t.Fatal(err)
			}
			repo := repository.NewModel3DGLBRepository(db)
			creator := uint(99)
			task := &models.Model3DGLBTask{TenantID: 7, Name: name, Enabled: true, CreatedBy: &creator, Config: model3DGLBTaskTestConfig(uuid.NewString(), "building.ifc")}
			if err := repo.CreateTask(context.Background(), task); err != nil {
				t.Fatal(err)
			}
			facts := authtest.NewTenantUserAuthContext("7", "31", nil)
			ctx := context.Background()
			source := commonExecution.ModuleManager
			trigger := commonExecution.TriggerTypeManual
			switch name {
			case "other tenant":
				facts = authtest.NewTenantUserAuthContext("8", "31", nil)
			case "service":
				facts.Principal.Type = "service_principal"
			case "incomplete":
				facts.Context.TenantMembershipID = nil
			case "spoofed source":
				source = commonExecution.ModuleOrchestrator
			case "scheduled":
				trigger = commonExecution.TriggerTypeScheduled
			}
			if name != "missing" {
				ctx = commonExecution.WithUserOrigin(ctx, facts)
			}
			svc := NewModel3DGLBTaskService(repo)
			tenant := uint(7)
			svc.SetSourceEngineClient(&glbSourceEngineFixture{engine: &commonModels.Engine{ID: 26, TenantID: &tenant, EngineType: "nfs", LifecycleState: "active", ConnectionStatus: "online"}})
			id, err := svc.Execute(ctx, task.ID, 7, trigger, source, nil, false, "")
			if name != "valid" {
				if !errors.Is(err, commonAPI.ErrForbidden) || id != "" {
					t.Fatalf("id=%q err=%v", id, err)
				}
				var count int64
				if err := db.Model(&commonExecution.TaskExecution{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("executions=%d err=%v", count, err)
				}
				stored, err := repo.GetTask(context.Background(), task.ID, 7)
				if err != nil || stored.LastExecutionID != nil || stored.LastExecutionStatus != nil {
					t.Fatalf("rejected origin changed task: %#v %v", stored, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			row, err := commonExecution.NewTaskExecutionRepository(db).GetByExecutionID(ctx, id, 7)
			if err != nil || row.TriggeredBy == nil || *row.TriggeredBy != 31 || row.ActorPrincipalID == nil || *row.ActorPrincipalID != 31 || row.ActorTenantMembershipID == nil || row.IssuedAuthorizationVersion == nil {
				t.Fatalf("origin=%#v err=%v", row, err)
			}
			if row.ExecutionAuthorizationID != nil {
				t.Fatal("origin was treated as source authorization")
			}
		})
	}
}

func TestModel3DGLBExecuteRejectsOrchestratorEvenWithVerifiedParent(t *testing.T) {
	for _, name := range []string{"valid", "missing", "other tenant", "stopped", "incomplete", "wrong source"} {
		t.Run(name, func(t *testing.T) {
			db := newModel3DGLBTaskServiceTestDB(t)
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE TABLE manager.model_3d_glb (id INTEGER PRIMARY KEY, tenant_id INTEGER, task_id INTEGER, status TEXT, deleted_at DATETIME)").Error; err != nil {
				t.Fatal(err)
			}
			repo := repository.NewModel3DGLBRepository(db)
			task := &models.Model3DGLBTask{TenantID: 7, Name: name, Enabled: true, Config: model3DGLBTaskTestConfig(uuid.NewString(), "building.ifc")}
			if err := repo.CreateTask(context.Background(), task); err != nil {
				t.Fatal(err)
			}
			id := uuid.NewString()
			parent := &commonExecution.TaskExecution{ExecutionID: id, TenantID: 7, Module: commonExecution.ModuleOrchestrator, TaskType: commonExecution.TaskTypeOrchestration, Source: commonExecution.ModuleOrchestrator, Status: commonExecution.ExecutionStatusRunning, TriggerType: "manual", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			ctx := commonExecution.WithUserOrigin(context.Background(), authtest.NewTenantUserAuthContext("7", "31", nil))
			if err := commonExecution.ApplyUserOrigin(ctx, parent); err != nil {
				t.Fatal(err)
			}
			source := commonExecution.ModuleOrchestrator
			switch name {
			case "other tenant":
				parent.TenantID = 8
			case "stopped":
				parent.Status = commonExecution.ExecutionStatusSuccess
			case "incomplete":
				parent.ActorTenantMembershipID = nil
			case "wrong source":
				source = commonExecution.ModuleManager
			}
			if name != "missing" {
				if err := db.Create(parent).Error; err != nil {
					t.Fatal(err)
				}
			}
			childID, err := NewModel3DGLBTaskService(repo).Execute(context.Background(), task.ID, 7, "manual", source, &id, false, "")
			var count int64
			if e := db.Model(&commonExecution.TaskExecution{}).Where("module = ?", commonExecution.ModuleManager).Count(&count).Error; e != nil {
				t.Fatal(e)
			}
			if !errors.Is(err, commonAPI.ErrForbidden) || childID != "" || count != 0 {
				t.Fatalf("id=%q count=%d err=%v", childID, count, err)
			}
			stored, err := repo.GetTask(context.Background(), task.ID, 7)
			if err != nil || stored.LastExecutionID != nil || stored.LastExecutionStatus != nil {
				t.Fatalf("rejected source changed task: %#v %v", stored, err)
			}
		})
	}
}

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

func TestMAXTaskPreservesUserUnitAndAllowsOmittedDefault(t *testing.T) {
	for _, value := range []interface{}{nil, "", "mm", "cm", "m", "km", "in", "ft", "mi", "meters", true} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			options := commonModels.JSONMap{}
			if value != nil {
				options["source_unit"] = value
			}
			config := commonModels.JSONMap{
				"source":  commonModels.JSONMap{"item_locator": "addp://engine/26/path/model.max?type=file&item_id=77", "source_engine_id": uint(26), "item_fingerprint": "max-source", "format": "max"},
				"options": options,
			}
			cfg, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
			if value == "meters" || value == true {
				if err == nil {
					t.Fatal("invalid MAX unit was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Source.Format != "max" || cfg.Options["source_unit"] != value {
				t.Fatalf("MAX options changed: %#v", cfg)
			}
			operator, formatName, err := model3DGLBOperatorForFormat("MAX")
			if err != nil || operator != "max_to_glb" || formatName != "max" || !model3DGLBUsesDirectorySource("max") {
				t.Fatal("MAX runtime access route is not registered")
			}
			if !isModel3DQuickViewSourceFormat("max", "single") {
				t.Fatal("MAX single model cannot generate quick view")
			}
		})
	}
}

func TestMAXTaskRejectsInvalidTextureDeclarationsBeforeExecution(t *testing.T) {
	for _, value := range []interface{}{
		commonModels.JSONMap{"C:\\textures\\brick.png": "textures/brick.png"},
		commonModels.JSONMap{},
		nil, []string{"brick.png"},
		commonModels.JSONMap{"": "brick.png"},
		commonModels.JSONMap{"brick": ""},
		commonModels.JSONMap{"brick": 1},
		commonModels.JSONMap{"brick": "/brick.png"},
		commonModels.JSONMap{"brick": "../brick.png"},
		commonModels.JSONMap{"brick": "sub/../brick.png"},
		commonModels.JSONMap{"brick": "sub\\brick.png"},
		commonModels.JSONMap{"brick": "https://example.org/brick.png"},
		commonModels.JSONMap{"brick": "\x00.png"},
		commonModels.JSONMap{"brick": "."},
	} {
		config := commonModels.JSONMap{
			"source":  commonModels.JSONMap{"item_locator": "addp://engine/26/path/model.max?type=file&item_id=77", "source_engine_id": uint(26), "item_fingerprint": "max-source", "format": "max"},
			"options": commonModels.JSONMap{"texture_files": value},
		}
		cfg, err := normalizeModel3DGLBTaskConfig(config, "manager", 7)
		textures, _ := asJSONMap(value)
		valid := textures != nil && (len(textures) == 0 || textures["C:\\textures\\brick.png"] == "textures/brick.png")
		if valid && (err != nil || cfg.Options["texture_files"] == nil) {
			t.Fatalf("valid mapping lost: %#v: %v", value, err)
		}
		if !valid && err == nil {
			t.Fatalf("invalid mapping accepted: %#v", value)
		}
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
	if got := err.Error(); got != "model 3d GLB config.source.format is not supported" {
		t.Fatalf("error = %q, want source format rejection", got)
	}
}

func TestModel3DGLBTaskCreateReusesExistingFingerprintTask(t *testing.T) {
	db := newModel3DGLBTaskServiceTestDB(t)
	taskSvc := NewModel3DGLBTaskService(repository.NewModel3DGLBRepository(db))

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
	ensureDerivedTaskDefinitionTestTables(t, db)
	return db
}
