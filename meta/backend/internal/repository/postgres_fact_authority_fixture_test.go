package repository

import (
	"os"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// Install the actual scan models and SQL migrations. The authority gate does
// not provision other modules or publish a whole-module readiness revision.
func TestPreparePostgresFactAuthorityFixture(t *testing.T) {
	if os.Getenv("ADDP_TEST_PG_FACT_AUTHORITY") != "1" {
		t.Skip("requires the isolated Meta fact authority gate")
	}
	db, err := bootstrap.OpenDatabase(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("POSTGRES_PORT"), User: "meta_install", Password: os.Getenv("ADDP_PG_META_INSTALL_PASSWORD"),
		DBName: "addp_test", Schema: "meta", SSLMode: "disable",
	}, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}, &models.ScanTask{}, &models.ObjectIndexDeletion{}); err != nil {
			return err
		}
		return applySQLMigrations(tx)
	}); err != nil {
		t.Fatal(err)
	}
}
