package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/exportartifact"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestExportSessionScopeAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS develop CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("CREATE SCHEMA develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := exportartifact.EnsureStore(tx, "develop.export_sessions"); err != nil {
		t.Fatal(err)
	}

	nonce := time.Now().UnixNano()
	tenantID := uint(nonce%1_000_000_000 + 10_000)
	session := &exportartifact.Session{
		TenantID:            tenantID,
		UserID:              tenantID + 1,
		SourceRef:           fmt.Sprintf("develop-query-execution-%d", nonce),
		Format:              "csv",
		FileName:            "query-result.csv",
		TargetParentLocator: "addp-infra://minio/manager/tenant/export/develop/session?type=prefix",
		TargetLocator:       "addp-infra://minio/manager/tenant/export/develop/session/query-result.csv?type=object",
		TransferExecutionID: fmt.Sprintf("transfer-execution-%d", nonce),
		Status:              exportartifact.StatusPending,
	}
	store := exportartifact.NewGormStore(tx, "develop.export_sessions")
	if err := store.Create(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(context.Background(), session.ID, session.TenantID, session.UserID)
	if err != nil || loaded == nil {
		t.Fatalf("load owning session = %#v, error = %v", loaded, err)
	}
	notOwned, err := store.Get(context.Background(), session.ID, session.TenantID, session.UserID+1)
	if err != nil {
		t.Fatal(err)
	}
	if notOwned != nil {
		t.Fatalf("different user loaded export session: %#v", notOwned)
	}
	if tx.Migrator().HasColumn("develop.export_sessions", "transfer_task_id") {
		t.Fatal("develop.export_sessions must not persist a Transfer task id")
	}
}
