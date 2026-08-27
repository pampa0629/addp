package repository

import (
	"context"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresMaterializationGateCopiesParentAuthorizationLineage(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatalf("run quality migrations: %v", err)
	}

	tenantID := time.Now().UnixNano()%100000000 + 930000000
	now := time.Now().UTC()
	principalID, membershipID, authorizationVersion := int64(41), int64(42), int64(43)
	parentExecutionID := uuid.NewString()
	parent := commonExecution.TaskExecution{
		ExecutionID: parentExecutionID, TenantID: int(tenantID), Module: commonExecution.ModuleOrchestrator,
		TaskType: "workflow", Source: commonExecution.ModuleOrchestrator,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status:            commonExecution.ExecutionStatusRunning, TriggerType: commonExecution.TriggerTypeManual,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
		CreatedAt:                  now, UpdatedAt: now,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent execution: %v", err)
	}
	task := models.MaterializationGateTask{
		TenantID: tenantID, Code: "lineage_gate", Name: "Lineage gate", Version: 1,
		MaterializationGroupID: 9, MaterializationGroupVersion: 2,
		TableBindings: []byte(`[{"alias":"orders","logical_table_id":3}]`),
		Assertions:    []byte(`{"schema_version":"addp.quality.materialization-gate/v1","assertions":[{"assertion_key":"f3889a4a-1675-4623-b6e3-773f9125a04d","type":"not_null","severity":"error","params":{"table":"orders","column":"id"}}]}`),
		CreatedBy:     1, UpdatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create materialization gate task: %v", err)
	}
	childExecutionID := uuid.NewString()
	t.Cleanup(func() {
		_ = db.Where("execution_id IN ?", []string{childExecutionID, parentExecutionID}).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.MaterializationGateTask{}).Error
	})

	child := &commonExecution.TaskExecution{
		ExecutionID: childExecutionID, TenantID: int(tenantID), Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeMaterializationGate, Source: commonExecution.ModuleOrchestrator,
		ParentExecutionID: &parentExecutionID, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		MaxAttempts: 3, ExecutionConfig: commonModels.JSONMap{"schema_version": "addp.quality.materialization-gate-execution-config/v1"},
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := NewMaterializationGateRepository(db).CreateExecution(context.Background(), task.ID, tenantID, child); err != nil {
		t.Fatalf("create materialization gate execution: %v", err)
	}

	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", childExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load child execution: %v", err)
	}
	if stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != principalID ||
		stored.ActorTenantMembershipID == nil || *stored.ActorTenantMembershipID != membershipID ||
		stored.IssuedAuthorizationVersion == nil || *stored.IssuedAuthorizationVersion != authorizationVersion {
		t.Fatalf("child authorization lineage = principal %v membership %v version %v",
			stored.ActorPrincipalID, stored.ActorTenantMembershipID, stored.IssuedAuthorizationVersion)
	}
}
