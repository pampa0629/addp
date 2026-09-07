package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerTileCacheConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.TileCacheTask{}, &models.TileCache{}); err != nil {
		t.Fatalf("migrate manager tile cache tables: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 930000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TileCache{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TileCacheTask{}).Error
	})
	task := models.TileCacheTask{
		TenantID: tenantID, Name: "manager-tile-cache-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-pg-%d", tenantID)},
			"tile":   commonModels.JSONMap{"format": "mvt"},
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	repo := NewTileCacheRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-tile-pg-%d-a", tenantID),
		fmt.Sprintf("manager-tile-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, tenantID,
				newTileCacheRepositoryTestExecution(executionID, int(tenantID), createdAt),
				false,
			)
			results <- claimErr
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		claimErr := <-results
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeVectorTileCacheGeneration, fmt.Sprint(task.ID),
	).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(managerExecutionLeaseContextForTest(t, db, executions[0].ExecutionID, int(tenantID), startedAt), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.TileCacheTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID == nil || *storedTask.LastExecutionID != executions[0].ExecutionID ||
		storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %#v/%#v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
}

func TestIntegrationPostgresManagerVectorMaterializedViewConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.VectorMaterializedViewTask{}, &models.VectorMaterializedView{}); err != nil {
		t.Fatalf("migrate manager vector materialized view tables: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 940000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.VectorMaterializedView{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.VectorMaterializedViewTask{}).Error
	})
	task := models.VectorMaterializedViewTask{
		TenantID: tenantID, Name: "manager-vmv-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-vmv-pg-%d", tenantID)},
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create vector materialized view task: %v", err)
	}

	repo := NewVectorMaterializedViewRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-vmv-pg-%d-a", tenantID),
		fmt.Sprintf("manager-vmv-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, tenantID,
				newManagerRepositoryTestExecution(
					executionID, int(tenantID), commonExecution.TaskTypeVectorMaterializedViewGeneration, createdAt,
				),
				false,
			)
			results <- claimErr
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		claimErr := <-results
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeVectorMaterializedViewGeneration, fmt.Sprint(task.ID),
	).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(managerExecutionLeaseContextForTest(t, db, executions[0].ExecutionID, int(tenantID), startedAt), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.VectorMaterializedViewTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID == nil || *storedTask.LastExecutionID != executions[0].ExecutionID ||
		storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %#v/%#v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
}

func TestIntegrationPostgresManagerRasterCOGConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.RasterCOGTask{}, &models.RasterCOG{}); err != nil {
		t.Fatalf("migrate manager raster COG tables: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 950000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.RasterCOG{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.RasterCOGTask{}).Error
	})
	task := models.RasterCOGTask{
		TenantID: tenantID, Name: "manager-raster-cog-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-cog-pg-%d", tenantID)},
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create raster COG task: %v", err)
	}

	repo := NewRasterCOGRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-cog-pg-%d-a", tenantID),
		fmt.Sprintf("manager-cog-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, tenantID,
				newManagerRepositoryTestExecution(
					executionID, int(tenantID), commonExecution.TaskTypeRasterCOGGeneration, createdAt,
				),
				false,
			)
			results <- claimErr
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		claimErr := <-results
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeRasterCOGGeneration, fmt.Sprint(task.ID),
	).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(managerExecutionLeaseContextForTest(t, db, executions[0].ExecutionID, int(tenantID), startedAt), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.RasterCOGTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID == nil || *storedTask.LastExecutionID != executions[0].ExecutionID ||
		storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %#v/%#v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
}

func TestIntegrationPostgresManagerRasterMosaicConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.RasterMosaicTask{}); err != nil {
		t.Fatalf("migrate manager raster mosaic table: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 960000000)
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.RasterMosaicTask{}).Error
	})
	task := models.RasterMosaicTask{
		TenantID: tenantID, Name: "manager-raster-mosaic-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{"node_locator": fmt.Sprintf("addp://engine/1/path/mosaics/%d?type=node", tenantID)},
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create raster mosaic task: %v", err)
	}

	repo := NewRasterMosaicRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-mosaic-pg-%d-a", tenantID),
		fmt.Sprintf("manager-mosaic-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, tenantID,
				newManagerRepositoryTestExecution(
					executionID, int(tenantID), commonExecution.TaskTypeRasterMosaicGeneration, createdAt,
				),
			)
			results <- claimErr
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		claimErr := <-results
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeRasterMosaicGeneration, fmt.Sprint(task.ID),
	).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(managerExecutionLeaseContextForTest(t, db, executions[0].ExecutionID, int(tenantID), startedAt), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.RasterMosaicTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID == nil || *storedTask.LastExecutionID != executions[0].ExecutionID ||
		storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %#v/%#v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
}

