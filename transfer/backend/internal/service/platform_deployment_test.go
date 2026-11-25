package service

import (
	"os"
	"testing"

	"github.com/addp/common/infra/database"
	"github.com/addp/transfer/internal/repository"
)

func TestInstalledTransferApproval(t *testing.T) {
	if os.Getenv("ADDP_TEST_TRANSFER_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Transfer deployment")
	}
	db, err := repository.OpenDatabase(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "transfer"})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	svc, task := seedStoredApprovalTask(t, db)
	checkApprovalConsumption(t, db, svc, task)
}
