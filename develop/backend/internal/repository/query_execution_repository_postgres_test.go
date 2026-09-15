package repository

import (
	"context"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestQueryExecutionQueueAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := commonExecution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}

	createdAt := time.Unix(1, 0).UTC()
	for index, source := range []string{commonExecution.ModuleDevelop, commonExecution.ModuleOrchestrator} {
		execution := &commonExecution.TaskExecution{
			TenantID: int(time.Now().UnixNano()) + index, ExecutionID: uuid.NewString(),
			Module: commonExecution.ModuleDevelop, TaskType: commonExecution.TaskTypeQuery, Source: source,
			Status: commonExecution.ExecutionStatusPending, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
			TriggerType: commonExecution.TriggerTypeManual, MaxAttempts: 1,
			CreatedAt: createdAt.Add(time.Duration(index) * time.Second), UpdatedAt: createdAt,
		}
		if err := tx.Create(execution).Error; err != nil {
			t.Fatal(err)
		}
	}
	repository := NewQueryExecutionRepository(tx)
	for _, wantSource := range []string{commonExecution.ModuleDevelop, commonExecution.ModuleOrchestrator} {
		claimed, lease, err := repository.ClaimNext(context.Background(), "postgres-query-supervisor", time.Now().UTC(), time.Minute, nil)
		if err != nil || claimed == nil || lease == nil {
			t.Fatalf("claim = %#v %#v, %v", claimed, lease, err)
		}
		if claimed.Source != wantSource || claimed.Attempt != 1 || claimed.LeaseToken == nil {
			t.Fatalf("claimed execution = %#v", claimed)
		}
		if err := repository.CompleteWithLease(context.Background(), claimed, *lease, commonExecution.ExecutionStatusSuccess, time.Now().UTC(), nil); err != nil {
			t.Fatal(err)
		}
	}

	for index, engineID := range []int{9, 10} {
		execution := &commonExecution.TaskExecution{
			TenantID: int(time.Now().UnixNano()) + index, ExecutionID: uuid.NewString(),
			Module: commonExecution.ModuleDevelop, TaskType: commonExecution.TaskTypeQuery,
			Source: commonExecution.ModuleDevelop, Status: commonExecution.ExecutionStatusPending,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
			ExecutionConfig: commonModels.JSONMap{"engine_id": engineID}, MaxAttempts: 1,
			CreatedAt: createdAt.Add(time.Duration(index+2) * time.Second), UpdatedAt: createdAt,
		}
		if err := tx.Create(execution).Error; err != nil {
			t.Fatal(err)
		}
	}
	claimed, lease, err := repository.ClaimNext(
		context.Background(), "postgres-query-supervisor", time.Now().UTC(), time.Minute, []uint{9},
	)
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("claim excluding engine 9 = %#v %#v, %v", claimed, lease, err)
	}
	engineID, ok := claimed.ExecutionConfig.GetInt("engine_id")
	if !ok || engineID != 10 {
		t.Fatalf("claimed engine_id = %d, %t; want 10", engineID, ok)
	}
	if err := repository.CompleteWithLease(context.Background(), claimed, *lease, commonExecution.ExecutionStatusSuccess, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}

	startedAt := time.Unix(3, 0).UTC()
	unleased := &commonExecution.TaskExecution{
		TenantID: int(time.Now().UnixNano()), ExecutionID: uuid.NewString(),
		Module: commonExecution.ModuleDevelop, TaskType: commonExecution.TaskTypeQuery, Source: commonExecution.ModuleDevelop,
		Status: commonExecution.ExecutionStatusRunning, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType: commonExecution.TriggerTypeManual, StartedAt: &startedAt,
		CreatedAt: startedAt, UpdatedAt: startedAt,
	}
	if err := tx.Create(unleased).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := repository.RecoverUnleased(context.Background(), time.Now().UTC(), 1); err != nil || count != 1 {
		t.Fatalf("recover unleased = %d, %v", count, err)
	}
	var stored commonExecution.TaskExecution
	if err := tx.Where("execution_id = ?", unleased.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != commonExecution.ExecutionStatusFailed || stored.ErrorDetails["code"] != "develop.query.lease_missing" {
		t.Fatalf("recovered execution = %#v", stored)
	}
}
