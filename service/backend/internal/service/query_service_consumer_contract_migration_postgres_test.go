package service

import (
	"os"
	"testing"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestQueryServiceConsumerContractMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SERVICE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SERVICE_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS service CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("CREATE SCHEMA service").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AutoMigrate(tx); err != nil {
		t.Fatal(err)
	}

	valid := consumerDescriptorTestService()
	valid.ID = 0
	valid.ServiceName = "valid-contract"
	invalid := *valid
	invalid.ID = 0
	invalid.ServiceName = "invalid-contract"
	invalid.DataConfig = models.JSONB{}
	if err := tx.Create(valid).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&invalid).Error; err != nil {
		t.Fatal(err)
	}

	repo := repository.NewQueryServiceRepository(tx)
	migrated, err := repo.MigrateInvalidQueryServiceConsumerContracts(ValidateQueryConsumerContract)
	if err != nil {
		t.Fatal(err)
	}
	if migrated != 1 {
		t.Fatalf("migrated = %d, want 1", migrated)
	}
	for id, want := range map[uint]string{valid.ID: "active", invalid.ID: "error"} {
		var stored models.QueryService
		if err := tx.First(&stored, id).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status != want {
			t.Fatalf("service %d status = %q, want %q", id, stored.Status, want)
		}
	}
	if migrated, err := repo.MigrateInvalidQueryServiceConsumerContracts(ValidateQueryConsumerContract); err != nil || migrated != 0 {
		t.Fatalf("second migration = %d, error = %v", migrated, err)
	}
}
