package service

import (
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

func TestCatalogRootReconcilerCreatesRootForActiveCatalogEngine(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)

	tenantID := uint(1)
	resource := &commonModels.Engine{
		ID:             9,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
	}

	reconciler := NewCatalogRootReconciler(db)
	if !reconciler.Reconcile(resource) {
		t.Fatal("Reconcile() = false, want true")
	}

	var root models.MetaNode
	if err := db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL AND full_name = ''", tenantID, resource.ID).
		First(&root).Error; err != nil {
		t.Fatalf("query root node: %v", err)
	}
	if root.Name != resource.Name {
		t.Fatalf("root name = %q, want %q", root.Name, resource.Name)
	}
}

func TestCatalogRootReconcilerSkipsInactiveEngine(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)

	tenantID := uint(1)
	resource := &commonModels.Engine{
		ID:             9,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "disabled",
	}

	reconciler := NewCatalogRootReconciler(db)
	if reconciler.Reconcile(resource) {
		t.Fatal("Reconcile() = true, want false")
	}

	var count int64
	if err := db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND engine_id = ?", tenantID, resource.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count root nodes: %v", err)
	}
	if count != 0 {
		t.Fatalf("root node count = %d, want 0", count)
	}
}
