package repository

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDataProfileRepositoryReplaceCurrentAtomicallyReplacesFields(t *testing.T) {
	db := newDataProfileRepositoryTestDB(t)
	repo := NewDataProfileRepository(db)
	profiledAt := time.Date(2026, 7, 27, 1, 2, 3, 0, time.UTC)
	state := &models.DataProfile{
		TenantID: 7, ItemFingerprint: "fingerprint", EngineID: 11,
		Locator: "addp://engine/11/item/roads", SourceVersion: "version-1",
		DependencySnapshot: []byte(`{"source_version":"version-1"}`),
		ProfileMode:        dataprofile.ModeSample, ProfileConfigHash: "config",
		LastExecutionID: "execution-1",
	}
	first := dataprofile.Profile{
		SchemaVersion: dataprofile.SchemaVersionV2, Mode: dataprofile.ModeSample, DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll},
		SampleMethod: "systematic_pages_reservoir", SampleSize: 2, RowsScanned: 4,
		FieldCount: 2, ProfiledAt: profiledAt,
		Fields: []dataprofile.FieldProfile{
			{Name: "id", Type: datatype.FieldTypeBigInt, Status: dataprofile.MetricStatusComputed},
			{Name: "name", Type: datatype.FieldTypeString, Status: dataprofile.MetricStatusComputed},
		},
	}
	if err := repo.ReplaceCurrent(context.Background(), state, first); err != nil {
		t.Fatalf("ReplaceCurrent(first) error = %v", err)
	}
	firstID := state.ID

	state.SourceVersion = "version-2"
	state.LastExecutionID = "execution-2"
	second := first
	second.SampleSize = 1
	second.FieldCount = 1
	second.Fields = []dataprofile.FieldProfile{
		{Name: "amount", Type: datatype.FieldTypeDouble, Status: dataprofile.MetricStatusComputed},
	}
	if err := repo.ReplaceCurrent(context.Background(), state, second); err != nil {
		t.Fatalf("ReplaceCurrent(second) error = %v", err)
	}
	if state.ID != firstID {
		t.Fatalf("profile id = %d, want stable id %d", state.ID, firstID)
	}

	stored, decoded, err := repo.GetCurrent(context.Background(), 7, "fingerprint", dataprofile.ModeSample, "config")
	if err != nil {
		t.Fatalf("GetCurrent() error = %v", err)
	}
	if stored == nil || stored.SourceVersion != "version-2" || stored.LastExecutionID != "execution-2" {
		t.Fatalf("stored state = %#v", stored)
	}
	if decoded == nil || len(decoded.Fields) != 1 || decoded.Fields[0].Name != "amount" {
		t.Fatalf("decoded profile = %#v", decoded)
	}

	var fieldCount int64
	if err := db.Model(&models.DataProfileField{}).Where("profile_id = ?", firstID).Count(&fieldCount).Error; err != nil {
		t.Fatalf("count fields: %v", err)
	}
	if fieldCount != 1 {
		t.Fatalf("field count = %d, want 1", fieldCount)
	}
}

