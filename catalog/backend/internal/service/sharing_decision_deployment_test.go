package service

import (
	"os"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"gorm.io/gorm"
)

func TestInstalledCatalogSharingDecision(t *testing.T) {
	if os.Getenv("ADDP_TEST_CATALOG_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Catalog deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "catalog"}, "catalog", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	exerciseSharingDecisionActions(t, db)
	t.Run("concurrent_recovery_after_lost_receipt", func(t *testing.T) { exerciseConcurrentReceiptRecovery(t, db) })
}
