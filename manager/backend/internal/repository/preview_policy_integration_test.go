package repository

import (
	"errors"
	"os"
	"testing"

	"github.com/addp/manager/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationMySQLManagerPreviewPolicies(t *testing.T) {
	verifyPreviewPolicies(t, managerMySQLStoreDB(t, "manager", "ADDP_TEST_MYSQL_MANAGER_STORAGE"))
}

func TestIntegrationPostgresManagerPreviewPolicies(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	verifyPreviewPolicies(t, db)
}

func verifyPreviewPolicies(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := preparePreviewPolicies(db); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() {
		if err := tx.Rollback().Error; err != nil {
			t.Error(err)
		}
	})
	ctx := t.Context()
	policy := NewQuickViewPolicyRepository(tx)
	current, err := policy.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current == nil {
		current = &models.QuickViewPolicy{DirectFlatGeobufMaxRows: 100, UpdatedBy: 8}
		if err := policy.Save(ctx, current, 0); err != nil {
			t.Fatal(err)
		}
	}
	old := current.Version
	current.DirectFlatGeobufMaxRows = 321
	if err := policy.Save(ctx, current, old); err != nil {
		t.Fatal(err)
	}
	if err := policy.Save(ctx, current, old); !errors.Is(err, ErrQuickViewPolicyVersionConflict) {
		t.Fatalf("stale preview policy update: %v", err)
	}
	stored, err := policy.Get(ctx)
	if err != nil || stored == nil || stored.DirectFlatGeobufMaxRows != 321 || stored.Version != old+1 {
		t.Fatalf("stored preview policy: %+v, %v", stored, err)
	}
	maps := NewBaseMapProviderRepository(tx)
	tenant := uint(897212)
	const provider = "maintenance-fixture"
	base := models.BaseMapProvider{Enabled: true, ScopeType: models.MapScopePlatform, Provider: provider, UpdatedBy: 8}
	if err := maps.Save(ctx, &base, 0); err != nil {
		t.Fatal(err)
	}
	override := models.BaseMapProvider{ScopeType: models.MapScopeTenant, TenantID: &tenant, Provider: provider, UpdatedBy: 9}
	if err := maps.Save(ctx, &override, 0); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id   uint
		want bool
	}{{tenant, false}, {tenant + 1, true}} {
		items, err := maps.GetEffective(ctx, test.id)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range items {
			if item.Provider == provider {
				found = true
			}
		}
		if found != test.want {
			t.Fatalf("effective map in tenant %d: found=%v", test.id, found)
		}
	}
}