func TestDataProfileExecutionRepositoryReusesOnlyActiveTarget(t *testing.T) {
	db := newDataProfileRepositoryTestDB(t)
	repo := NewDataProfileExecutionRepository(db)
	now := time.Date(2026, 7, 27, 1, 2, 3, 0, time.UTC)

	first, created, err := repo.CreateOrReuseActive(context.Background(), "target-a", newDataProfileRepositoryTestExecution("execution-1", now))
	if err != nil || !created {
		t.Fatalf("first create = (%#v, %v, %v)", first, created, err)
	}
	reused, created, err := repo.CreateOrReuseActive(context.Background(), "target-a", newDataProfileRepositoryTestExecution("execution-2", now.Add(time.Second)))
	if err != nil || created || reused.ExecutionID != "execution-1" {
		t.Fatalf("active reuse = (%#v, %v, %v)", reused, created, err)
	}
	other, created, err := repo.CreateOrReuseActive(context.Background(), "target-b", newDataProfileRepositoryTestExecution("execution-3", now.Add(2*time.Second)))
	if err != nil || !created || other.ExecutionID != "execution-3" {
		t.Fatalf("other target create = (%#v, %v, %v)", other, created, err)
	}
	if err := repo.Timeout(context.Background(), 7, "execution-3", now.Add(2*time.Second), "timeout", "timed out"); err != nil {
		t.Fatalf("Timeout() error = %v", err)
	}
	timedOut, err := repo.GetByExecutionID(context.Background(), 7, "execution-3")
	if err != nil || timedOut == nil || timedOut.Status != commonExecution.ExecutionStatusTimeout {
		t.Fatalf("timed out execution = (%#v, %v)", timedOut, err)
	}

	if err := repo.Start(context.Background(), 7, "execution-1", now.Add(3*time.Second)); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := repo.Complete(context.Background(), 7, "execution-1", now.Add(3*time.Second), 10, map[string]interface{}{"result_id": 1}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	completed, err := repo.GetByExecutionID(context.Background(), 7, "execution-1")
	if err != nil || completed == nil || completed.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("GetByExecutionID() = (%#v, %v)", completed, err)
	}
	next, created, err := repo.CreateOrReuseActive(context.Background(), "target-a", newDataProfileRepositoryTestExecution("execution-4", now.Add(4*time.Second)))
	if err != nil || !created || next.ExecutionID != "execution-4" {
		t.Fatalf("post-completion create = (%#v, %v, %v)", next, created, err)
	}
}

func newDataProfileRepositoryTestExecution(executionID string, createdAt time.Time) *commonExecution.TaskExecution {
	return &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: executionID, Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeDataProfiling, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		ExecutionConfig: commonModels.JSONMap{"target_key": map[string]string{
			"execution-1": "target-a", "execution-2": "target-a", "execution-3": "target-b", "execution-4": "target-a",
		}[executionID]},
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}

func newDataProfileRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:data-profile-repository?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		"ATTACH DATABASE ':memory:' AS manager",
		"ATTACH DATABASE ':memory:' AS common",
		`CREATE TABLE manager.data_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
			item_id INTEGER, engine_id INTEGER NOT NULL, locator TEXT NOT NULL, source_version TEXT NOT NULL,
			dependency_snapshot JSON NOT NULL, profile_mode TEXT NOT NULL, profile_config_hash TEXT NOT NULL,
			data_scope JSON NOT NULL DEFAULT '{"kind":"all"}',
			schema_version TEXT NOT NULL, sample_method TEXT NOT NULL, sample_size INTEGER NOT NULL,
			rows_scanned INTEGER NOT NULL, row_count INTEGER, row_count_exact NUMERIC NOT NULL,
			field_count INTEGER NOT NULL, truncated NUMERIC NOT NULL, partial NUMERIC NOT NULL,
			observations JSON NOT NULL, last_execution_id TEXT NOT NULL, profiled_at DATETIME NOT NULL,
			created_at DATETIME, updated_at DATETIME,
			UNIQUE (tenant_id, item_fingerprint, profile_mode, profile_config_hash)
		)`,
		`CREATE TABLE manager.data_profile_fields (
			id INTEGER PRIMARY KEY AUTOINCREMENT, profile_id INTEGER NOT NULL, position INTEGER NOT NULL,
			name TEXT NOT NULL, type TEXT NOT NULL, status TEXT NOT NULL, profile JSON NOT NULL,
			created_at DATETIME, updated_at DATETIME, UNIQUE (profile_id, name)
		)`,
		`CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, execution_id TEXT NOT NULL UNIQUE,
			module TEXT NOT NULL, task_type TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', source_task_id TEXT,
			source_task_name TEXT, parent_execution_id TEXT, status TEXT NOT NULL, progress INTEGER,
			current_step TEXT, trigger_type TEXT NOT NULL, triggered_by INTEGER, execution_config JSON,
			error_details JSON, metadata JSON, execution_time_ms INTEGER, rows_affected INTEGER,
			records_read INTEGER, records_written INTEGER, bytes_read INTEGER, bytes_written INTEGER,
			started_at DATETIME, completed_at DATETIME, created_at DATETIME, updated_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("execute test schema statement: %v", err)
		}
	}
	addTaskExecutionAuthorizationColumns(t, db)
	t.Cleanup(func() {
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
