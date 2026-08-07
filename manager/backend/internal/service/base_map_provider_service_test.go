package service

import (
	"context"
	"testing"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newBaseMapProviderTestService(t *testing.T) *BaseMapProviderService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:manager-map-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE manager.base_map_providers (
		id INTEGER PRIMARY KEY, version INTEGER NOT NULL, scope_type TEXT NOT NULL,
		tenant_id INTEGER, provider TEXT NOT NULL, enabled INTEGER NOT NULL,
		sort_order INTEGER NOT NULL, a_map_key TEXT, a_map_security_js_code TEXT,
		tdt_key TEXT, updated_by INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME
	)`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	return NewBaseMapProviderService(repository.NewBaseMapProviderRepository(db))
}

func TestBaseMapProviderTenantOverridesPlatform(t *testing.T) {
	svc := newBaseMapProviderTestService(t)
	platform, err := svc.Update(context.Background(), models.MapScopePlatform, nil, UpdateBaseMapProviderInput{Provider: models.MapProviderAMap, Enabled: true, SortOrder: 1, AMapKey: "platform-key"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(context.Background(), models.MapScopePlatform, nil, UpdateBaseMapProviderInput{Provider: models.MapProviderOSM, Enabled: true, SortOrder: 0}, 1); err != nil {
		t.Fatal(err)
	}
	tenantID := uint(7)
	if _, err := svc.Update(context.Background(), models.MapScopeTenant, &tenantID, UpdateBaseMapProviderInput{Provider: models.MapProviderAMap, Enabled: true, SortOrder: 1, AMapKey: "tenant-key"}, 2); err != nil {
		t.Fatal(err)
	}
	public, err := svc.ResolvePublic(context.Background(), tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if public["amap_key"] != "tenant-key" {
		t.Fatalf("tenant override = %#v", public)
	}
	if platform.Version != 1 {
		t.Fatalf("platform version = %d", platform.Version)
	}
}
