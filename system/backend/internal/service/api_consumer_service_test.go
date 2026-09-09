package service

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAPIConsumerServiceEnforcesTenantOwnershipAndExactGrant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:api-consumer-tenant?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE system.api_consumers (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
			description TEXT NOT NULL, rate_limit_per_minute INTEGER NOT NULL, status TEXT NOT NULL,
			created_by_principal_id INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE system.api_consumer_service_grants (
			id INTEGER PRIMARY KEY AUTOINCREMENT, api_consumer_id INTEGER NOT NULL,
			service_type TEXT NOT NULL, service_id INTEGER NOT NULL, created_at DATETIME
		)`,
		`CREATE TABLE system.api_consumer_credentials (
			id INTEGER PRIMARY KEY AUTOINCREMENT, api_consumer_id INTEGER NOT NULL,
			key_prefix TEXT NOT NULL, key_hash TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
			last_used_at DATETIME, expires_at DATETIME, status TEXT NOT NULL,
			created_by_principal_id INTEGER NOT NULL, created_at DATETIME,
			revoked_at DATETIME, revoked_by_principal_id INTEGER
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := NewAPIConsumerService(repository.NewAPIConsumerRepository(db))
	created, err := service.Create(&models.CreateAPIConsumerRequest{
		Name: "warehouse-client",
		ServiceGrants: []models.APIConsumerServiceReference{{
			ServiceType: models.APIConsumerServiceTypeQuery, ServiceID: 71,
		}},
	}, 1, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(created.ID, 2); err == nil {
		t.Fatal("cross-tenant API consumer read succeeded")
	}
	if _, err := service.Update(created.ID, 2, &models.UpdateAPIConsumerRequest{Name: "changed"}); err == nil {
		t.Fatal("cross-tenant API consumer update succeeded")
	}
	if _, err := service.CreateCredential(created.ID, 2, 22, &models.CreateAPIConsumerCredentialRequest{}); err == nil {
		t.Fatal("cross-tenant API consumer credential creation succeeded")
	}

	credential, err := service.CreateCredential(created.ID, 1, 11, &models.CreateAPIConsumerCredentialRequest{Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if credential.PlainTextCredential == "" || credential.KeyHash == "" ||
		credential.KeyPrefix != credential.PlainTextCredential[:12] {
		t.Fatalf("credential = %#v", credential)
	}
	digest := sha256.Sum256([]byte(credential.PlainTextCredential))
	validation, err := service.ValidateCredential(hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid || validation.TenantID != 1 || validation.APIConsumerID != created.ID ||
		len(validation.ServiceGrants) != 1 || validation.ServiceGrants[0].ServiceID != 71 {
		t.Fatalf("validation = %#v", validation)
	}
	if err := service.RevokeCredential(created.ID, credential.ID, 1, 11); err != nil {
		t.Fatal(err)
	}
	validation, err = service.ValidateCredential(hex.EncodeToString(digest[:]))
	if err != nil || validation.Valid {
		t.Fatalf("revoked validation = %#v, %v", validation, err)
	}
}
