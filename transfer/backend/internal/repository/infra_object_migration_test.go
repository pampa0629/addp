package repository

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresInfraObjectReferenceUpgrade(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires Transfer PostgreSQL gate")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.TransferTask{}); err != nil {
		t.Fatal(err)
	}
	old := "addp-infra://minio/manager/tenant_7/import/fixture/roads.shp?type=object"
	tenant := uint(time.Now().UnixNano()%10000000 + 860000000)
	hash := "frozen-authorization-hash"
	task := models.TransferTask{Name: "object-upgrade-" + uuid.NewString(), TenantID: tenant, Config: commonModels.JSONMap{"source": map[string]interface{}{"locator": old}, "target": map[string]interface{}{"locator": "addp://engine/7/schema/public/table/roads"}}, AuthorizationDefinitionHash: &hash}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Unscoped().Delete(&task)
	taskID := fmt.Sprint(task.ID)
	id := uuid.NewString()
	frozen := commonExecution.TaskExecution{ExecutionID: id, Module: "transfer", TaskType: "sync", TriggerType: "manual", TenantID: int(tenant), SourceTaskID: &taskID, Status: "running", ExecutionConfig: commonModels.JSONMap{"source": map[string]interface{}{"locator": old}}, Metadata: commonModels.JSONMap{"audit": "frozen-audit"}}
	if err := db.Create(&frozen).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Where("execution_id=?", id).Delete(&commonExecution.TaskExecution{})
	migrate := func() error { return db.Transaction(MigrateInfraObjectReferences) }
	if err := migrate(); err == nil {
		t.Fatal("upgraded active task")
	}
	if err := db.Model(&frozen).Update("status", "success").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrate(); err != nil {
		t.Fatal(err)
	}
	var got models.TransferTask
	if err := db.First(&got, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Config["source"].(map[string]interface{})["locator"] != "addp-infra://object/manager/tenant_7/import/fixture/roads.shp?type=object" || got.AuthorizationDefinitionHash == nil || *got.AuthorizationDefinitionHash != hash {
		t.Fatalf("task changed %+v", got)
	}
	var history commonExecution.TaskExecution
	if err := db.Where("execution_id=?", id).First(&history).Error; err != nil {
		t.Fatal(err)
	}
	if history.ExecutionConfig["source"].(map[string]interface{})["locator"] != old || history.Metadata["audit"] != "frozen-audit" {
		t.Fatalf("history changed %+v", history)
	}
	if err := migrate(); err != nil {
		t.Fatal("upgrade not repeatable", err)
	}
}
