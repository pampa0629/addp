package service

import (
	"testing"
	"time"

	"github.com/addp/model/internal/models"
)

func TestMaterializedTargetDecommissionRejectsGroupMembershipAndActiveBatch(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	table := models.LogicalTable{
		TenantID: 1, Name: "Obsolete Pair Metric", Code: "obsolete_pair_metric", TableType: "fact",
		Layer: "dws", Status: "approved", Version: 3, CreatedBy: 1,
		Materialization: models.JSONB{
			"target_parent_locator": "addp://engine/2/path/outdoor?type=schema",
			"target_name":           "dws_outdoor_person_pair_metric",
		},
	}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	request := models.MaterializedTargetDecommissionRequest{
		Version: 3, TargetParentLocator: "addp://engine/2/path/outdoor?type=schema",
		TargetName: "dws_outdoor_person_pair_metric",
	}
	if err := validateMaterializedTargetDecommissionState(db, table.ID, 1, request); err != nil {
		t.Fatalf("valid decommission state: %v", err)
	}

	group := models.MaterializationGroup{TenantID: 1, Code: "outdoor", Name: "Outdoor", Version: 1, CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	member := models.MaterializationGroupMember{GroupID: group.ID, TenantID: 1, LogicalTableID: table.ID, Position: 0}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create group member: %v", err)
	}
	requireDomainErrorCode(t, validateMaterializedTargetDecommissionState(db, table.ID, 1, request), "materialized_target_group_member")
	if err := db.Delete(&member).Error; err != nil {
		t.Fatalf("delete group member: %v", err)
	}

	now := time.Now().UTC()
	batch := models.MaterializationBatch{
		ID: "11111111-1111-1111-1111-111111111111", TenantID: 1, LogicalTableID: table.ID,
		LogicalTableVersion: 3, EngineID: 2, TargetParentLocator: request.TargetParentLocator,
		TargetName: request.TargetName, StagingName: "pair__staging", SchemaFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status: models.MaterializationBatchSealed, PrepareExecutionID: "prepare", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&batch).Error; err != nil {
		t.Fatalf("create active batch: %v", err)
	}
	requireDomainErrorCode(t, validateMaterializedTargetDecommissionState(db, table.ID, 1, request), "materialized_target_batch_active")
}

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
	stale := models.MaterializedTargetDecommissionRequest{Version: 3, TargetParentLocator: "addp://engine/2/path/outdoor?type=schema", TargetName: "metric"}
	requireDomainErrorCode(t, validateMaterializedTargetDecommissionState(db, table.ID, 1, stale), "resource_version_conflict")
	mismatch := models.MaterializedTargetDecommissionRequest{Version: 4, TargetParentLocator: "addp://engine/2/path/outdoor?type=schema", TargetName: "other"}
	requireDomainErrorCode(t, validateMaterializedTargetDecommissionState(db, table.ID, 1, mismatch), "materialized_target_confirmation_mismatch")
}
