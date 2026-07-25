package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSchemaChangeMetadataScanClaimExpiryAndFencing(t *testing.T) {
	db := newSchemaChangeRepositoryTestDB(t)
	repo := NewSchemaChangeRequestRepository(db)
	now := time.Date(2026, 7, 25, 4, 0, 0, 0, time.UTC)
	seedSchemaChangeMetadataScanRequest(t, db, 1)

	first, owned, err := repo.ClaimMetadataScan(context.Background(), 1, now, time.Minute)
	if err != nil || !owned || first.MetadataScanStatus != models.SchemaChangeMetadataScanRunning ||
		first.MetadataScanClaimToken == "" || first.MetadataScanLeaseUntil == nil || first.MetadataScanAttempt != 1 {
		t.Fatalf("first claim=%#v owned=%v err=%v", first, owned, err)
	}
	assertSchemaChangeProjection(t, db, first.ExecutionID, schemaChangeProjectionApplied, models.SchemaChangeMetadataScanRunning, 1)
	firstToken := first.MetadataScanClaimToken
	beforeExpiry, owned, err := repo.ClaimMetadataScan(context.Background(), 1, now.Add(30*time.Second), time.Minute)
	if err != nil || owned || beforeExpiry.MetadataScanClaimToken != firstToken || beforeExpiry.MetadataScanAttempt != 1 {
		t.Fatalf("before-expiry claim=%#v owned=%v err=%v", beforeExpiry, owned, err)
	}

	takeover, owned, err := repo.ClaimMetadataScan(context.Background(), 1, first.MetadataScanLeaseUntil.Add(time.Nanosecond), time.Minute)
	if err != nil || !owned || takeover.MetadataScanClaimToken == "" || takeover.MetadataScanClaimToken == firstToken ||
		takeover.MetadataScanAttempt != 2 {
		t.Fatalf("takeover claim=%#v owned=%v err=%v", takeover, owned, err)
	}

	current, completed, err := repo.CompleteMetadataScan(
		context.Background(), 1, firstToken, models.SchemaChangeMetadataScanSuccess, "stale-execution", "", now.Add(2*time.Minute),
	)
	if err != nil || completed || current.MetadataScanStatus != models.SchemaChangeMetadataScanRunning ||
		current.MetadataScanClaimToken != takeover.MetadataScanClaimToken || current.MetadataScanExecutionID != "" {
		t.Fatalf("stale completion=%#v completed=%v err=%v", current, completed, err)
	}

	current, completed, err = repo.CompleteMetadataScan(
		context.Background(), 1, takeover.MetadataScanClaimToken, models.SchemaChangeMetadataScanSuccess, "scan-execution", "", now.Add(2*time.Minute),
	)
	if err != nil || !completed || current.MetadataScanStatus != models.SchemaChangeMetadataScanSuccess ||
		current.MetadataScanClaimToken != "" || current.MetadataScanLeaseUntil != nil ||
		current.MetadataScanExecutionID != "scan-execution" || current.MetadataScanAttempt != 2 {
		t.Fatalf("owned completion=%#v completed=%v err=%v", current, completed, err)
	}
	assertSchemaChangeProjection(t, db, current.ExecutionID, schemaChangeProjectionApplied, models.SchemaChangeMetadataScanSuccess, 2)
	terminal, owned, err := repo.ClaimMetadataScan(context.Background(), 1, now.Add(10*time.Minute), time.Minute)
	if err != nil || owned || terminal.MetadataScanStatus != models.SchemaChangeMetadataScanSuccess || terminal.MetadataScanAttempt != 2 {
		t.Fatalf("terminal claim=%#v owned=%v err=%v", terminal, owned, err)
	}
}

func TestSchemaChangeMetadataScanFailureIsTerminal(t *testing.T) {
	db := newSchemaChangeRepositoryTestDB(t)
	repo := NewSchemaChangeRequestRepository(db)
	now := time.Date(2026, 7, 25, 5, 0, 0, 0, time.UTC)
	seedSchemaChangeMetadataScanRequest(t, db, 2)

	claimed, owned, err := repo.ClaimMetadataScan(context.Background(), 2, now, time.Minute)
	if err != nil || !owned {
		t.Fatalf("claim=%#v owned=%v err=%v", claimed, owned, err)
	}
	failed, completed, err := repo.CompleteMetadataScan(
		context.Background(), 2, claimed.MetadataScanClaimToken, models.SchemaChangeMetadataScanFailed, "", "Meta unavailable", now.Add(time.Second),
	)
	if err != nil || !completed || failed.MetadataScanStatus != models.SchemaChangeMetadataScanFailed ||
		failed.MetadataScanError != "Meta unavailable" || failed.MetadataScanClaimToken != "" || failed.MetadataScanLeaseUntil != nil {
		t.Fatalf("failed completion=%#v completed=%v err=%v", failed, completed, err)
	}
	assertSchemaChangeProjection(t, db, failed.ExecutionID, schemaChangeProjectionApplied, models.SchemaChangeMetadataScanFailed, 1)
	terminal, owned, err := repo.ClaimMetadataScan(context.Background(), 2, now.Add(10*time.Minute), time.Minute)
	if err != nil || owned || terminal.MetadataScanStatus != models.SchemaChangeMetadataScanFailed || terminal.MetadataScanAttempt != 1 {
		t.Fatalf("failed terminal claim=%#v owned=%v err=%v", terminal, owned, err)
	}
}

