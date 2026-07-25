package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresTaskPrivateStateDeletionTransaction(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := MigrateCaptureProviderResources(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.TransferTask{}, &models.DeadLetter{}, &models.SyncState{}, &models.RuntimeLease{}, &models.CaptureResource{},
		&models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.SchemaChangeRequest{},
	); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	cleanable := createPostgresTaskResourceFixture(t, db, now, now.Add(-time.Minute), "expired-worker")
	guarded := createPostgresTaskResourceFixture(t, db, now, now.Add(time.Minute), "active-worker")

	repo := NewTaskResourceRepository(db)
	stats, err := repo.DeleteTaskAndPrivateState(
		context.Background(), cleanable.TenantID, cleanable.ID, true, TaskDefinitionDeletePhysical, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats != (TaskPrivateStateDeleteStats{DeadLetters: 1, SyncStates: 1, RuntimeLeases: 1, SchemaChangeRequests: 1, CaptureResources: 1, CancelledExecutions: 2}) {
		t.Fatalf("PostgreSQL delete stats = %#v", stats)
	}
	assertPostgresTaskPrivateStateCounts(t, db, cleanable, 0, 0)
	assertPostgresExecutionStates(t, db, cleanable, []string{
		commonExecution.ExecutionStatusCancelled,
		commonExecution.ExecutionStatusCancelled,
		commonExecution.ExecutionStatusSuccess,
	})

	stats, err = repo.DeleteTaskAndPrivateState(
		context.Background(), guarded.TenantID, guarded.ID, true, TaskDefinitionDeletePhysical, now,
	)
	if !errors.Is(err, ErrTaskDeletionRuntimeActive) {
		t.Fatalf("active lease delete error = %v", err)
	}
	if stats != (TaskPrivateStateDeleteStats{}) {
		t.Fatalf("active lease delete stats = %#v, want zero", stats)
	}
	assertPostgresTaskPrivateStateCounts(t, db, guarded, 1, 1)
	assertPostgresExecutionStates(t, db, guarded, []string{
		commonExecution.ExecutionStatusPending,
		commonExecution.ExecutionStatusRunning,
		commonExecution.ExecutionStatusSuccess,
	})
}

func createPostgresTaskResourceFixture(t *testing.T, db *gorm.DB, now, leaseUntil time.Time, leaseOwner string) models.TransferTask {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	tenantID := uint(900000 + now.UnixNano()%90000)
	task := models.TransferTask{
		ApplyIdentity: uuid.NewString(), TenantID: tenantID, Name: "task-private-state-" + suffix,
		TaskType: commonExecution.TaskTypeSync, Config: models.JSONMap{}, BatchSize: 100,
		Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sourceTaskID := fmt.Sprint(task.ID)
		_ = db.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, sourceTaskID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ? AND task_id = ?", task.TenantID, task.ID).Delete(&models.DeadLetter{}).Error
		_ = db.Where("task_id = ?", task.ID).Delete(&models.SyncState{}).Error
		_ = db.Where("task_id = ?", task.ID).Delete(&models.RuntimeLease{}).Error
		_ = db.Where("tenant_id = ? AND task_id = ?", task.TenantID, task.ID).Delete(&models.CaptureResource{}).Error
		_ = db.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
	})
	sourceIdentity := "integration://task-private-state/" + suffix
	state := models.SyncState{
		TaskID: task.ID, SourceIdentity: sourceIdentity, Partition: "0",
		Position: models.JSONMap{"next_offset": 10}, PositionType: "kafka_offset", PositionVersion: "v1",
	}
	lease := models.RuntimeLease{
		TaskID: task.ID, ExecutionID: "lease-" + suffix, OwnerInstanceID: leaseOwner,
		LeaseUntil: leaseUntil, HeartbeatAt: now.Add(-time.Minute), FencingToken: 1, ClaimedAt: now.Add(-time.Minute),
	}
	capture := models.CaptureResource{
		TaskID: task.ID, TenantID: task.TenantID, Generation: 1,
		ConnectorName: "connector-" + suffix, TopicName: "topic." + suffix, ConsumerGroup: "group-" + suffix,
		SourceType:     models.CaptureSourcePostgreSQL,
		SourceIdentity: sourceIdentity, SourceConnectionFingerprint: strings.Repeat("a", 64),
		SourceEngineID: 1, SourceDatabase: "postgres", SourceSchema: "public", SourceTable: "orders",
		SourceSpatialInfo: models.JSONMap{}, Status: models.CaptureStatusRunning, ResourceVersion: 1,
	}
	deadLetter := models.DeadLetter{
		Identity: uuid.NewString(), TenantID: task.TenantID, TaskID: task.ID, ApplyIdentity: task.ApplyIdentity,
		FirstExecutionID: "pending-" + suffix, LastExecutionID: "pending-" + suffix,
		SourceIdentity: sourceIdentity, SourceTopic: "orders", SourcePartition: "0", SourceOffset: 10,
		ErrorCode: "invalid_json", ErrorCategory: "record_decode", ErrorMessage: "invalid record",
		PayloadTopic: "__addp_dlq." + suffix, PayloadPartition: 0, PayloadOffset: 1, PayloadAvailable: true,
		FirstObservedAt: now, LastObservedAt: now, OccurrenceCount: 1,
	}
	for _, value := range []interface{}{&state, &lease, &capture, &deadLetter} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.PostgreSQLCaptureResource{
		CaptureResourceID: capture.ID, SlotName: "slot_" + suffix, PublicationName: "publication_" + suffix,
		SlotOwned: true, PublicationOwned: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SchemaChangeRequest{
		TaskID: task.ID, TenantID: task.TenantID, CaptureResourceID: capture.ID, Generation: 1,
		ExecutionID: uuid.NewString(), SourcePartition: "0", SourceOffset: 10, Scope: "Debezium after",
		Diff: models.JSONMap{"unexpected_fields": []string{"added"}}, ApprovedMappings: models.JSONMap{},
		FromRevision: 1, ToRevision: 2, Status: models.SchemaChangeRequestPending, DetectedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	startedAt := now.Add(-2 * time.Minute)
	statuses := []struct {
		name      string
		status    string
		startedAt *time.Time
	}{
		{name: "pending", status: commonExecution.ExecutionStatusPending},
		{name: "running", status: commonExecution.ExecutionStatusRunning, startedAt: &startedAt},
		{name: "success", status: commonExecution.ExecutionStatusSuccess},
	}
	for _, status := range statuses {
		sourceTaskID := fmt.Sprint(task.ID)
		execution := commonExecution.TaskExecution{
			TenantID: int(task.TenantID), ExecutionID: status.name + "-" + suffix,
			Module: commonExecution.ModuleTransfer, TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
			SourceTaskID: &sourceTaskID, Status: status.status, TriggerType: commonExecution.TriggerTypeManual,
			Metadata: commonModels.JSONMap{"existing": "value"}, StartedAt: status.startedAt, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&execution).Error; err != nil {
			t.Fatal(err)
		}
	}
	return task
}

func assertPostgresTaskPrivateStateCounts(t *testing.T, db *gorm.DB, task models.TransferTask, wantTask, wantPrivate int64) {
	t.Helper()
	var taskCount int64
	if err := db.Unscoped().Model(&models.TransferTask{}).Where("id = ?", task.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != wantTask {
		t.Fatalf("task %d count = %d, want %d", task.ID, taskCount, wantTask)
	}
	queries := []struct {
		model interface{}
		where string
		args  []interface{}
	}{
		{model: &models.DeadLetter{}, where: "tenant_id = ? AND task_id = ?", args: []interface{}{task.TenantID, task.ID}},
		{model: &models.SyncState{}, where: "task_id = ?", args: []interface{}{task.ID}},
		{model: &models.RuntimeLease{}, where: "task_id = ?", args: []interface{}{task.ID}},
		{model: &models.SchemaChangeRequest{}, where: "tenant_id = ? AND task_id = ?", args: []interface{}{task.TenantID, task.ID}},
		{model: &models.CaptureResource{}, where: "tenant_id = ? AND task_id = ?", args: []interface{}{task.TenantID, task.ID}},
	}
	for _, query := range queries {
		var count int64
		if err := db.Model(query.model).Where(query.where, query.args...).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != wantPrivate {
			t.Fatalf("task %d private state count = %d, want %d", task.ID, count, wantPrivate)
		}
	}
}

func assertPostgresExecutionStates(t *testing.T, db *gorm.DB, task models.TransferTask, want []string) {
	t.Helper()
	var executions []commonExecution.TaskExecution
	if err := db.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, fmt.Sprint(task.ID)).
		Order("execution_id ASC").Find(&executions).Error; err != nil {
		t.Fatal(err)
	}
	if len(executions) != len(want) {
		t.Fatalf("task %d execution count = %d, want %d", task.ID, len(executions), len(want))
	}
	statusCounts := make(map[string]int)
	for _, execution := range executions {
		statusCounts[execution.Status]++
		if execution.Status == commonExecution.ExecutionStatusCancelled && execution.Metadata["stop_reason"] != "cleanup" {
			t.Fatalf("cancelled execution metadata = %#v", execution.Metadata)
		}
	}
	for _, status := range want {
		if statusCounts[status] == 0 {
			t.Fatalf("task %d execution statuses = %#v, missing %s", task.ID, statusCounts, status)
		}
		statusCounts[status]--
	}
}
