package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerProfilePersistsActualReadSet(t *testing.T) {
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
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureDataProfileSchema(db); err != nil {
		t.Fatal(err)
	}
	tenantID := uint(time.Now().UnixNano()%100000000 + 970000000)
	state := &models.DataProfile{TenantID: tenantID, EngineID: 11, ItemFingerprint: uuid.NewString(),
		ProfileConfigHash: uuid.NewString(), LastExecutionID: uuid.NewString(), Locator: "addp://test-profile"}
	t.Cleanup(func() {
		if err := db.Transaction(func(tx *gorm.DB) error {
			return NewDataProfileRepository(db).DeleteByItemFingerprints(context.Background(), tx, int64(tenantID), []string{state.ItemFingerprint})
		}); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		var remaining int64
		if err := db.Model(&models.DataProfile{}).Where("tenant_id = ?", tenantID).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("profile residue=%d %v", remaining, err)
		}
	})
	readSet, err := plugin.NewQueryReadSet(plugin.TabularItemPath(11, "schema", "public", "C"), plugin.TabularItemPath(11, "schema", "public", "D"))
	if err != nil {
		t.Fatal(err)
	}
	state.DependencySnapshot, err = json.Marshal(map[string]interface{}{"read_set": readSet})
	if err != nil {
		t.Fatal(err)
	}
	expected := readSet.Clone()
	repo := NewDataProfileRepository(db)
	profile := dataprofile.Profile{SchemaVersion: dataprofile.SchemaVersionV2, Mode: dataprofile.ModeSample,
		DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, ProfiledAt: time.Now().UTC()}
	if err := db.Transaction(func(tx *gorm.DB) error { return repo.ReplaceCurrent(context.Background(), tx, state, profile) }); err != nil {
		t.Fatal(err)
	}
	readSet.Paths[0] = plugin.TabularItemPath(11, "schema", "public", "today_changed_source")
	stored, result, err := repo.GetCurrent(context.Background(), tenantID, state.ItemFingerprint, dataprofile.ModeSample, state.ProfileConfigHash)
	if err != nil || stored == nil || result == nil || stored.LastExecutionID != state.LastExecutionID {
		t.Fatalf("actual source proof lost: %#v %v", stored, err)
	}
	var snapshot struct {
		ReadSet *plugin.QueryReadSet `json:"read_set"`
	}
	if err := json.Unmarshal(stored.DependencySnapshot, &snapshot); err != nil || !reflect.DeepEqual(snapshot.ReadSet, expected) {
		t.Fatalf("stored sources changed: %+v %v", snapshot, err)
	}
}

