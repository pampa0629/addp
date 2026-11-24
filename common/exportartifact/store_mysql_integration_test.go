package exportartifact

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestExportSessionStoreAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_EXPORT_STORE") != "1" {
		t.Skip("requires the platform-export-store disposable MySQL gate")
	}
	dialect, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), DBName: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const table = "manager.export_sessions"
	if err := EnsureStore(db, "develop.export_sessions"); err == nil {
		t.Fatal("wrong owner accepted")
	}
	if err := EnsureStore(db, table); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	store := NewGormStore(db, table)
	session := &Session{TenantID: 7, UserID: 9, SourceRef: "orders", Format: "csv", FileName: "orders.csv",
		Status: StatusPending, TransferExecutionID: uuid.NewString(), ExecutionRequestDigest: strings.Repeat("a", 64),
		Protection: &client.TransferSourceProtectionSelection{SourceLocator: "source"}, ArtifactManifest: models.JSONMap{"file": "orders.csv"}}
	if err := store.Create(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, table); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, session.ID, 7, 9)
	if err != nil || got == nil || got.Protection == nil || got.Protection.SourceLocator != "source" || got.ArtifactManifest["file"] != "orders.csv" {
		t.Fatal("protected session did not round trip", err)
	}
	for _, scope := range [][2]uint{{8, 9}, {7, 10}} {
		if got, err := store.Get(ctx, session.ID, scope[0], scope[1]); err != nil || got != nil {
			t.Fatal("wrong owner scope", scope, err)
		}
	}
	req := client.ExportExecutionSourceRequest{ExecutionID: session.TransferExecutionID, RequestDigest: session.ExecutionRequestDigest}
	if got, err := store.GetExecutionSource(ctx, session.ID, 7, req); err != nil || got == nil {
		t.Fatal("missing execution binding", err)
	}
	req.RequestDigest = strings.ToUpper(req.RequestDigest)
	if got, err := store.GetExecutionSource(ctx, session.ID, 7, req); err != nil || got != nil {
		t.Fatal("inexact digest accepted", err)
	}
	session.Status = StatusSuccess
	session.ArtifactManifest = models.JSONMap{"file": "final.csv"}
	if err := store.UpdateStatus(ctx, session); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Table(table).Where("id = ?", session.ID).Update("updated_at", now.Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	fresh := &Session{TenantID: 8, UserID: 9, Status: StatusSuccess, TransferExecutionID: uuid.NewString()}
	if err := store.Create(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	retention := NormalizeCleanupOptions(CleanupOptions{})
	expired, err := store.ListExpiredFinalSessions(ctx, now.Add(-retention.SuccessRetention), now.Add(-retention.FailedRetention), 10)
	if err != nil || len(expired) != 1 || expired[0].ID != session.ID || expired[0].ArtifactManifest["file"] != "final.csv" {
		t.Fatal("expiry or manifest mismatch", err)
	}
	if err := store.Delete(ctx, session.ID, 7, 9); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(ctx, session.ID, 7, 9); err != nil || got != nil {
		t.Fatal("expired session retained", err)
	}
	if got, err := store.Get(ctx, fresh.ID, 8, 9); err != nil || got == nil {
		t.Fatal("unexpired session lost", err)
	}
}
