package service

import (
	"testing"

	"github.com/addp/model/internal/models"
)

func TestMaterializedTargetDecommissionRequiresExactVersionAndTarget(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	table := models.LogicalTable{
		TenantID: 1, Name: "Metric", Code: "metric", TableType: "fact", Layer: "dws",
		Status: "approved", Version: 4, CreatedBy: 1,
		Materialization: models.JSONB{"target_parent_locator": "addp://engine/2/path/outdoor?type=schema", "target_name": "metric"},
	}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	stale := models.PhysicalTargetDeleteRequest{Version: 3, TargetParentLocator: "addp://engine/2/path/outdoor?type=schema", TargetName: "metric"}
	requireDomainErrorCode(t, validateMaterializedTargetDecommissionState(db, table.ID, 1, stale), "resource_version_conflict")
	mismatch := models.PhysicalTargetDeleteRequest{Version: 4, TargetParentLocator: "addp://engine/2/path/outdoor?type=schema", TargetName: "other"}
	requireDomainErrorCode(t, validateMaterializedTargetDecommissionState(db, table.ID, 1, mismatch), "materialized_target_confirmation_mismatch")
}
