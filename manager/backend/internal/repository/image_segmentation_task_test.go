package repository

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestImageSegmentationFreezesIndependentExecutionTargets(t *testing.T) {
	exerciseSegmentationExecutionTargets(t, newTileCacheExecutionRepositoryTestDB(t))
}

func TestIntegrationPostgresManagerSegmentationExecutionTargets(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := ensureTaskDefinitionSchema(db); err != nil {
		t.Fatal(err)
	}
	exerciseSegmentationExecutionTargets(t, db)
}

func exerciseSegmentationExecutionTargets(t *testing.T, db *gorm.DB) {
	t.Helper()
	tenant := uint(time.Now().UnixNano()%100000000 + 970000000)
	target := resourcetree.ResourceLocator{EngineID: 12, Path: []string{"artifacts", "nested", "buildings.geojson"}, Type: resourcetree.TypeObject}
	sourcePath := plugin.ObjectItemPath(12, "images", "source.tif")
	composition, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", "single", []authorizationfacts.FileCompositionRef{
		{Target: sourcePath, Role: "image", Primary: true, Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := execution.ImageSegmentationConfig{Text: "building", ModelProfileID: "profile",
		Source: execution.ImageSegmentationSource{EngineID: 12, EngineVersion: 3, ItemID: 13, ItemFingerprint: "synthetic-source", Locator: "addp://engine/12/path/images/source.tif?type=object", Composition: *composition},
		Target: execution.ImageSegmentationTarget{EngineID: 12, EngineVersion: 3, Locator: target.ToURI(), Path: plugin.ObjectItemPath(12, "artifacts", "nested/buildings.geojson")}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var original commonmodels.JSONMap
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	task := models.TaskDefinition{TenantID: tenant, TaskType: execution.TaskTypeImageSegmentation, Name: "Synthetic segmentation", Config: original}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, model := range []any{&execution.TaskExecution{}, &models.TaskDefinition{}} {
			if err := db.Unscoped().Where("tenant_id = ?", tenant).Delete(model).Error; err != nil {
				t.Error(err)
			}
		}
	})
	repo := NewImageSegmentationTaskRepository(db)
	locators := map[string]bool{}
	for range 2 {
		run := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: execution.ModuleManager,
			TaskType: execution.TaskTypeImageSegmentation, Source: execution.ModuleManager, TriggerType: execution.TriggerTypeManual,
			Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded}
		if err := repo.Enqueue(t.Context(), task.ID, tenant, run); err != nil {
			t.Fatal(err)
		}
		var stored execution.TaskExecution
		if err := db.Where("execution_id = ?", run.ExecutionID).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(stored.ExecutionConfig)
		var frozen execution.ImageSegmentationConfig
		if err := json.Unmarshal(payload, &frozen); err != nil {
			t.Fatal(err)
		}
		expected := target
		expected.Path = []string{"artifacts", "nested", fmt.Sprintf("buildings-%s.geojson", run.ExecutionID)}
		actualPath, err := plugin.RequireObjectLeafPath(frozen.Target.Path)
		if err != nil || frozen.Target.Locator != expected.ToURI() || actualPath != "artifacts/nested/"+expected.Path[2] || frozen.Target.EngineVersion != config.Target.EngineVersion {
			t.Fatalf("execution target not frozen to its own object: %+v: %v", frozen.Target, err)
		}
		locators[frozen.Target.Locator] = true
		if err := db.Model(&stored).Update("status", execution.ExecutionStatusSuccess).Error; err != nil {
			t.Fatal(err)
		}
	}
	if len(locators) != 2 {
		t.Fatal("repeated runs reused the same target")
	}
	var saved models.TaskDefinition
	if err := db.First(&saved, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Config, original) {
		t.Fatal("running the task rewrote the saved source, target or model configuration")
	}
}
