package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	_ "github.com/addp/common/format/plugins/csv"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTransferCleanupScanFindsTaskDefinitionsByEngineContext(t *testing.T) {
	t.Parallel()

	db := newTransferCleanupTestDB(t)
	svc := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	createTransferCleanupTestTask(t, db, 7, "match-source", 1, 2, true)
	createTransferCleanupTestTask(t, db, 7, "match-target", 3, 1, true)
	createTransferCleanupTestTask(t, db, 7, "other", 3, 4, true)

	stats, err := svc.ScanReclaimCandidates(context.Background(), 7, map[string]interface{}{"engine_id": 1})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if stats.TaskDefinitions != 2 {
		t.Fatalf("TaskDefinitions = %d, want 2", stats.TaskDefinitions)
	}

	stats, err = svc.ScanReclaimCandidates(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() without context error = %v", err)
	}
	if stats.TaskDefinitions != 0 {
		t.Fatalf("TaskDefinitions without lifecycle context = %d, want 0", stats.TaskDefinitions)
	}
}

func TestTransferCleanupLogicalDisablesTaskDefinitions(t *testing.T) {
	t.Parallel()

	db := newTransferCleanupTestDB(t)
	svc := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	task := createTransferCleanupTestTask(t, db, 7, "match", 1, 2, true)
	createTransferCleanupTestTask(t, db, 7, "other", 3, 4, true)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"engine_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DisabledTaskDefinitions != 1 || stats.DeletedTaskDefinitions != 0 {
		t.Fatalf("stats = %#v, want one disabled", stats)
	}

	var updated models.TransferTask
	if err := db.First(&updated, task.ID).Error; err != nil {
		t.Fatalf("load updated task: %v", err)
	}
	if updated.Enabled || updated.NextRunAt != nil || updated.Status != models.TaskStatusIdle {
		t.Fatalf("updated task = %#v, want disabled idle task without next_run_at", updated)
	}
}

func TestTransferCleanupPhysicalDeletesTaskDefinitions(t *testing.T) {
	t.Parallel()

	db := newTransferCleanupTestDB(t)
	svc := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	task := createTransferCleanupTestTask(t, db, 7, "match", 1, 2, true)
	other := createTransferCleanupTestTask(t, db, 7, "other", 3, 4, true)
	if err := db.Model(&models.TransferTask{}).Where("id = ?", task.ID).Update("status", models.TaskStatusIdle).Error; err != nil {
		t.Fatal(err)
	}

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"engine_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DeletedTaskDefinitions != 1 || stats.DisabledTaskDefinitions != 0 {
		t.Fatalf("stats = %#v, want one deleted", stats)
	}

	var deleted models.TransferTask
	if err := db.First(&deleted, task.ID).Error; err == nil {
		t.Fatal("physically cleaned task should be deleted from active query")
	}
	if err := db.Unscoped().First(&deleted, task.ID).Error; err == nil {
		t.Fatal("physically cleaned task should be deleted from table")
	}

	var kept models.TransferTask
	if err := db.First(&kept, other.ID).Error; err != nil {
		t.Fatalf("other task should remain active: %v", err)
	}
}

func TestTransferCleanupPhysicalRejectsRunningBoundedTask(t *testing.T) {
	t.Parallel()

	db := newTransferCleanupTestDB(t)
	svc := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	task := createTransferCleanupTestTask(t, db, 7, "running-bounded", 1, 2, true)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"engine_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DeletedTaskDefinitions != 0 || len(stats.Errors) != 1 {
		t.Fatalf("stats = %#v, want one runtime-active cleanup error", stats)
	}
	var count int64
	if err := db.Unscoped().Model(&models.TransferTask{}).Where("id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("running bounded task count = %d, want 1", count)
	}
}

func TestTransferCleanupUsesCaptureOwnerForPostgreSQLCDC(t *testing.T) {
	db := newTransferCleanupTestDB(t)
	task := models.TransferTask{
		TenantID: 7, Name: "cdc-cleanup", TaskType: "sync", Config: validPostgreSQLCDCTaskConfig(),
		Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	control := &fakeCaptureControl{}
	cleanup := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	cleanup.SetCaptureControl(control)
	stats, err := cleanup.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"engine_id": 12})
	if err != nil {
		t.Fatal(err)
	}
	if stats.CaptureResources != 1 || stats.CleanedCaptureResources != 1 || control.stopCalls != 1 {
		t.Fatalf("capture cleanup stats=%+v calls=%d", stats, control.stopCalls)
	}
}

