package execution

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestExecutionAuthorizationFactsMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_EXECUTION_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_EXECUTION_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
		t.Fatalf("reset common schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS common CASCADE").Error })

	if err := EnsureStore(db); err != nil {
		t.Fatalf("EnsureStore: %v", err)
	}
	now := time.Now().UTC()
	plain := &TaskExecution{
		TenantID: 7, ExecutionID: "00000000-0000-0000-0000-000000000001",
		Module: ModuleDevelop, TaskType: TaskTypeWorkflow, Source: ModuleDevelop,
		Status: ExecutionStatusPending, TriggerType: TriggerTypeManual, CreatedAt: now, UpdatedAt: now,
	}
	if err := NewTaskExecutionRepository(db).Create(context.Background(), plain); err != nil {
		t.Fatalf("create execution without authorization: %v", err)
	}
	eventTriggered := &TaskExecution{
		TenantID: 7, ExecutionID: "00000000-0000-0000-0000-000000000005",
		Module: ModuleSecurity, TaskType: TaskTypeSensitiveDataDiscovery, Source: ModuleSecurity,
		Status: ExecutionStatusPending, TriggerType: TriggerTypeEvent, CreatedAt: now, UpdatedAt: now,
	}
	if err := NewTaskExecutionRepository(db).Create(context.Background(), eventTriggered); err != nil {
		t.Fatalf("create event-triggered execution: %v", err)
	}

	principalID := int64(11)
	partial := &TaskExecution{
		TenantID: 7, ExecutionID: "00000000-0000-0000-0000-000000000002",
		Module: ModuleDevelop, TaskType: TaskTypeQuery, Source: ModuleDevelop,
		Status: ExecutionStatusPending, TriggerType: TriggerTypeManual,
		ActorPrincipalID: &principalID, CreatedAt: now, UpdatedAt: now,
	}
	if err := NewTaskExecutionRepository(db).Create(context.Background(), partial); err == nil {
		t.Fatal("partial execution authorization tuple was accepted")
	}

	membershipID, version := int64(13), int64(17)
	lineageOnly := &TaskExecution{
		TenantID: 7, ExecutionID: "00000000-0000-0000-0000-000000000004",
		Module: ModuleDevelop, TaskType: TaskTypeQuery, Source: ModuleOrchestrator,
		Status: ExecutionStatusPending, TriggerType: TriggerTypeManual,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &version, CreatedAt: now, UpdatedAt: now,
	}
	if err := NewTaskExecutionRepository(db).Create(context.Background(), lineageOnly); err != nil {
		t.Fatalf("create execution with authorization lineage: %v", err)
	}

	authorizationID := int64(19)
	expiresAt := now.Add(5 * time.Minute)
	authorized := &TaskExecution{
		TenantID: 7, ExecutionID: "00000000-0000-0000-0000-000000000003",
		Module: ModuleDevelop, TaskType: TaskTypeQuery, Source: ModuleDevelop,
		Status: ExecutionStatusPending, TriggerType: TriggerTypeManual,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &version, ExecutionAuthorizationID: &authorizationID,
		AuthorizationExpiresAt: &expiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := NewTaskExecutionRepository(db).Create(context.Background(), authorized); err != nil {
		t.Fatalf("create authorized execution: %v", err)
	}
}
