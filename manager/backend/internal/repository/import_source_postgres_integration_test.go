package repository

import (
	"context"
	"errors"
	"github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestIntegrationPostgresManagerImportSourceIsPrivateAndBoundOnce(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ImportSource{}); err != nil {
		t.Fatal(err)
	}
	tenant := uint(time.Now().UnixNano()%10000000 + 800000000)
	repo := NewImportSourceRepository(db)
	source := &models.ImportSource{UploadUUID: uuid.NewString(), TenantID: tenant, UploaderID: 31, SourceLocator: "addp-infra://minio/manager/private/" + uuid.NewString() + ".shp?type=object"}
	t.Cleanup(func() {
		if err := db.Where("tenant_id = ?", tenant).Delete(&models.ImportSource{}).Error; err != nil {
			t.Error(err)
		}
	})
	if err := repo.Create(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	request := client.ManagerImportSourceRequest{ExecutionID: uuid.NewString(), TaskID: tenant, PrincipalID: 31, SourceLocator: source.SourceLocator}
	if _, err := repo.Verify(t.Context(), tenant, request); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unbound upload: %v", err)
	}
	if err := repo.BindTask(t.Context(), source.UploadUUID, tenant, request.TaskID); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindTask(t.Context(), source.UploadUUID, tenant, request.TaskID+1); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("rebound upload: %v", err)
	}
	facts, err := repo.Verify(t.Context(), tenant, request)
	if err != nil || !facts.Matches(tenant, request) {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	for _, kind := range []string{"tenant", "uploader", "task", "locator"} {
		r := request
		currentTenant := tenant
		switch kind {
		case "tenant":
			currentTenant++
		case "uploader":
			r.PrincipalID++
		case "task":
			r.TaskID++
		case "locator":
			r.SourceLocator += "other"
		}
		if _, err := repo.Verify(t.Context(), currentTenant, r); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("accepted %s: %v", kind, err)
		}
	}
}