func TestIntegrationPostgresManagerModel3DGLBConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.Model3DGLBTask{}, &models.Model3DGLB{}); err != nil {
		t.Fatalf("migrate manager model 3d GLB tables: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 970000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.Model3DGLB{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.Model3DGLBTask{}).Error
	})
	task := models.Model3DGLBTask{
		TenantID: tenantID, Name: "manager-model-3d-glb-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-glb-pg-%d", tenantID)},
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create model 3d GLB task: %v", err)
	}

	repo := NewModel3DGLBRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-glb-pg-%d-a", tenantID),
		fmt.Sprintf("manager-glb-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, tenantID,
				newManagerRepositoryTestExecution(
					executionID, int(tenantID), commonExecution.TaskTypeModel3DGLBGeneration, createdAt,
				),
				false,
			)
			results <- claimErr
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		claimErr := <-results
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeModel3DGLBGeneration, fmt.Sprint(task.ID),
	).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(managerExecutionLeaseContextForTest(t, db, executions[0].ExecutionID, int(tenantID), startedAt), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.Model3DGLBTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID == nil || *storedTask.LastExecutionID != executions[0].ExecutionID ||
		storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %#v/%#v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
}

func TestIntegrationPostgresManagerGaussianSplatKSplatConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.GaussianSplatKSplatTask{}, &models.GaussianSplatKSplat{}); err != nil {
		t.Fatalf("migrate manager gaussian splat KSplat tables: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 980000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.GaussianSplatKSplat{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.GaussianSplatKSplatTask{}).Error
	})
	task := models.GaussianSplatKSplatTask{
		TenantID: tenantID, Name: "manager-gaussian-splat-ksplat-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-ksplat-pg-%d", tenantID)},
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create gaussian splat KSplat task: %v", err)
	}

	repo := NewGaussianSplatKSplatRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-ksplat-pg-%d-a", tenantID),
		fmt.Sprintf("manager-ksplat-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, tenantID,
				newManagerRepositoryTestExecution(
					executionID, int(tenantID), commonExecution.TaskTypeGaussianSplatKSplatGeneration, createdAt,
				),
				false,
			)
			results <- claimErr
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		claimErr := <-results
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeGaussianSplatKSplatGeneration, fmt.Sprint(task.ID),
	).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(managerExecutionLeaseContextForTest(t, db, executions[0].ExecutionID, int(tenantID), startedAt), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.GaussianSplatKSplatTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.LastExecutionID == nil || *storedTask.LastExecutionID != executions[0].ExecutionID ||
		storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning {
		t.Fatalf("started task summary = %#v/%#v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}
}

func TestIntegrationPostgresManagerPointCloudCOPCConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.PointCloudCOPCTask{}, &models.PointCloudCOPC{}); err != nil {
		t.Fatalf("migrate manager point cloud COPC tables: %v", err)
	}
	tenantID := uint(time.Now().UnixNano()%100000000 + 990000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.PointCloudCOPC{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.PointCloudCOPCTask{}).Error
	})
	task := models.PointCloudCOPCTask{
		TenantID: tenantID, Name: "manager-point-cloud-copc-integration", Enabled: true,
		Config: commonModels.JSONMap{"source": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-copc-pg-%d", tenantID)}},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create point cloud COPC task: %v", err)
	}
	repo := NewPointCloudCOPCRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("manager-copc-pg-%d-a", tenantID), fmt.Sprintf("manager-copc-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(context.Background(), task.ID, tenantID,
				newManagerRepositoryTestExecution(executionID, int(tenantID), commonExecution.TaskTypePointCloudCOPCGeneration, createdAt), false)
			results <- claimErr
		}()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		switch claimErr := <-results; {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}
	var executions []commonExecution.TaskExecution
	if err := db.Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypePointCloudCOPCGeneration, fmt.Sprint(task.ID)).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	leaseCtx := managerExecutionLeaseContextForTest(t, db, executions[0].ExecutionID, int(tenantID), startedAt)
	if err := repo.StartExecution(leaseCtx, task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	if err := repo.UpdateRunningExecutionProgress(leaseCtx, tenantID, executions[0].ExecutionID, map[string]interface{}{"progress": 30}); err != nil {
		t.Fatalf("update running progress: %v", err)
	}
}

func TestIntegrationPostgresManagerModel3DTilesConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.Model3DTilesTask{}, &models.Model3DTiles{}); err != nil {
		t.Fatalf("migrate manager model3d tiles tables: %v", err)
	}
	tenantID := uint(time.Now().UnixNano()%100000000 + 900000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.Model3DTiles{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.Model3DTilesTask{}).Error
	})
	fingerprint := fmt.Sprintf("manager-model3d-tiles-pg-%d", tenantID)
	task := models.Model3DTilesTask{
		TenantID: tenantID, Name: "manager-model3d-tiles-integration", Enabled: true,
		Config: commonModels.JSONMap{
			"source":        commonModels.JSONMap{"item_fingerprint": fingerprint},
			"target_format": models.Model3DTilesTargetFormat3DTiles,
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create model3d tiles task: %v", err)
	}
	repo := NewModel3DTilesRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{fmt.Sprintf("manager-model3d-tiles-pg-%d-a", tenantID), fmt.Sprintf("manager-model3d-tiles-pg-%d-b", tenantID)} {
		executionID := executionID
		go func() {
			<-start
			_, claimErr := repo.ClaimExecution(context.Background(), task.ID, tenantID,
				newManagerRepositoryTestExecution(executionID, int(tenantID), commonExecution.TaskTypeModel3DTilesGeneration, createdAt), false)
			results <- claimErr
		}()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		switch claimErr := <-results; {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}
	var executions []commonExecution.TaskExecution
	if err := db.Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?",
		int(tenantID), commonExecution.ModuleManager, commonExecution.TaskTypeModel3DTilesGeneration, fmt.Sprint(task.ID)).Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	if err := repo.StartExecution(managerExecutionLeaseContextForTest(t, db, executions[0].ExecutionID, int(tenantID), createdAt.Add(time.Second)), task.ID, tenantID, executions[0].ExecutionID, createdAt.Add(time.Second)); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
}

