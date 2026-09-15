package worker

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/addp/develop/backend/internal/service"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQueryExecutionSupervisorClaimsAndConvergesInvalidQuery(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	execution := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: uuid.NewString(), Module: commonExecution.ModuleDevelop,
		TaskType: commonExecution.TaskTypeQuery, Source: "unsupported", Status: commonExecution.ExecutionStatusPending,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(execution).Error; err != nil {
		t.Fatal(err)
	}
	queries := repository.NewQueryExecutionRepository(db)
	executor := service.NewDevExecutor(nil, commonExecution.NewTaskExecutionRepository(db), nil, nil, nil,
		service.NewSQLEngineService(&config.Config{}, nil, nil), nil, nil, 100)
	queryService, err := service.NewQueryExecutionService(executor, queries)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewQueryExecutionSupervisor(queries, queryService, QueryExecutionSupervisorConfig{
		InstanceID: "backend-1", Concurrency: 1, PerEngineConcurrency: 1, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: 5 * time.Millisecond,
		IdleMaxInterval: 20 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		supervisor.Run(ctx, func() bool { return true })
	}()
	supervisor.Notify()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var stored commonExecution.TaskExecution
		if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status == commonExecution.ExecutionStatusFailed {
			cancel()
			<-done
			if stored.Attempt != 1 || stored.LeaseToken != nil || stored.ErrorDetails["code"] != "develop.query.source_invalid" {
				t.Fatalf("converged execution = %#v", stored)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("query execution did not converge")
}

func TestQueryExecutionSupervisorTracksSaturatedEngines(t *testing.T) {
	supervisor := &QueryExecutionSupervisor{
		config:        QueryExecutionSupervisorConfig{PerEngineConcurrency: 5},
		activeEngines: make(map[uint]int),
	}
	for count := 0; count < 5; count++ {
		supervisor.reserveEngine(9)
	}
	supervisor.reserveEngine(10)
	saturated := supervisor.saturatedEngineIDs()
	if len(saturated) != 1 || saturated[0] != 9 {
		t.Fatalf("saturated engines = %v, want [9]", saturated)
	}
	supervisor.releaseEngine(9)
	if saturated = supervisor.saturatedEngineIDs(); len(saturated) != 0 {
		t.Fatalf("saturated engines after release = %v, want none", saturated)
	}
}