func TestIntegrationPostgresManagerProfileActorPersistenceAndReuse(t *testing.T) {
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
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	tenantID := int(time.Now().UnixNano()%100000000 + 970000000)
	t.Cleanup(func() {
		if err := db.Where("tenant_id = ? AND module = ? AND task_type = ?", tenantID, commonExecution.ModuleManager, commonExecution.TaskTypeDataProfiling).Delete(&commonExecution.TaskExecution{}).Error; err != nil {
			t.Errorf("cleanup profiling executions: %v", err)
		}
		var remaining int64
		if err := db.Model(&commonExecution.TaskExecution{}).Where("tenant_id = ?", tenantID).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("profiling fixture residue=%d err=%v", remaining, err)
		}
	})
	repo := NewDataProfileExecutionRepository(db)
	principalID, membershipID, version := int64(9), int64(12), int64(3)
	makeExecution := func(key string) *commonExecution.TaskExecution {
		return &commonExecution.TaskExecution{
			TenantID: tenantID, ExecutionID: uuid.NewString(), Module: commonExecution.ModuleManager,
			Source: commonExecution.ModuleManager, TaskType: commonExecution.TaskTypeDataProfiling,
			Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
			ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID, IssuedAuthorizationVersion: &version,
			ExecutionConfig: commonModels.JSONMap{"target_key": key, "engine_id": 11,
				"budget": map[string]interface{}{"sample_size": 10, "max_rows_scanned": 20, "page_size": 5, "timeout_ms": 30000}},
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
	}
	key := uuid.NewString()
	var results [2]*commonExecution.TaskExecution
	var created [2]bool
	var failures [2]error
	var workers sync.WaitGroup
	for i := range results {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			results[i], created[i], failures[i] = repo.CreateOrReuseActive(context.Background(), key, makeExecution(key))
		}(i)
	}
	workers.Wait()
	if failures[0] != nil || failures[1] != nil || results[0] == nil || results[1] == nil {
		t.Fatalf("concurrent enqueue: errors=%v results=%#v", failures, results)
	}
	if created[0] == created[1] || results[0].ExecutionID != results[1].ExecutionID {
		t.Fatal("identical scoped target did not reuse exactly one execution")
	}
	stored, err := repo.GetByExecutionID(context.Background(), tenantID, results[0].ExecutionID)
	if err != nil || stored == nil || stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != principalID ||
		stored.ActorTenantMembershipID == nil || *stored.ActorTenantMembershipID != membershipID ||
		stored.IssuedAuthorizationVersion == nil || *stored.IssuedAuthorizationVersion != version {
		t.Fatalf("persisted actor=%#v err=%v", stored, err)
	}
	budgetJSON, err := json.Marshal(stored.ExecutionConfig["budget"])
	if err != nil || string(budgetJSON) != `{"max_rows_scanned":20,"page_size":5,"sample_size":10,"timeout_ms":30000}` {
		t.Fatalf("persisted frozen budget=%s err=%v", budgetJSON, err)
	}
	engineJSON, err := json.Marshal(stored.ExecutionConfig["engine_id"])
	if err != nil || string(engineJSON) != "11" {
		t.Fatalf("persisted frozen engine=%s err=%v", engineJSON, err)
	}
	otherKey := uuid.NewString()
	other := makeExecution(otherKey)
	otherPrincipal := int64(10)
	other.ActorPrincipalID = &otherPrincipal
	separate, didCreate, err := repo.CreateOrReuseActive(context.Background(), otherKey, other)
	if err != nil || !didCreate || separate.ExecutionID == stored.ExecutionID {
		t.Fatalf("different actor target reused: created=%v err=%v", didCreate, err)
	}
}

