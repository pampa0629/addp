package service

import (
	"errors"
	"testing"

	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestResourceRepo(t *testing.T) *repository.ResourceRepository {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	if err := db.Exec(`ATTACH DATABASE ':memory:' AS system`).Error; err != nil {
		t.Fatalf("failed to attach system schema: %v", err)
	}

	createTable := `
	CREATE TABLE system.engines (
		id INTEGER PRIMARY KEY,
		name TEXT,
		resource_type TEXT,
		connection_info TEXT,
		description TEXT,
		created_by INTEGER,
		tenant_id INTEGER,
		is_active BOOLEAN
	);`
	if err := db.Exec(createTable).Error; err != nil {
		t.Fatalf("failed to create engines table: %v", err)
	}

	seed := []struct {
		id       int
		name     string
		tenantID interface{}
		isActive bool
		resType  string
	}{
		{id: 1, name: "tenant-one-db", tenantID: 1, isActive: true, resType: "postgresql"},
		{id: 2, name: "tenant-two-db", tenantID: 2, isActive: true, resType: "postgresql"},
		{id: 3, name: "global-db", tenantID: nil, isActive: true, resType: "postgresql"},
		{id: 4, name: "inactive-db", tenantID: 1, isActive: false, resType: "postgresql"},
	}

	for _, item := range seed {
		if err := db.Exec(
			`INSERT INTO system.engines (id, name, resource_type, connection_info, tenant_id, is_active) 
			 VALUES (?, ?, ?, '{}', ?, ?)`,
			item.id, item.name, item.resType, item.tenantID, item.isActive,
		).Error; err != nil {
			t.Fatalf("failed to seed resource %d: %v", item.id, err)
		}
	}

	return repository.NewResourceRepository(db)
}

func TestListExplorerResourcesTenantFiltering(t *testing.T) {
	resourceRepo := setupTestResourceRepo(t)
	service := NewMetadataService(nil, resourceRepo, nil, nil, nil, "")

	// 无租户上下文（超级管理员）应看到所有激活资源（含租户为空）
	resourcesAll, err := service.ListExplorerResources(nil)
	if err != nil {
		t.Fatalf("ListExplorerResources(nil) returned error: %v", err)
	}
	if got, want := len(resourcesAll), 3; got != want {
		t.Fatalf("ListExplorerResources(nil) length = %d, want %d", got, want)
	}

	tenantOne := uint(1)
	resourcesTenantOne, err := service.ListExplorerResources(&tenantOne)
	if err != nil {
		t.Fatalf("ListExplorerResources(tenant=1) returned error: %v", err)
	}
	if got, want := len(resourcesTenantOne), 1; got != want {
		t.Fatalf("ListExplorerResources(tenant=1) length = %d, want %d", got, want)
	}
	if resourcesTenantOne[0].Name != "tenant-one-db" {
		t.Fatalf("ListExplorerResources(tenant=1)[0] = %s, want tenant-one-db", resourcesTenantOne[0].Name)
	}

	tenantTwo := uint(2)
	resourcesTenantTwo, err := service.ListExplorerResources(&tenantTwo)
	if err != nil {
		t.Fatalf("ListExplorerResources(tenant=2) returned error: %v", err)
	}
	if got, want := len(resourcesTenantTwo), 1; got != want {
		t.Fatalf("ListExplorerResources(tenant=2) length = %d, want %d", got, want)
	}
	if resourcesTenantTwo[0].Name != "tenant-two-db" {
		t.Fatalf("ListExplorerResources(tenant=2)[0] = %s, want tenant-two-db", resourcesTenantTwo[0].Name)
	}

	tenantThree := uint(3)
	resourcesTenantThree, err := service.ListExplorerResources(&tenantThree)
	if err != nil {
		t.Fatalf("ListExplorerResources(tenant=3) returned error: %v", err)
	}
	if got, want := len(resourcesTenantThree), 0; got != want {
		t.Fatalf("ListExplorerResources(tenant=3) length = %d, want %d", got, want)
	}
}

func TestGetResourceTreeDeniedForOtherTenant(t *testing.T) {
	resourceRepo := setupTestResourceRepo(t)
	service := NewMetadataService(nil, resourceRepo, nil, nil, nil, "")

	tenantOne := uint(1)
	_, err := service.GetResourceTree(2, &tenantOne)
	if !errors.Is(err, ErrResourceAccessDenied) {
		t.Fatalf("GetResourceTree should deny cross-tenant access, got err=%v", err)
	}
}
