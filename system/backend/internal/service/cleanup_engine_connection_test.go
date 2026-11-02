package service

import (
	"errors"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/secretcipher"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCleanupConnectionReadsOnlyOriginalDeletingTenantEngine(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	key := []byte("addp-dev-encryption-key-2025!!!!")
	secret, err := secretcipher.Encrypt("cleanup-private-secret", key)
	if err != nil {
		t.Fatal("encrypt fixture")
	}
	tenant := uint(7)
	item := models.Engine{ID: 9, Name: "cleanup-owned", TenantID: &tenant, EngineType: "postgresql", LifecycleState: "deleting", ExternalArtifactPolicy: "delete", ConnectionInfo: models.ConnectionInfo{"host": "fixture", "password": secret}}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewEngineService(repository.NewEngineRepository(db), key, nil)
	if _, err := svc.GetForConnection(9, 7); !errors.Is(err, ErrEngineDeleting) {
		t.Fatal("ordinary connection path no longer rejects deleting")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		connection, err := svc.GetForCleanupConnection(t.Context(), tx, 9, 7)
		if err != nil || connection.ConnectionInfo["password"] != "cleanup-private-secret" {
			t.Fatal("approved internal connection did not decrypt")
		}
		if _, err := svc.GetForCleanupConnection(t.Context(), tx, 9, 8); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("foreign Tenant connection issued")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"active", "disabled", "deleted"} {
		if err := db.Model(&models.Engine{}).Where("id=9").Update("lifecycle_state", state).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.GetForCleanupConnection(t.Context(), db, 9, 7); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatal("connection issued for wrong lifecycle")
		}
	}
	if err := db.Model(&models.Engine{}).Where("id=9").Updates(map[string]any{"lifecycle_state": "deleting", "connection_info": models.ConnectionInfo{"password": "not-ciphertext"}}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetForCleanupConnection(t.Context(), db, 9, 7); !errors.Is(err, commonapi.ErrInternalServerError) || strings.Contains(err.Error(), "not-ciphertext") {
		t.Fatal("decryption failure escaped closed server error")
	}
}
