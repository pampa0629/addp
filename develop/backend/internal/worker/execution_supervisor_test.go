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
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE develop.file_executions (
		execution_id TEXT PRIMARY KEY, tenant_id INTEGER, admission_state TEXT, admission_expires_at DATETIME
	)`).Error; err != nil {
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

func TestExecutionSupervisorUnstoppedPrivateFilesHoldOnlyWorkflowCapacity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.FileExecution{}); err != nil {
		t.Fatal(err)
	}
	queue := repository.NewExecutionRepository(db)
	blocked := &blockedQueryService{queries: queue, releases: map[string]chan struct{}{}, started: make(chan string, 3)}
	blocked.unstopped.Store(1)
	ids := map[string]string{}
	for _, kind := range []string{"query", "workflow"} {
		id := uuid.NewString()
		ids[kind] = id
		blocked.releases[id] = make(chan struct{})
		if err := db.Create(&commonExecution.TaskExecution{ExecutionID: id, TenantID: 7, Module: "develop", Source: "develop", TaskType: kind, Status: "pending", ExecutionBoundary: "bounded", MaxAttempts: 1, TriggerType: "manual", ExecutionConfig: commonModels.JSONMap{"engine_id": 9}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	supervisor, err := NewExecutionSupervisor(queue, blocked, ExecutionSupervisorConfig{InstanceID: "same-instance", ResolveConcurrency: func(context.Context) (models.ExecutionConcurrency, error) {
		return models.ExecutionConcurrency{Query: 1, QueryPerEngine: 1, Workflow: 1, Notebook: 1}, nil
	}, LeaseDuration: time.Minute, HeartbeatInterval: time.Second, ClaimInterval: time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var group sync.WaitGroup
	supervisor.claimAvailable(ctx, &group)
	var waiting commonExecution.TaskExecution
	if err := db.First(&waiting, "execution_id = ?", ids["workflow"]).Error; err != nil {
		cancel()
		group.Wait()
		t.Fatal(err)
	}
	if waiting.Status != "pending" {
		cancel()
		group.Wait()
		t.Fatal("unstopped private work allowed another workflow", waiting.Status)
	}
	select {
	case id := <-blocked.started:
		if id != ids["query"] {
			t.Fatal("unstopped private work allowed another workflow", id)
		}
		close(blocked.releases[id])
	case <-time.After(time.Second):
		t.Fatal("private workflow blocked independent query capacity")
	}
	group.Wait()
	if supervisor.ActiveCount("workflow") != 1 || supervisor.ActiveCount("query") != 0 {
		t.Fatal("pending capacity was dropped or borrowed")
	}
	// The existing recovery round is the only polling owner. A confirmed stop
	// releases the reservation; it does not rewrite or replay the old execution.
	recovered := false
	blocked.recoverPrivate = func(context.Context) error { recovered = true; blocked.unstopped.Store(0); cancel(); return nil }
	supervisor.recoveryLoop(ctx, func() bool { return true })
	if !recovered || supervisor.ActiveCount("workflow") != 0 {
		t.Fatal("private stop was not recovered")
	}
	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()
	supervisor.claimAvailable(ctx2, &group)
	select {
	case id := <-blocked.started:
		if id != ids["workflow"] {
			t.Fatal(id)
		}
		close(blocked.releases[id])
	case <-time.After(time.Second):
		t.Fatal("confirmed stop did not release workflow slot")
	}
	group.Wait()
}

func TestExecutionSupervisorPrivateBatchHandoffCountsSameUUIDOnce(t *testing.T) {
	id := uuid.NewString()
	blocked := &blockedQueryService{unstoppedExecutionID: id}
	blocked.unstopped.Store(1)
	supervisor := &ExecutionSupervisor{active: executionCounters(), service: blocked, activeWorkflows: map[string]struct{}{id: {}}}
	if supervisor.ActiveCount("workflow") != 1 {
		t.Fatal("same running and pending UUID counted twice")
	}
	supervisor.activeMu.Lock()
	delete(supervisor.activeWorkflows, id)
	supervisor.activeMu.Unlock()
	if supervisor.ActiveCount("workflow") != 1 {
		t.Fatal("handoff dropped its original slot")
	}
	blocked.unstopped.Store(0)
	if supervisor.ActiveCount("workflow") != 0 {
		t.Fatal("confirmed stop retained a slot")
	}
}
