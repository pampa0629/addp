package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
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
	firstLeaseCtx := managerExecutionLeaseContextForTest(t, db, "execution-1", 7, now.Add(3*time.Second))
	otherLeaseCtx := managerExecutionLeaseContextForTest(t, db, "execution-3", 7, now.Add(2*time.Second))
	if err := repo.Timeout(otherLeaseCtx, 7, "execution-3", now.Add(2*time.Second), "timeout", "timed out"); err != nil {
		t.Fatalf("Timeout() error = %v", err)
	}
	timedOut, err := repo.GetByExecutionID(context.Background(), 7, "execution-3")
	if err != nil || timedOut == nil || timedOut.Status != commonExecution.ExecutionStatusTimeout {
		t.Fatalf("timed out execution = (%#v, %v)", timedOut, err)
	}

	if err := repo.Start(firstLeaseCtx, 7, "execution-1", now.Add(3*time.Second)); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := repo.Complete(firstLeaseCtx, 7, "execution-1", now.Add(3*time.Second), 10, map[string]interface{}{"result_id": 1}); err != nil {
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

func TestDataProfileProjectionCleanupDeletesCachedResultsAndSuppressesConditionValues(t *testing.T) {
	db := newDataProfileRepositoryTestDB(t)
	profiles := NewDataProfileRepository(db)
	executions := NewDataProfileExecutionRepository(db)
	for _, fingerprint := range []string{"protected-item", "other-item"} {
		state := &models.DataProfile{
			TenantID: 7, ItemFingerprint: fingerprint, EngineID: 11,
			Locator: "addp://engine/11/item/" + fingerprint, SourceVersion: "version-1",
			DependencySnapshot: []byte(`{"source_version":"version-1"}`),
			ProfileMode:        dataprofile.ModeSample, ProfileConfigHash: "config-" + fingerprint,
			LastExecutionID: "execution-" + fingerprint,
		}
		profile := dataprofile.Profile{
			SchemaVersion: dataprofile.SchemaVersionV2, Mode: dataprofile.ModeSample,
			DataScope:    dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll},
			SampleMethod: "systematic_pages_reservoir", FieldCount: 1, ProfiledAt: time.Now().UTC(),
			Fields: []dataprofile.FieldProfile{{Name: "phone", Type: datatype.FieldTypeString, Status: dataprofile.MetricStatusComputed}},
		}
		if err := profiles.ReplaceCurrent(context.Background(), state, profile); err != nil {
			t.Fatal(err)
		}
		execution := newDataProfileRepositoryTestExecution("cleanup-"+fingerprint, time.Now().UTC())
		execution.ExecutionConfig = commonModels.JSONMap{
			"item_fingerprint": fingerprint,
			"data_scope": map[string]interface{}{
				"kind":       "condition",
				"conditions": []interface{}{map[string]interface{}{"field": "phone", "operator": "eq", "value": "13661384499"}},
			},
		}
		if err := db.Create(execution).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := profiles.DeleteByItemFingerprints(context.Background(), tx, 7, []string{"protected-item"}); err != nil {
			return err
		}
		return executions.SuppressConditionalScopesByItemFingerprints(context.Background(), tx, 7, []string{"protected-item"})
	}); err != nil {
		t.Fatal(err)
	}

	removed, _, err := profiles.GetCurrent(context.Background(), 7, "protected-item", dataprofile.ModeSample, "config-protected-item")
	if err != nil || removed != nil {
		t.Fatalf("protected profile = %#v, err = %v", removed, err)
	}
	remaining, _, err := profiles.GetCurrent(context.Background(), 7, "other-item", dataprofile.ModeSample, "config-other-item")
	if err != nil || remaining == nil {
		t.Fatalf("other profile = %#v, err = %v", remaining, err)
	}

	protectedExecution, err := executions.GetByExecutionID(context.Background(), 7, "cleanup-protected-item")
	if err != nil || protectedExecution == nil {
		t.Fatalf("protected execution = %#v, err = %v", protectedExecution, err)
	}
	payload, _ := json.Marshal(protectedExecution.ExecutionConfig)
	if strings.Contains(string(payload), "13661384499") || !strings.Contains(string(payload), "values_suppressed") {
		t.Fatalf("protected execution config = %s", payload)
	}
	otherExecution, err := executions.GetByExecutionID(context.Background(), 7, "cleanup-other-item")
	if err != nil || otherExecution == nil {
		t.Fatalf("other execution = %#v, err = %v", otherExecution, err)
	}
	otherPayload, _ := json.Marshal(otherExecution.ExecutionConfig)
	if !strings.Contains(string(otherPayload), "13661384499") {
		t.Fatalf("unrelated execution config was changed: %s", otherPayload)
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
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("execute test schema statement: %v", err)
		}
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
