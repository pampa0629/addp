package repository

import (
	"fmt"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerObjectReferenceUpgrade(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ImportSource{}); err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	tenant := uint(time.Now().UnixNano()%10000000 + 850000000)
	task := tenant
	source := &models.ImportSource{UploadUUID: uuid.NewString(), TenantID: tenant, UploaderID: 31, TransferTaskID: &task, Files: map[string]string{"roads.shp": "unchanged-digest"}, SourceLocator: "addp-infra://minio/manager/tenant_7/import/fixture/roads.shp?type=object"}
	if err := db.Create(source).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Where("upload_uuid=?", source.UploadUUID).Delete(&models.ImportSource{})
	id := uuid.NewString()
	taskID := fmt.Sprint(task)
	frozen := commonExecution.TaskExecution{ExecutionID: id, Module: "transfer", TaskType: "sync", TriggerType: "manual", TenantID: int(tenant), SourceTaskID: &taskID, Status: "running", ExecutionConfig: map[string]interface{}{"locator": source.SourceLocator}, Metadata: map[string]interface{}{"audit": "frozen-audit"}}
	if err := db.Create(&frozen).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Where("execution_id=?", id).Delete(&commonExecution.TaskExecution{})
	if err := migrateImportObjectReferences(db); err == nil {
		t.Fatal("upgraded an active upload")
	}
	if err := db.Model(&frozen).Update("status", "success").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateImportObjectReferences(db); err != nil {
		t.Fatal(err)
	}
	var got models.ImportSource
	if err := db.Where("upload_uuid=?", source.UploadUUID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.SourceLocator != "addp-infra://object/manager/tenant_7/import/fixture/roads.shp?type=object" || got.TenantID != tenant || got.UploaderID != 31 || *got.TransferTaskID != task || got.Files["roads.shp"] != "unchanged-digest" {
		t.Fatalf("changed identity %+v", got)
	}
	var history commonExecution.TaskExecution
	if err := db.Where("execution_id=?", id).First(&history).Error; err != nil {
		t.Fatal(err)
	}
	if history.ExecutionConfig["locator"] != source.SourceLocator || history.Metadata["audit"] != "frozen-audit" {
		t.Fatalf("history changed %+v", history)
	}
	if err := migrateImportObjectReferences(db); err != nil {
		t.Fatal("upgrade not repeatable", err)
	}
}
