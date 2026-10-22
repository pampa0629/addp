package service

import (
	"os"
	"testing"

	"github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The standard gate validates addp_test ownership. All schema and fixture
// changes below are rolled back, including execution-store initialization.
func TestScanPlanFactsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("run make test-meta-postgres")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() {
		if err := tx.Rollback().Error; err != nil {
			t.Error("rollback Meta scan plan fixture", err)
		}
	}()
	if err := tx.Exec("DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}, &models.ScanTask{}); err != nil {
		t.Fatal(err)
	}
	if err := execution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	exerciseScanPlanFacts(t, tx)
	exerciseProductionScanPlanFacts(t, tx)
	exerciseScanPlanTaskBinding(t, tx)
}
