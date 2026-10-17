package service

import (
	"testing"

	plugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	execution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
)

func TestBeginTargetProductionUsesActualCatalogBusinessPath(t *testing.T) {
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	item := createExecutionServiceTestExecution(t, db, task, execution.ExecutionStatusRunning)
	svc := NewExecutionService(db, execution.NewTaskExecutionRepository(db))
	engine := &ExecutionEngineService{executionService: svc}
	pipeline := &executor.TableTransferExecutor{TargetNativePreparer: &postgresql.PostgreSQLPlugin{}}
	target := executor.TableTargetPlan{Kind: executor.TableEndpointNative, Path: plugin.TabularItemPath(27, plugin.EngineCatalogTermSchema, "outdoor", "persons")}
	if _, _, err := engine.beginTableTargetProduction(t.Context(), &task, &item, uint(item.ID), target, pipeline); err == nil {
		t.Fatal("production intent admitted without owner lease")
	}
	bindExecutionServiceTestLease(t, db, svc, &item)
	if _, _, err := engine.beginTableTargetProduction(t.Context(), &task, &item, uint(item.ID+1), target, pipeline); err == nil {
		t.Fatal("production intent rebound to another execution")
	}
	locator, lease, err := engine.beginTableTargetProduction(t.Context(), &task, &item, uint(item.ID), target, pipeline)
	if err != nil {
		t.Fatal(err)
	}
	if locator != "addp://engine/27/path/outdoor/persons?type=table" || lease.ExecutionID != item.ExecutionID {
		t.Fatalf("structural root or wrong target in receipt: locator=%s lease=%+v", locator, lease)
	}
	var stored execution.TaskExecution
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	record, err := models.DecodeTargetProduction(stored.Metadata[models.TargetProductionMetadataKey])
	if err != nil || record.TargetLocator != locator || record.Phase != models.TargetProductionPending {
		t.Fatalf("intent not durable before target I/O: record=%+v error=%v", record, err)
	}
}

func TestProductionEvidenceSurvivesUpdatesAndRetry(t *testing.T) {
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	item := createExecutionServiceTestExecution(t, db, task, execution.ExecutionStatusRunning)
	svc := NewExecutionService(db, execution.NewTaskExecutionRepository(db))
	bindExecutionServiceTestLease(t, db, svc, &item)
	if err := db.Model(&task).Updates(map[string]interface{}{"status": models.TaskStatusRunning, "last_execution_id": item.ExecutionID, "last_execution_status": execution.ExecutionStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}
	deniedCtx := execution.WithReadScopes(t.Context(), nil)
	if err := svc.UpdateExecution(deniedCtx, uint(item.ID), map[string]interface{}{"records_written": int64(999)}); err == nil {
		t.Fatal("locked update bypassed execution read scope")
	}
	if err := svc.FinishExecution(deniedCtx, uint(item.ID), models.ExecutionStatusSuccess, nil, nil); err == nil {
		t.Fatal("locked completion bypassed execution read scope")
	}
	lease, err := execution.LeaseFromExecution(item)
	if err != nil {
		t.Fatal(err)
	}
	locator := "addp://engine/2/path/outdoor/persons?type=table"
	if err := svc.taskRepo.BeginTargetProduction(t.Context(), lease, locator); err != nil {
		t.Fatal(err)
	}
	if err := svc.taskRepo.RecordTargetPreparation(t.Context(), lease, locator, plugin.TableTargetPreparation{Disposition: plugin.TableTargetRecreated}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateExecution(t.Context(), uint(item.ID), map[string]interface{}{"metadata": commonmodels.JSONMap{"outputs": map[string]interface{}{"row_count": 0}}, "checkpoint_offset": 1}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateExecution(t.Context(), uint(item.ID), map[string]interface{}{"metadata": commonmodels.JSONMap{models.TargetProductionMetadataKey: nil}}); err == nil {
		t.Fatal("generic update erased receipt")
	}
	if err := svc.FinishExecution(t.Context(), uint(item.ID), models.ExecutionStatusFailed, nil, commonmodels.JSONMap{models.TargetProductionMetadataKey: nil}); err == nil {
		t.Fatal("completion erased receipt")
	}
	if err := svc.FinishExecution(t.Context(), uint(item.ID), models.ExecutionStatusFailed, nil, commonmodels.JSONMap{"lineage_facts": map[string]interface{}{"test": true}}); err != nil {
		t.Fatal(err)
	}
	var original execution.TaskExecution
	if err := db.First(&original, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	record, err := models.DecodeTargetProduction(original.Metadata[models.TargetProductionMetadataKey])
	if err != nil || record.Phase != models.TargetProductionPrepared || record.Preparation != plugin.TableTargetRecreated {
		t.Fatalf("unknown commit was lost: record=%+v error=%v", record, err)
	}
	if execution.Observe(&original).Metadata[models.TargetProductionMetadataKey] != nil {
		t.Fatal("observation exposes private receipt")
	}
	retry, err := svc.RetryExecution(t.Context(), uint(item.ID), uint(item.TenantID), 9)
	if err != nil {
		t.Fatal(err)
	}
	var retried execution.TaskExecution
	if err := db.First(&retried, retry.ID).Error; err != nil {
		t.Fatal(err)
	}
	retried.Status = execution.ExecutionStatusRunning
	bindExecutionServiceTestLease(t, db, svc, &retried)
	retryLease, err := execution.LeaseFromExecution(retried)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.taskRepo.BeginTargetProduction(t.Context(), retryLease, locator); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&retried, retry.ID).Error; err != nil {
		t.Fatal(err)
	}
	retryRecord, err := models.DecodeTargetProduction(retried.Metadata[models.TargetProductionMetadataKey])
	if err != nil || retryRecord.RetryOfExecutionID == nil || *retryRecord.RetryOfExecutionID != original.ExecutionID || retryRecord.Phase != models.TargetProductionPending {
		t.Fatalf("retry laundered original execution: %+v error=%v", retryRecord, err)
	}
}
