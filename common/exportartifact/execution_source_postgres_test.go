package exportartifact

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestExportExecutionSourceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_EXECUTION_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires Common PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	schema := "export_source_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := tx.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	table := schema + ".sessions"
	if err := EnsureStore(tx, table); err != nil {
		t.Fatal(err)
	}
	// A pre-existing session with unknown provenance must survive schema installation,
	// without acquiring a guessed user binding for an already-created execution.
	if err := tx.Exec("ALTER TABLE " + table + " DROP COLUMN execution_request_digest, DROP COLUMN protection").Error; err != nil {
		t.Fatal(err)
	}
	oldID := uuid.NewString()
	if err := tx.Table(table).Create(map[string]interface{}{
		"tenant_id": 7, "user_id": 9, "source_item_locator": "source", "format": "csv", "file_name": "orders.csv",
		"target_parent_locator": "", "target_locator": "", "transfer_execution_id": oldID, "status": StatusPending,
		"created_at": time.Now(), "updated_at": time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(tx, table); err != nil {
		t.Fatal(err)
	}
	store := NewGormStore(tx, table)
	if got, err := store.GetExecutionSource(t.Context(), 1, 7, client.ExportExecutionSourceRequest{ExecutionID: oldID, RequestDigest: strings.Repeat("a", 64)}); err != nil || got != nil {
		t.Fatalf("old unknown source accepted: err=%v", err)
	}
	session := &Session{TenantID: 7, UserID: 9, TransferExecutionID: uuid.NewString(), ExecutionRequestDigest: strings.Repeat("b", 64), Status: StatusPending}
	if err := store.Create(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	selection := &client.TransferSourceProtectionSelection{SourceLocator: "addp://engine/11/path/public/persons?type=table", Choices: []dataprotection.FieldRoleChoice{{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "hash"}}}}
	if err := tx.Table(table).Where("id = ?", session.ID).Update("protection", gorm.Expr("?::jsonb", string(mustProtectionJSON(t, selection)))).Error; err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(t.Context(), session.ID, 7, 9)
	if err != nil || stored.Protection == nil || stored.Protection.SourceLocator != selection.SourceLocator || len(stored.Protection.Choices) != 1 {
		t.Fatal("role selection did not survive PostgreSQL storage", err)
	}
	request := client.ExportExecutionSourceRequest{ExecutionID: session.TransferExecutionID, RequestDigest: session.ExecutionRequestDigest}
	for _, tenant := range []uint{7, 8} {
		got, err := store.GetExecutionSource(t.Context(), session.ID, tenant, request)
		if err != nil || (got != nil) != (tenant == 7) {
			t.Fatalf("tenant=%d lookup err=%v", tenant, err)
		}
	}
	session.Status = StatusFailed
	if err := store.UpdateStatus(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetExecutionSource(t.Context(), session.ID, 7, request); err != nil || got != nil {
		t.Fatal("final session accepted")
	}
}

func mustProtectionJSON(t *testing.T, selection *client.TransferSourceProtectionSelection) []byte {
	t.Helper()
	raw, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
