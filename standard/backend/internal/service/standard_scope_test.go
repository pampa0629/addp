package service

import (
	"errors"
	"testing"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateTenantStandardScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE standard.domains (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, lifecycle_state TEXT NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.domains (id, tenant_id, lifecycle_state) VALUES (10, 7, 'active'), (20, 8, 'active'), (30, 7, 'deleting')`).Error; err != nil {
		t.Fatal(err)
	}
	refs := repository.NewTenantReferenceRepository(db)
	domain10 := int64(10)
	foreignDomain := int64(20)
	deletingDomain := int64(30)

	tests := []struct {
		name          string
		scopeType     string
		ownerDomainID *int64
		want          string
		wantError     error
	}{
		{name: "tenant common", scopeType: models.StandardScopeTenantCommon, want: models.StandardScopeTenantCommon},
		{name: "domain", scopeType: models.StandardScopeDomain, ownerDomainID: &domain10, want: models.StandardScopeDomain},
		{name: "domain missing owner", scopeType: models.StandardScopeDomain, wantError: ErrInvalidStandardScope},
		{name: "tenant common with owner", scopeType: models.StandardScopeTenantCommon, ownerDomainID: &domain10, wantError: ErrInvalidStandardScope},
		{name: "platform rejected", scopeType: models.StandardScopePlatform, wantError: ErrInvalidStandardScope},
		{name: "unknown rejected", scopeType: "global", wantError: ErrInvalidStandardScope},
		{name: "foreign domain rejected", scopeType: models.StandardScopeDomain, ownerDomainID: &foreignDomain, wantError: repository.ErrInvalidTenantReference},
		{name: "deleting domain rejected", scopeType: models.StandardScopeDomain, ownerDomainID: &deletingDomain, wantError: repository.ErrInvalidTenantReference},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateTenantStandardScope(refs, 7, tt.scopeType, tt.ownerDomainID)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if got != tt.want {
				t.Fatalf("scope = %q, want %q", got, tt.want)
			}
		})
	}
}