func TestStopPendingSchemaChangeResolvesExecutionProjection(t *testing.T) {
	db := newSchemaChangeRepositoryTestDB(t)
	seedSchemaChangeMetadataScanRequest(t, db, 3)
	if err := db.Model(&models.SchemaChangeRequest{}).Where("id = ?", 3).Updates(map[string]interface{}{
		"status": models.SchemaChangeRequestPending, "metadata_scan_status": "",
	}).Error; err != nil {
		t.Fatal(err)
	}
	var request models.SchemaChangeRequest
	if err := db.First(&request, 3).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return UpdateSchemaChangeExecutionProjectionTx(tx, &request) }); err != nil {
		t.Fatal(err)
	}
	stoppedAt := time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return StopPendingSchemaChangeExecutionProjectionTx(tx, request.TaskID, request.TenantID, stoppedAt)
	}); err != nil {
		t.Fatal(err)
	}
	var execution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", request.ExecutionID).First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	continuous, _ := execution.Metadata["continuous"].(map[string]interface{})
	change, _ := continuous["schema_change"].(map[string]interface{})
	if change["status"] != schemaChangeProjectionStopped || change["stopped_at"] == nil {
		t.Fatalf("stopped schema change projection=%#v", change)
	}
}

func newSchemaChangeRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:schema_change_repository_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE transfer.schema_change_requests (
			id INTEGER PRIMARY KEY,
			task_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			generation INTEGER NOT NULL,
			execution_id TEXT NOT NULL,
			source_partition TEXT NOT NULL,
			source_offset INTEGER NOT NULL,
			scope TEXT NOT NULL,
			diff JSON NOT NULL,
			from_revision INTEGER NOT NULL,
			to_revision INTEGER NOT NULL,
			status TEXT NOT NULL,
		metadata_scan_status TEXT NOT NULL,
		metadata_scan_claim_token TEXT NOT NULL DEFAULT '',
		metadata_scan_lease_until DATETIME,
		metadata_scan_attempt INTEGER NOT NULL DEFAULT 0,
		metadata_scan_execution_id TEXT NOT NULL DEFAULT '',
			metadata_scan_error TEXT NOT NULL DEFAULT '',
			detected_at DATETIME NOT NULL,
			updated_at DATETIME
		)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL UNIQUE,
			metadata JSON,
			updated_at DATETIME
		)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func seedSchemaChangeMetadataScanRequest(t *testing.T, db *gorm.DB, id uint) {
	t.Helper()
	executionID := fmt.Sprintf("schema-exec-%d", id)
	if err := db.Exec(`INSERT INTO common.task_executions (tenant_id, execution_id, metadata, updated_at)
		VALUES (?, ?, ?, ?)`, 7, executionID, `{"continuous":{}}`, time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO transfer.schema_change_requests
			(id, task_id, tenant_id, generation, execution_id, source_partition, source_offset, scope, diff,
			 from_revision, to_revision, status, metadata_scan_status, detected_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, id, 7, 1, executionID, "0", 17, "public.orders", `{"unexpected_fields":["new_field"]}`,
		1, 2, models.SchemaChangeRequestApplied, models.SchemaChangeMetadataScanPending, time.Now(),
	).Error; err != nil {
		t.Fatal(err)
	}
}

func assertSchemaChangeProjection(t *testing.T, db *gorm.DB, executionID, status string, scanStatus models.SchemaChangeMetadataScanStatus, attempt uint64) {
	t.Helper()
	var execution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", executionID).First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	continuous, _ := execution.Metadata["continuous"].(map[string]interface{})
	change, _ := continuous["schema_change"].(map[string]interface{})
	scan, _ := change["metadata_scan"].(map[string]interface{})
	if change["status"] != status || scan["status"] != string(scanStatus) || scan["attempt"] != float64(attempt) {
		t.Fatalf("schema change projection=%#v", change)
	}
}