func TestIntegrationPostgresManagerManagedTaskSemanticIdentityIndexes(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := ensureTileCacheStateSchema(db); err != nil {
		t.Fatalf("ensure tile cache schema: %v", err)
	}
	if err := ensureRasterCOGSchema(db); err != nil {
		t.Fatalf("ensure raster COG schema: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 930000000)
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TileCacheTask{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.RasterCOGTask{}).Error
	})
	fingerprint := fmt.Sprintf("manager-semantic-%d", tenantID)
	tileConfig := commonModels.JSONMap{
		"target":       commonModels.JSONMap{"item_fingerprint": fingerprint},
		"profile_hash": "profile-a",
	}
	if err := db.Create(&models.TileCacheTask{TenantID: tenantID, Name: "tile-a", Enabled: true, Config: tileConfig}).Error; err != nil {
		t.Fatalf("create first tile cache task: %v", err)
	}
	if err := db.Create(&models.TileCacheTask{TenantID: tenantID, Name: "tile-b", Enabled: true, Config: tileConfig}).Error; err == nil || !strings.Contains(err.Error(), "idx_vector_tile_cache_tasks_source_profile_unique") {
		t.Fatalf("duplicate tile cache task error = %v, want semantic identity index", err)
	}

	rasterConfig := commonModels.JSONMap{
		"target": commonModels.JSONMap{"item_fingerprint": fingerprint},
	}
	if err := db.Create(&models.RasterCOGTask{TenantID: tenantID, Name: "cog-a", Enabled: true, Config: rasterConfig}).Error; err != nil {
		t.Fatalf("create first raster COG task: %v", err)
	}
	if err := db.Create(&models.RasterCOGTask{TenantID: tenantID, Name: "cog-b", Enabled: true, Config: rasterConfig}).Error; err == nil || !strings.Contains(err.Error(), "idx_raster_cog_tasks_source_unique") {
		t.Fatalf("duplicate raster COG task error = %v, want semantic identity index", err)
	}
}

func TestIntegrationPostgresManagerDisablesTileCacheOwnerSchedule(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatalf("create manager schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := ensureTileCacheStateSchema(db); err != nil {
		t.Fatalf("ensure initial tile cache schema: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 940000000)
	nextRunAt := time.Now().Add(time.Hour)
	task := &models.TileCacheTask{
		TenantID:  tenantID,
		Name:      "legacy scheduled tile cache",
		Enabled:   true,
		Schedule:  "0 * * * *",
		NextRunAt: &nextRunAt,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{"item_fingerprint": fmt.Sprintf("manager-schedule-%d", tenantID)},
			"tile":   commonModels.JSONMap{"format": "mvt"},
		},
	}
	t.Cleanup(func() {
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TileCacheTask{}).Error
	})
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create historical scheduled tile cache task: %v", err)
	}
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_vector_tile_cache_tasks_schedule
		ON manager.vector_tile_cache_tasks (enabled, next_run_at)
	`).Error; err != nil {
		t.Fatalf("create historical schedule index: %v", err)
	}

	if err := ensureTileCacheStateSchema(db); err != nil {
		t.Fatalf("normalize tile cache schedule state: %v", err)
	}
	var refreshed models.TileCacheTask
	if err := db.Unscoped().First(&refreshed, task.ID).Error; err != nil {
		t.Fatalf("reload normalized tile cache task: %v", err)
	}
	if refreshed.Schedule != "" || refreshed.NextRunAt != nil {
		t.Fatalf("normalized schedule=%q next_run_at=%v, want empty and nil", refreshed.Schedule, refreshed.NextRunAt)
	}
	var scheduleIndexCount int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM pg_indexes
		WHERE schemaname = 'manager' AND indexname = 'idx_vector_tile_cache_tasks_schedule'
	`).Scan(&scheduleIndexCount).Error; err != nil {
		t.Fatalf("count tile cache schedule index: %v", err)
	}
	if scheduleIndexCount != 0 {
		t.Fatalf("tile cache schedule index count = %d, want 0", scheduleIndexCount)
	}
}

func managerTileCacheRepositoryIntegrationDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		managerTileCacheRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
}

func managerTileCacheRepositoryIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
