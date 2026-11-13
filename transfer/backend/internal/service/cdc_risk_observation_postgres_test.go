package service

import (
	"context"
	"os"
	"testing"

	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// No executions, source tables, connectors or logical slots are created.
func TestIntegrationPostgresCDCRiskInventory(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
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
			var remaining int64
			if err := db.Unscoped().Model(&models.TransferTask{}).Where("id IN ?", ids).Count(&remaining).Error; err != nil || remaining != 0 {
				t.Errorf("risk inventory cleanup count=%d err=%v", remaining, err)
			}
		}
	})
	for i := 0; i < 104; i++ {
		config := validPostgreSQLCDCTaskConfig()
		if i == 101 {
			config = validContinuousTaskConfig()
		}
		if i == 102 {
			config = validTableTransferTaskConfig()
		}
		row := models.TransferTask{TenantID: uint(991231 + i%2), Name: t.Name(), TaskType: "sync", DesiredState: models.TaskDesiredStatePaused, Config: config}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, row.ID)
		if i == 103 {
			if err := db.Delete(&row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	svc := NewTaskService(db, nil, nil)
	after := ids[0] - 1
	first, err := svc.ReadCDCRisks(context.Background(), after)
	if err != nil || len(first.Items) != 100 || !first.HasMore || first.NextAfterID != ids[99] {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	for i, row := range first.Items {
		if row.TaskID != ids[i] || row.TenantID != uint(991231+i%2) || row.State != "unknown" {
			t.Fatalf("current inventory lost paused task identity: %#v", row)
		}
	}
	second, err := svc.ReadCDCRisks(context.Background(), first.NextAfterID)
	if err != nil || len(second.Items) != 1 || second.HasMore || second.Items[0].TaskID != ids[100] {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
}
