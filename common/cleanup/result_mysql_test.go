package cleanup

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMySQLCleanupResultsRetainOriginal(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_CLEANUP_RESULTS") != "1" {
		t.Skip("requires disposable platform cleanup results gate")
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
	t.Cleanup(func() { _ = pool.Close() })
	if err := MigrateResults(db, "service"); err == nil {
		t.Fatal("prepared results in another owner's database")
	}
	if err := MigrateResults(db, "manager"); err != nil {
		t.Fatal(err)
	}
	const table = "manager.cleanup_results"
	row := resultRecord{ExecutionID: uuid.NewString(), TenantID: 3, Attempt: 1, RequestDigest: "abc", Payload: `{"status":"success"}`, CreatedAt: time.Now().UTC()}
	if err := db.Table(table).Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateResults(db, "manager"); err != nil {
		t.Fatal(err)
	}
	err = db.Table(table).Where("execution_id = ?", row.ExecutionID).Update("payload", "changed").Error
	var rejected *mysqlDriver.MySQLError
	if !errors.As(err, &rejected) || rejected.Number != 1644 || rejected.Message != "cleanup result is immutable" {
		t.Fatalf("result update: %v", err)
	}
	var got resultRecord
	if err := db.Table(table).Where("execution_id = ?", row.ExecutionID).Take(&got).Error; err != nil || got.Payload != row.Payload {
		t.Fatalf("original result missing: %v", err)
	}
}