func TestTransferCleanupPhysicalDeletesDLQTopicBeforeIndexAndTask(t *testing.T) {
	db := newTransferCleanupTestDB(t)
	task := models.TransferTask{
		TenantID: 7, Name: "business-kafka-cleanup", TaskType: commonExecution.TaskTypeSync,
		Config: validContinuousTaskConfig(), BatchSize: 100, Status: models.TaskStatusIdle,
		DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	createTransferCleanupDeadLetter(t, db, task)
	cleaner := &fakeDeadLetterTopicCleaner{db: db}
	svc := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	svc.SetDeadLetterTopicCleaner(cleaner)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Errors) != 0 || stats.CleanedDeadLetterTopics != 1 || stats.DeletedDeadLetterRecords != 1 || stats.DeletedTaskDefinitions != 1 {
		t.Fatalf("cleanup stats = %#v", stats)
	}
	if cleaner.calls != 1 || !cleaner.sawIndexAtDelete {
		t.Fatalf("topic cleaner calls=%d saw_index=%v", cleaner.calls, cleaner.sawIndexAtDelete)
	}
	var count int64
	if err := db.Model(&models.DeadLetter{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("remaining dead-letter count=%d err=%v", count, err)
	}
}

func TestTransferCleanupKeepsIndexAndTaskWhenDLQTopicDeleteFails(t *testing.T) {
	db := newTransferCleanupTestDB(t)
	task := models.TransferTask{
		TenantID: 7, Name: "business-kafka-cleanup-failure", TaskType: commonExecution.TaskTypeSync,
		Config: validContinuousTaskConfig(), BatchSize: 100, Status: models.TaskStatusIdle,
		DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	createTransferCleanupDeadLetter(t, db, task)
	cleaner := &fakeDeadLetterTopicCleaner{db: db, err: errors.New("Kafka admin unavailable")}
	svc := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	svc.SetDeadLetterTopicCleaner(cleaner)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Errors) != 1 || stats.DeletedTaskDefinitions != 0 || stats.DeletedDeadLetterRecords != 0 {
		t.Fatalf("cleanup failure stats = %#v", stats)
	}
	var taskCount, deadLetterCount int64
	_ = db.Model(&models.TransferTask{}).Where("id = ?", task.ID).Count(&taskCount).Error
	_ = db.Model(&models.DeadLetter{}).Where("task_id = ?", task.ID).Count(&deadLetterCount).Error
	if taskCount != 1 || deadLetterCount != 1 {
		t.Fatalf("cleanup failure deleted facts: task=%d dead_letters=%d", taskCount, deadLetterCount)
	}
}

func TestTransferCleanupLogicalKeepsDLQTopicAndIndex(t *testing.T) {
	db := newTransferCleanupTestDB(t)
	task := models.TransferTask{
		TenantID: 7, Name: "business-kafka-logical", TaskType: commonExecution.TaskTypeSync,
		Config: validContinuousTaskConfig(), BatchSize: 100, Status: models.TaskStatusIdle,
		DesiredState: models.TaskDesiredStateStopped, Enabled: true,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	createTransferCleanupDeadLetter(t, db, task)
	cleaner := &fakeDeadLetterTopicCleaner{db: db}
	svc := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	svc.SetDeadLetterTopicCleaner(cleaner)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"tenant_id": 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Errors) != 0 || stats.DisabledTaskDefinitions != 1 || cleaner.calls != 0 || stats.DeletedDeadLetterRecords != 0 {
		t.Fatalf("logical cleanup stats=%#v cleaner_calls=%d", stats, cleaner.calls)
	}
	var count int64
	if err := db.Model(&models.DeadLetter{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("logical cleanup dead-letter count=%d err=%v", count, err)
	}
}

func TestTransferCleanupUsesRetainedDLQIndexAfterTaskConfigChanged(t *testing.T) {
	db := newTransferCleanupTestDB(t)
	task := createTransferCleanupTestTask(t, db, 7, "changed-from-kafka", 1, 2, false)
	task.Status = models.TaskStatusIdle
	if err := db.Model(&models.TransferTask{}).Where("id = ?", task.ID).Update("status", task.Status).Error; err != nil {
		t.Fatal(err)
	}
	createTransferCleanupDeadLetter(t, db, task)
	cleaner := &fakeDeadLetterTopicCleaner{db: db}
	svc := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	svc.SetDeadLetterTopicCleaner(cleaner)
	if _, err := svc.DeleteTaskAndOwnedResources(context.Background(), &task, repository.TaskDefinitionDeleteSoft); err != nil {
		t.Fatal(err)
	}
	if cleaner.calls != 1 {
		t.Fatalf("retained DLQ index did not trigger topic cleanup, calls=%d", cleaner.calls)
	}
}

func TestTaskServiceDeleteUsesTaskOwnedResourceCleanup(t *testing.T) {
	db := newTransferCleanupTestDB(t)
	task := models.TransferTask{
		TenantID: 7, Name: "direct-delete", TaskType: commonExecution.TaskTypeSync,
		Config: validContinuousTaskConfig(), BatchSize: 100, Status: models.TaskStatusIdle,
		DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	createTransferCleanupDeadLetter(t, db, task)
	cleaner := &fakeDeadLetterTopicCleaner{db: db}
	cleanup := NewTransferCleanupService(db, nil, nil, transferCleanupTestConfig())
	cleanup.SetDeadLetterTopicCleaner(cleaner)
	tasks := NewTaskService(db, nil, nil, nil)
	tasks.SetTaskOwnedResourceCleanup(cleanup)

	if err := tasks.DeleteTask(context.Background(), task.ID, task.TenantID); err != nil {
		t.Fatal(err)
	}
	var activeCount, unscopedCount, deadLetterCount int64
	_ = db.Model(&models.TransferTask{}).Where("id = ?", task.ID).Count(&activeCount).Error
	_ = db.Unscoped().Model(&models.TransferTask{}).Where("id = ?", task.ID).Count(&unscopedCount).Error
	_ = db.Model(&models.DeadLetter{}).Where("task_id = ?", task.ID).Count(&deadLetterCount).Error
	if activeCount != 0 || unscopedCount != 1 || deadLetterCount != 0 || cleaner.calls != 1 {
		t.Fatalf("direct delete facts active=%d unscoped=%d dead_letters=%d cleaner_calls=%d", activeCount, unscopedCount, deadLetterCount, cleaner.calls)
	}
}

func newTransferCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatalf("attach transfer schema: %v", err)
	}
	stmt := `CREATE TABLE transfer.transfer_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		apply_identity TEXT NOT NULL UNIQUE,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		task_type TEXT NOT NULL,
		config JSON,
		schedule TEXT,
		batch_size INTEGER,
		enabled BOOLEAN,
		auto_scan_metadata BOOLEAN,
		status TEXT,
		desired_state TEXT NOT NULL DEFAULT 'stopped',
		progress REAL,
		created_by INTEGER,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		next_run_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("create transfer task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE transfer.dead_letters (
		identity TEXT PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		task_id INTEGER NOT NULL,
		apply_identity TEXT NOT NULL,
		first_execution_id TEXT NOT NULL,
		last_execution_id TEXT NOT NULL,
		source_identity TEXT NOT NULL,
		source_topic TEXT NOT NULL,
		source_partition TEXT NOT NULL,
		source_offset INTEGER NOT NULL,
		source_timestamp DATETIME,
		error_code TEXT NOT NULL,
		error_category TEXT NOT NULL,
		error_message TEXT NOT NULL,
		payload_topic TEXT NOT NULL,
		payload_partition INTEGER NOT NULL,
		payload_offset INTEGER NOT NULL,
		payload_available BOOLEAN NOT NULL,
		first_observed_at DATETIME NOT NULL,
		last_observed_at DATETIME NOT NULL,
		occurrence_count INTEGER NOT NULL,
		created_at DATETIME,
		updated_at DATETIME,
		UNIQUE(apply_identity, source_identity, source_partition, source_offset)
	)`).Error; err != nil {
		t.Fatalf("create dead-letter table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE transfer.sync_states (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id INTEGER NOT NULL,
		source_identity TEXT,
		partition TEXT
	)`).Error; err != nil {
		t.Fatalf("create sync_states table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE transfer.runtime_leases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id INTEGER NOT NULL,
		execution_id TEXT,
		owner_instance_id TEXT NOT NULL DEFAULT '',
		lease_until DATETIME NOT NULL,
		heartbeat_at DATETIME,
		fencing_token INTEGER,
		claimed_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create runtime_leases table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE transfer.capture_resources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id INTEGER NOT NULL,
		tenant_id INTEGER NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create capture_resources table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE transfer.schema_change_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id INTEGER NOT NULL,
		tenant_id INTEGER NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create schema_change_requests table: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL,
		module TEXT NOT NULL,
		task_type TEXT NOT NULL,
		source_task_id TEXT,
		status TEXT NOT NULL,
		metadata JSON,
		started_at DATETIME,
		completed_at DATETIME,
		execution_time_ms INTEGER,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	return db
}

func transferCleanupTestConfig() TaskOwnedCleanupConfig {
	return TaskOwnedCleanupConfig{RuntimeStopTimeout: time.Second, RuntimeStopPollInterval: time.Millisecond}
}

type fakeDeadLetterTopicCleaner struct {
	db               *gorm.DB
	err              error
	calls            int
	sawIndexAtDelete bool
}

func (f *fakeDeadLetterTopicCleaner) DeleteTaskTopic(_ context.Context, tenantID, taskID uint) error {
	f.calls++
	var count int64
	if f.db != nil {
		_ = f.db.Model(&models.DeadLetter{}).Where("tenant_id = ? AND task_id = ?", tenantID, taskID).Count(&count).Error
	}
	f.sawIndexAtDelete = count > 0
	return f.err
}

func createTransferCleanupDeadLetter(t *testing.T, db *gorm.DB, task models.TransferTask) {
	t.Helper()
	now := time.Now().UTC()
	deadLetter := models.DeadLetter{
		Identity: "a220d5ad-d86e-52ca-ad4f-5ff2d8bfad1c", TenantID: task.TenantID, TaskID: task.ID,
		ApplyIdentity: task.ApplyIdentity, FirstExecutionID: "execution-1", LastExecutionID: "execution-1",
		SourceIdentity: "addp://engine/30/path/orders.events?type=topic", SourceTopic: "orders.events",
		SourcePartition: "0", SourceOffset: 10, ErrorCode: "invalid_json_object", ErrorCategory: "record_decode",
		ErrorMessage: "record value must be a JSON object", PayloadTopic: "__addp_dlq.7.1",
		PayloadPartition: 0, PayloadOffset: 10, PayloadAvailable: true,
		FirstObservedAt: now, LastObservedAt: now, OccurrenceCount: 1,
	}
	if err := db.Create(&deadLetter).Error; err != nil {
		t.Fatal(err)
	}
}

func createTransferCleanupTestTask(t *testing.T, db *gorm.DB, tenantID uint, name string, sourceEngineID uint, targetEngineID uint, enabled bool) models.TransferTask {
	t.Helper()
	nextRunAt := time.Now().Add(time.Hour)
	task := models.TransferTask{
		TenantID:  tenantID,
		Name:      name,
		TaskType:  commonExecution.TaskTypeSync,
		Config:    transferCleanupTestTaskConfig(sourceEngineID, targetEngineID),
		BatchSize: 100,
		Enabled:   enabled,
		Status:    models.TaskStatusRunning,
		NextRunAt: &nextRunAt,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create transfer task: %v", err)
	}
	return task
}

func transferCleanupTestTaskConfig(sourceEngineID uint, targetEngineID uint) models.JSONMap {
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator":        transferCleanupTableLocator(sourceEngineID, "public", "roads"),
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": transferCleanupDirectoryLocator(targetEngineID, "exports"),
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         "csv",
			"policy":         map[string]interface{}{"apply_mode": "replace"},
		},
	}
}

func transferCleanupTableLocator(engineID uint, schema string, table string) string {
	return "addp://engine/" + uintText(engineID) + "/path/" + schema + "/" + table + "?type=table"
}

func transferCleanupDirectoryLocator(engineID uint, path string) string {
	return "addp://engine/" + uintText(engineID) + "/path/" + path + "?type=directory"
}

func uintText(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
