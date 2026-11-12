package repository

import (
	"os"
	"testing"

	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresCDCTaskList(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard PostgreSQL gate")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.TransferTask{}); err != nil {
		t.Fatal(err)
	}
	ids := []uint{}
	t.Cleanup(func() {
		if len(ids) > 0 {
			if err := db.Unscoped().Where("id IN ?", ids).Delete(&models.TransferTask{}).Error; err != nil {
				t.Error(err)
			}
		}
	})
	for i := 0; i < 6; i++ {
		tenant := uint(991231)
		boundary, detection := "continuous", "cdc"
		if i == 3 {
			tenant++
		}
		if i == 4 {
			detection = "kafka_offset"
		}
		if i == 5 {
			boundary = "bounded"
			detection = "watermark"
		}
		task := models.TransferTask{TenantID: tenant, Name: t.Name(), TaskType: "sync", Config: models.JSONMap{"runtime": map[string]interface{}{"boundary": boundary}, "load": map[string]interface{}{"change_detection": map[string]interface{}{"type": detection}}}}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}
	repo := NewTaskRepository(db)
	seen := map[uint]bool{}
	for page := 1; page <= 3; page++ {
		rows, total, err := repo.List(991231, map[string]interface{}{"runtime_boundary": "continuous", "change_detection": "cdc"}, page, 1)
		if err != nil || total != 3 || len(rows) != 1 || seen[rows[0].ID] {
			t.Fatalf("page=%d total=%d rows=%v err=%v", page, total, rows, err)
		}
		seen[rows[0].ID] = true
	}
}
