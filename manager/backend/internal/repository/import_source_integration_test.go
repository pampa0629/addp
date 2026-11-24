package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
	verifyManagerImportSource(t, db)
}

func TestIntegrationMySQLManagerImportSourceIsPrivateAndBoundOnce(t *testing.T) {
	verifyManagerImportSource(t, managerMySQLStoreDB(t, "manager", "ADDP_TEST_MYSQL_MANAGER_STORAGE"))
}

func managerMySQLStoreDB(t *testing.T, name, gate string) *gorm.DB {
	t.Helper()
	if os.Getenv(gate) != "1" {
		t.Skip("requires disposable MySQL gate " + gate)
	}
	dialect, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), DBName: name,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return db
}

func verifyManagerImportSource(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migrateImportSources(db); err != nil {
		t.Fatal(err)
	}
	tenant := uint(time.Now().UnixNano()%10000000 + 800000000)
	repo := NewImportSourceRepository(db)
	source := &models.ImportSource{UploadUUID: uuid.NewString(), TenantID: tenant, UploaderID: 31, Files: map[string]string{"fixture.shp": strings.Repeat("a", 64)}, SourceLocator: "addp-infra://object/manager/private/" + uuid.NewString() + ".shp?type=object"}
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
	if err != nil || !facts.Matches(tenant, request) || facts.UploadID != source.UploadUUID || facts.Files["fixture.shp"] != source.Files["fixture.shp"] {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	for _, kind := range []string{"tenant", "uploader", "task", "locator", "locator-case", "locator-space"} {
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
		case "locator-case":
			r.SourceLocator = strings.ToUpper(r.SourceLocator)
		case "locator-space":
			r.SourceLocator += " "
		}
		if _, err := repo.Verify(t.Context(), currentTenant, r); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("accepted %s: %v", kind, err)
		}
	}
}
