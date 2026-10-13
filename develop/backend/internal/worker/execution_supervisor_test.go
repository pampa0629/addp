package worker

import (
	"context"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"sync"
	"testing"
	"time"
)

func TestExecutionSupervisorBudgetsAreIndependentAndShrinkDoesNotCancel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	queue := repository.NewExecutionRepository(db)
	blocked := &blockedQueryService{queries: queue, releases: map[string]chan struct{}{}, started: make(chan string, 20)}
	kinds := []string{"query", "workflow", "script"}
	for _, kind := range kinds {
		for index := 0; index < 4; index++ {
			id := uuid.NewString()
			blocked.releases[id] = make(chan struct{})
			if err := db.Create(&commonExecution.TaskExecution{TenantID: 7, ExecutionID: id, Module: "develop", TaskType: kind, Source: "develop", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: commonModels.JSONMap{"engine_id": 9}, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	limits := models.ExecutionConcurrency{Query: 3, QueryPerEngine: 2, Workflow: 2, Notebook: 1}
	supervisor, err := NewExecutionSupervisor(queue, blocked, ExecutionSupervisorConfig{InstanceID: "mixed", ResolveConcurrency: func(context.Context) (models.ExecutionConcurrency, error) { return limits, nil }, LeaseDuration: time.Minute, HeartbeatInterval: time.Second, ClaimInterval: time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var group sync.WaitGroup
	t.Cleanup(func() { cancel(); group.Wait() })
	supervisor.claimAvailable(ctx, &group)
	for _, count := range []struct {
		kind string
		want int
	}{{"query", 2}, {"workflow", 2}, {"script", 1}} {
		if supervisor.ActiveCount(count.kind) != count.want {
			t.Fatalf("%s active=%d want=%d", count.kind, supervisor.ActiveCount(count.kind), count.want)
		}
	}
	limits = models.ExecutionConcurrency{Query: 1, QueryPerEngine: 1, Workflow: 1, Notebook: 1}
	supervisor.claimAvailable(ctx, &group)
	if supervisor.ActiveCount("query") != 2 || supervisor.ActiveCount("workflow") != 2 || supervisor.Capacity("workflow") != 1 {
		t.Fatal("shrink cancelled an active execution or claimed extra work")
	}
	// Release every active execution and wait for the current attempts to close.
	for index := 0; index < 5; index++ {
		select {
		case id := <-blocked.started:
			close(blocked.releases[id])
		case <-time.After(time.Second):
			t.Fatal("claimed task did not start")
		}
	}
	group.Wait()
	for _, kind := range kinds {
		if supervisor.ActiveCount(kind) != 0 {
			t.Fatal("active slot leaked")
		}
	}
}