func TestIntegrationPostgresManagerUnifiedTaskDefinitionLifecycle(t *testing.T) {
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

	tenantID := uint(time.Now().UnixNano()%100000000 + 960000000)
	legacyExecutionID := fmt.Sprintf("manager-legacy-model3d-tiles-%d", tenantID)
	derivedExecutionID := fmt.Sprintf("manager-derived-vector-tile-cache-%d", tenantID)
	t.Cleanup(func() {
		_ = db.Exec(`DROP TABLE IF EXISTS manager.model_3d_tiles_tasks`).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.TaskResourceBinding{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TaskDefinition{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
	})

	if err := db.Exec(`DROP TABLE IF EXISTS manager.model_3d_tiles_tasks`).Error; err != nil {
		t.Fatalf("drop stale legacy table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model_3d_tiles_tasks (id BIGSERIAL PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create legacy task table: %v", err)
	}
	legacyExecution := &commonExecution.TaskExecution{
		TenantID:          int(tenantID),
		ExecutionID:       legacyExecutionID,
		Module:            commonExecution.ModuleManager,
		TaskType:          "model_3d_tiles_generation",
		Source:            commonExecution.ModuleManager,
		Status:            commonExecution.ExecutionStatusSuccess,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType:       commonExecution.TriggerTypeManual,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := db.Create(legacyExecution).Error; err != nil {
		t.Fatalf("create legacy execution: %v", err)
	}
	derivedExecution := *legacyExecution
	derivedExecution.ID = 0
	derivedExecution.ExecutionID = derivedExecutionID
	derivedExecution.TaskType = commonExecution.TaskTypeVectorTileCacheGeneration
	if err := db.Create(&derivedExecution).Error; err != nil {
		t.Fatalf("create derived execution tied to legacy task schema: %v", err)
	}

	if err := dropLegacyQuickViewTables(db); err != nil {
		t.Fatalf("drop legacy quick view state: %v", err)
	}
	if err := ensureTaskDefinitionSchema(db); err != nil {
		t.Fatalf("ensure unified task definition schema: %v", err)
	}
	var legacyTableExists bool
	if err := db.Raw(`SELECT to_regclass('manager.model_3d_tiles_tasks') IS NOT NULL`).Scan(&legacyTableExists).Error; err != nil {
		t.Fatalf("check legacy task table: %v", err)
	}
	if legacyTableExists {
		t.Fatal("legacy model_3d_tiles_tasks table still exists")
	}
	var obsoleteExecutionCount int64
	if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id IN ?", []string{legacyExecutionID, derivedExecutionID}).Count(&obsoleteExecutionCount).Error; err != nil {
		t.Fatalf("count obsolete executions: %v", err)
	}
	if obsoleteExecutionCount != 0 {
		t.Fatalf("obsolete execution count = %d, want 0", obsoleteExecutionCount)
	}

	legacyBindingStatus := "missing_source"
	task := &models.TaskDefinition{
		TenantID:            tenantID,
		Name:                "unknown-engine vector tile set",
		Enabled:             true,
		LastExecutionStatus: &legacyBindingStatus,
		Config: commonModels.JSONMap{
			"semantic_hash": fmt.Sprintf("manager-task-%d", tenantID),
			"source": commonModels.JSONMap{
				"source_engine_id": float64(990001),
				"locator":          "addp://engine/990001/path/public/roads?type=table&item_id=91",
				"item_id":          float64(91),
				"item_fingerprint": "missing-source",
			},
			"target": commonModels.JSONMap{
				"engine_id":       float64(990002),
				"storage_locator": "addp://engine/990002/path/tiles?type=directory",
			},
		},
	}
	if err := createTaskDefinition(context.Background(), db, commonExecution.TaskTypeVectorTileSetGeneration, task); err != nil {
		t.Fatalf("create unified task definition: %v", err)
	}
	if err := normalizeTaskDefinitionBindingStatus(db); err != nil {
		t.Fatalf("normalize historical binding status: %v", err)
	}
	var migrated models.TaskDefinition
	if err := db.Where("id = ?", task.ID).Take(&migrated).Error; err != nil {
		t.Fatalf("load normalized task: %v", err)
	}
	if migrated.BindingStatus != models.TaskBindingStatusMissing || migrated.BindingIssue != models.TaskBindingIssueMissingSource || migrated.LastExecutionStatus != nil {
		t.Fatalf("normalized historical task = %#v", migrated)
	}
	if err := db.Model(&models.TaskDefinition{}).Where("id = ?", task.ID).Update("last_execution_status", commonExecution.ExecutionStatusSuccess).Error; err != nil {
		t.Fatalf("set recent execution status: %v", err)
	}
	duplicateTask := &models.TaskDefinition{
		TenantID: tenantID,
		Name:     "duplicate semantic identity",
		Enabled:  true,
		Config:   task.Config,
	}
	if err := createTaskDefinition(context.Background(), db, commonExecution.TaskTypeVectorTileSetGeneration, duplicateTask); err == nil {
		t.Fatal("duplicate task semantic identity was accepted")
	}

	var bindingCount int64
	if err := db.Model(&models.TaskResourceBinding{}).Where("task_definition_id = ?", task.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count task resource bindings: %v", err)
	}
	if bindingCount != 2 {
		t.Fatalf("task resource binding count = %d, want 2", bindingCount)
	}

	cleanupRepo := NewCleanupTaskDefinitionRepository(db, []CleanupTaskDefinitionSpec{{
		TaskType: commonExecution.TaskTypeVectorTileSetGeneration,
		Table:    "manager.task_definitions",
	}})
	definitions, err := cleanupRepo.List(context.Background(), tenantID)
	if err != nil || len(definitions) != 1 {
		t.Fatalf("list cleanup definitions = %#v, error = %v", definitions, err)
	}
	if err := cleanupRepo.MarkBindingMissing(context.Background(), definitions[0], "missing_engine"); err != nil {
		t.Fatalf("mark cleanup definition binding missing: %v", err)
	}
	disabledDefinitions, err := cleanupRepo.List(context.Background(), tenantID)
	if err != nil || len(disabledDefinitions) != 1 || disabledDefinitions[0].Enabled {
		t.Fatalf("disabled cleanup definitions = %#v, error = %v", disabledDefinitions, err)
	}
	if disabledDefinitions[0].BindingStatus != models.TaskBindingStatusMissing || disabledDefinitions[0].BindingIssue != models.TaskBindingIssueMissingEngine {
		t.Fatalf("cleanup binding status = %#v", disabledDefinitions[0])
	}
	if disabledDefinitions[0].LastExecutionStatus == nil || *disabledDefinitions[0].LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("cleanup overwrote last execution status: %#v", disabledDefinitions[0].LastExecutionStatus)
	}
	if err := cleanupRepo.HardDelete(context.Background(), disabledDefinitions[0]); err != nil {
		t.Fatalf("hard delete cleanup definition: %v", err)
	}

	var taskCount int64
	if err := db.Unscoped().Model(&models.TaskDefinition{}).Where("id = ? AND tenant_id = ?", task.ID, tenantID).Count(&taskCount).Error; err != nil {
		t.Fatalf("count deleted task: %v", err)
	}
	if err := db.Model(&models.TaskResourceBinding{}).Where("task_definition_id = ?", task.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count deleted task bindings: %v", err)
	}
	if taskCount != 0 || bindingCount != 0 {
		t.Fatalf("cleanup residual task count = %d, binding count = %d", taskCount, bindingCount)
	}

	missingManaged := &models.TaskDefinition{
		TenantID: tenantID, Name: "missing managed task", Enabled: false,
		Config: commonModels.JSONMap{"source": commonModels.JSONMap{
			"source_engine_id": uint(990003), "item_locator": "addp://engine/990003/path/models/old.ifc?type=file&item_id=93",
			"item_id": uint(93), "item_fingerprint": "missing-managed-source", "format": "ifc",
		}},
	}
	if err := createTaskDefinition(context.Background(), db, commonExecution.TaskTypeModel3DGLBGeneration, missingManaged); err != nil {
		t.Fatalf("create missing managed task: %v", err)
	}
	if err := db.Model(&models.TaskDefinition{}).Where("id = ?", missingManaged.ID).Updates(map[string]interface{}{
		"binding_status": models.TaskBindingStatusMissing,
		"binding_issue":  models.TaskBindingIssueMissingEngine,
	}).Error; err != nil {
		t.Fatalf("mark managed task missing: %v", err)
	}
	replacementManaged := &models.TaskDefinition{
		TenantID: tenantID, Name: "replacement managed task", Enabled: true,
		Config: commonModels.JSONMap{"source": commonModels.JSONMap{
			"source_engine_id": uint(990004), "item_locator": "addp://engine/990004/path/models/new.ifc?type=file&item_id=94",
			"item_id": uint(94), "item_fingerprint": "replacement-managed-source", "format": "ifc",
		}},
	}
	if err := createTaskDefinition(context.Background(), db, commonExecution.TaskTypeModel3DGLBGeneration, replacementManaged); err != nil {
		t.Fatalf("create replacement managed task: %v", err)
	}
	taskDefinitionRepo := NewTaskDefinitionRepository(db)
	if err := taskDefinitionRepo.RetireMissingTask(
		context.Background(), tenantID, commonExecution.TaskTypeModel3DGLBGeneration,
		missingManaged.ID, missingManaged.Version, replacementManaged.ID,
	); err != nil {
		t.Fatalf("retire missing managed task: %v", err)
	}
	if retired, err := taskDefinitionRepo.Get(context.Background(), tenantID, commonExecution.TaskTypeModel3DGLBGeneration, missingManaged.ID); err != nil || retired != nil {
		t.Fatalf("retired managed task = %#v, error = %v", retired, err)
	}
	if replacement, err := taskDefinitionRepo.Get(context.Background(), tenantID, commonExecution.TaskTypeModel3DGLBGeneration, replacementManaged.ID); err != nil || replacement == nil {
		t.Fatalf("replacement managed task = %#v, error = %v", replacement, err)
	}
}

func TestIntegrationPostgresManagerPPTXUsesUnifiedTaskDefinition(t *testing.T) {
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
	if err := ensureTaskDefinitionSchema(db); err != nil {
		t.Fatalf("ensure unified task definition schema: %v", err)
	}
	if err := db.AutoMigrate(&models.PPTXPDF{}); err != nil {
		t.Fatalf("migrate PPTX result: %v", err)
	}
	if err := db.Exec(`DROP TABLE IF EXISTS manager.pptx_pdf_tasks CASCADE`).Error; err != nil {
		t.Fatalf("drop stale legacy PPTX task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.pptx_pdf_tasks (id BIGSERIAL PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create legacy PPTX task table: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 970000000)
	executionID := fmt.Sprintf("manager-legacy-pptx-%d", tenantID)
	t.Cleanup(func() {
		_ = db.Exec(`DROP TABLE IF EXISTS manager.pptx_pdf_tasks CASCADE`).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.PPTXPDF{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.TaskResourceBinding{}).Error
		_ = db.Unscoped().Where("tenant_id = ?", tenantID).Delete(&models.TaskDefinition{}).Error
		_ = db.Where("tenant_id = ?", int(tenantID)).Delete(&commonExecution.TaskExecution{}).Error
	})
	if err := db.Create(&commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: executionID, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypePPTXPDFGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusSuccess, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType: commonExecution.TriggerTypeManual, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create legacy PPTX execution: %v", err)
	}
	if err := ensurePPTXPDFSchema(db); err != nil {
		t.Fatalf("ensure unified PPTX schema: %v", err)
	}
	var legacyTableExists bool
	if err := db.Raw(`SELECT to_regclass('manager.pptx_pdf_tasks') IS NOT NULL`).Scan(&legacyTableExists).Error; err != nil {
		t.Fatalf("check legacy PPTX task table: %v", err)
	}
	if legacyTableExists {
		t.Fatal("legacy pptx_pdf_tasks table still exists")
	}
	var executionCount int64
	if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", executionID).Count(&executionCount).Error; err != nil {
		t.Fatalf("count legacy PPTX execution: %v", err)
	}
	if executionCount != 0 {
		t.Fatalf("legacy PPTX execution count = %d, want 0", executionCount)
	}

	task := &models.PPTXPDFTask{
		TenantID: tenantID, Name: "slides.pptx", Enabled: true,
		Config: commonModels.JSONMap{"source": commonModels.JSONMap{
			"source_engine_id": uint(12), "item_id": uint(77), "item_fingerprint": fmt.Sprintf("pptx-%d", tenantID),
			"item_locator": "addp://engine/12/path/docs/slides.pptx?type=file&item_id=77", "format": "pptx",
		}},
	}
	if err := NewPPTXPDFRepository(db).CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create unified PPTX task: %v", err)
	}
	if task.TaskType != commonExecution.TaskTypePPTXPDFGeneration || task.SemanticKey == "" {
		t.Fatalf("unified PPTX task identity = type %q semantic %q", task.TaskType, task.SemanticKey)
	}
	var bindingCount int64
	if err := db.Model(&models.TaskResourceBinding{}).Where("tenant_id = ? AND task_definition_id = ?", tenantID, task.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count PPTX source binding: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("PPTX source binding count = %d, want 1", bindingCount)
	}
}
