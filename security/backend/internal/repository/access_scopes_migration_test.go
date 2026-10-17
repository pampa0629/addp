package repository

import (
	"reflect"
	"testing"
	"time"

	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAccessScopeMigrationPreservesOriginalGrantBoundary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS security").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	assertAccessScopeMigration(t, db)
}

func assertAccessScopeMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	row := models.ProtectionAccessRequest{ID: uuid.NewString(), TenantID: 77, AssessmentID: uuid.NewString(), AssessmentRevision: 3, Scopes: []models.ProtectionAccessScope{{ConsumerOwner: "manager", Action: "preview"}}, SubjectType: "user", SubjectID: "41", SubjectDisplayName: "原申请人", RequestedExpiresAt: now.Add(time.Hour), Rationale: "原批准范围", State: "pending", Version: 4, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP INDEX IF EXISTS security.uq_security_pending_access_request",
		"ALTER TABLE security.protection_access_requests DROP COLUMN scopes",
		"ALTER TABLE security.protection_access_requests ADD COLUMN consumer_owner TEXT NOT NULL DEFAULT 'manager'",
		"ALTER TABLE security.protection_access_requests ADD COLUMN action TEXT NOT NULL DEFAULT 'preview'",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var loaded models.ProtectionAccessRequest
	if err := db.First(&loaded, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Scopes, row.Scopes) || loaded.Version != row.Version || loaded.State != row.State || !loaded.RequestedExpiresAt.Equal(row.RequestedExpiresAt) {
		t.Fatalf("old approval boundary changed: %+v", loaded)
	}
	for _, column := range []string{"consumer_owner", "action"} {
		exists, err := accessRequestColumnExists(db, column)
		if err != nil || exists {
			t.Fatalf("legacy runtime column remains: %s %v", column, err)
		}
	}
	if err := db.Delete(&loaded).Error; err != nil {
		t.Fatal(err)
	}
}
