package execution

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
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

	membershipID, version, authorizationID := int64(13), int64(17), int64(19)
	expiresAt := now.Add(5 * time.Minute)
	authorized := &TaskExecution{
		TenantID: 7, ExecutionID: "00000000-0000-0000-0000-000000000003",
		Module: ModuleDevelop, TaskType: TaskTypeQuery, Source: ModuleDevelop,
		Status: ExecutionStatusPending, TriggerType: TriggerTypeManual,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &version, ExecutionAuthorizationID: &authorizationID,
		AuthorizationEffects: pq.StringArray{"read"}, AuthorizationExpiresAt: &expiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := NewTaskExecutionRepository(db).Create(context.Background(), authorized); err != nil {
		t.Fatalf("create authorized execution: %v", err)
	}
}
