package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
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
		service.NewSQLEngineService(&config.Config{}, nil, nil), nil, nil)
	queryService, err := service.NewQueryExecutionService(executor, queries)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewQueryExecutionSupervisor(queries, queryService, QueryExecutionSupervisorConfig{
		InstanceID: "backend-1", ResolveConcurrency: func(context.Context) (int, int, error) { return 1, 1, nil }, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: 5 * time.Millisecond,
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
		activeEngines: make(map[uint]int),
	}
	for count := 0; count < 5; count++ {
		supervisor.reserveEngine(9)
	}
	supervisor.reserveEngine(10)
	saturated := supervisor.saturatedEngineIDs(5)
	if len(saturated) != 1 || saturated[0] != 9 {
		t.Fatalf("saturated engines = %v, want [9]", saturated)
	}
	supervisor.releaseEngine(9)
	if saturated = supervisor.saturatedEngineIDs(5); len(saturated) != 0 {
		t.Fatalf("saturated engines after release = %v, want none", saturated)
	}
}

type blockedQueryService struct {
	queries  *repository.QueryExecutionRepository
	releases map[string]chan struct{}
	started  chan string
}

func (b *blockedQueryService) Execute(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease) error {
	b.started <- execution.ExecutionID
	select {
	case <-b.releases[execution.ExecutionID]:
	case <-ctx.Done():
	}
	return b.queries.CompleteWithLease(context.Background(), execution, lease, commonExecution.ExecutionStatusSuccess, time.Now(), nil)
}

func TestQuerySupervisorReloadsLimitsWithoutRestartOrCancellingRunningQueries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE develop.query_policy (
id INTEGER PRIMARY KEY AUTOINCREMENT, scope_type TEXT NOT NULL, tenant_id INTEGER,
default_query_timeout INTEGER NOT NULL, max_query_timeout INTEGER NOT NULL, query_result_limit INTEGER NOT NULL,
query_concurrency INTEGER NOT NULL DEFAULT 20, query_per_engine_concurrency INTEGER NOT NULL DEFAULT 5,
version INTEGER NOT NULL, updated_by INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME,
UNIQUE(scope_type,tenant_id))`).Error; err != nil {
		t.Fatal(err)
	}
	queries := repository.NewQueryExecutionRepository(db)
	policy := service.NewQueryPolicyService(repository.NewQueryPolicyRepository(db))
	input := service.UpdateQueryPolicyInput{DefaultQueryTimeout: 30, MaxQueryTimeout: 300, QueryResultLimit: 500, QueryConcurrency: 3, QueryPerEngineConcurrency: 2}
	saved, err := policy.Update(context.Background(), "platform", nil, input, 1)
	if err != nil {
		t.Fatal(err)
	}
	blocked := &blockedQueryService{queries: queries, releases: map[string]chan struct{}{}, started: make(chan string, 10)}
	now := time.Now()
	for i, engine := range []int{1, 1, 1, 1, 2, 2, 2, 3} {
		id := fmt.Sprintf("query-%d", i)
		blocked.releases[id] = make(chan struct{})
		execution := &commonExecution.TaskExecution{
			TenantID: 7, ExecutionID: id, Module: "develop", TaskType: "query", Source: "develop", Status: "pending",
			ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: commonModels.JSONMap{"engine_id": engine},
			CreatedAt: now.Add(time.Duration(i) * time.Millisecond), UpdatedAt: now,
		}
		if err := db.Create(execution).Error; err != nil {
			t.Fatal(err)
		}
	}
	supervisor, err := NewQueryExecutionSupervisor(queries, blocked, QueryExecutionSupervisorConfig{
		InstanceID: "reload-test", ResolveConcurrency: policy.ResolveConcurrency,
		LeaseDuration: time.Minute, HeartbeatInterval: 10 * time.Second, ClaimInterval: 5 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); supervisor.Run(ctx, nil) }()
	defer func() { cancel(); <-done }()
	readStarted := func(want string) {
		t.Helper()
		select {
		case got := <-blocked.started:
			if got != want {
				t.Fatalf("started %s want %s", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("not started: %s", want)
		}
	}
	// First two engine-1 queries occupy its limit; engine 2 uses the remaining slot.
	initial := map[string]bool{}
	for i := 0; i < 3; i++ {
		select {
		case id := <-blocked.started:
			initial[id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("initial claims stalled")
		}
	}
	if !initial["query-0"] || !initial["query-1"] || !initial["query-4"] {
		t.Fatalf("initial: %v", initial)
	}
	// No local notification: the running supervisor must discover persisted changes.
	input.Version = saved.Version
	input.QueryConcurrency = 5
	saved, err = policy.Update(context.Background(), "platform", nil, input, 1)
	if err != nil {
		t.Fatal(err)
	}
	next := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-blocked.started:
			next[id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("increase not applied")
		}
	}
	if !next["query-5"] || !next["query-7"] {
		t.Fatalf("expanded: %v", next)
	}
	input.Version = saved.Version
	input.QueryConcurrency = 2
	input.QueryPerEngineConcurrency = 1
	if _, err = policy.Update(context.Background(), "platform", nil, input, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for supervisor.Capacity() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if supervisor.Capacity() != 2 || supervisor.ActiveCount() != 5 {
		t.Fatalf("shrink cancelled running work: capacity=%d active=%d", supervisor.Capacity(), supervisor.ActiveCount())
	}
	// Free four slots. Engine 1 still has one running query, so only engine 2 can start.
	for _, id := range []string{"query-0", "query-4", "query-5", "query-7"} {
		close(blocked.releases[id])
	}
	readStarted("query-6")
	select {
	case id := <-blocked.started:
		t.Fatalf("started above reduced limits: %s", id)
	case <-time.After(30 * time.Millisecond):
	}
}
