package service

import (
	"os"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/schema"
	"github.com/addp/orchestrator/internal/repository"
	"gorm.io/gorm"
)

func TestMySQLOrchestratorExecutionReliability(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_ORCHESTRATOR") != "1" {
		t.Skip("requires the isolated Orchestrator MySQL gate")
	}
	commonDB, err := bootstrap.OpenPlatformInstallation(database.Config{}, "common")
	if err != nil {
		t.Fatal(err)
	}
	commonPool, err := commonDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer commonPool.Close()
	if err := schema.InitializeCommon(commonDB); err != nil {
		t.Fatal(err)
	}

	owner, err := bootstrap.OpenPlatformInstallation(database.Config{}, "orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	deploymentPool, err := owner.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer deploymentPool.Close()
	for range 2 {
		if err := PrepareSchema(owner); err != nil {
			t.Fatal(err)
		}
	}

	db, err := bootstrap.OpenPlatformRuntime(database.Config{}, "orchestrator", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := schema.Require(db, "orchestrator", repository.StartupSchemaVersion); err != nil {
		t.Fatal(err)
	}
	checkOrchestratorExecutionReliability(t, db)
}
