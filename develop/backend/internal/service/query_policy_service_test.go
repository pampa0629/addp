package service

import (
	"context"
	"errors"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newQueryPolicyServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
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
	return db
}

func TestQueryPolicyHotReloadAndExecutionSnapshot(t *testing.T) {
	db := newQueryPolicyServiceTestDB(t)
	policy := NewQueryPolicyService(repository.NewQueryPolicyRepository(db))
	ctx := context.Background()
	defaults, err := policy.Get(ctx, "platform", nil)
	if err != nil || defaults.QueryConcurrency != 20 || defaults.QueryPerEngineConcurrency != 5 {
		t.Fatalf("defaults: %#v %v", defaults, err)
	}
	notifications := 0
	policy.SetOnChange(func() { notifications++ })
	input := UpdateQueryPolicyInput{DefaultQueryTimeout: 60, MaxQueryTimeout: 600, QueryResultLimit: 2, QueryConcurrency: 20, QueryPerEngineConcurrency: 5}
	saved, err := policy.Update(ctx, "platform", nil, input, 1)
	if err != nil {
		t.Fatal(err)
	}
	tenant := uint(7)
	if _, err := policy.Update(ctx, "tenant", &tenant, UpdateQueryPolicyInput{DefaultQueryTimeout: 90}, 2); err != nil {
		t.Fatal(err)
	}
	executor := &DevExecutor{sqlEngine: NewSQLEngineService(&config.Config{}, nil, nil, policy)}
	task := &models.DevTask{DevType: "query", Content: models.DevTaskContent{"query_type": "sql", "query": "SELECT 1"}, ExecutionConfig: models.DevTaskContent{"engine_id": 9}}
	if err := executor.freezeQueryPolicy(ctx, task, tenant); err != nil {
		t.Fatal(err)
	}
	if task.Timeout != 90 || task.QueryResultLimit != 2 {
		t.Fatalf("snapshot: %#v", task)
	}
	// Roundtrip through JSON-shaped execution config, as a persisted queue consumer does.
	record := devTaskExecutionRecordConfig(task, nil)
	record["engine_id"] = 9
	frozen, err := devQueryTaskFromExecution(&commonExecution.TaskExecution{TenantID: 7, ExecutionConfig: record})
	if err != nil {
		t.Fatal(err)
	}
	input.Version = saved.Version
	input.DefaultQueryTimeout, input.MaxQueryTimeout, input.QueryResultLimit = 10, 20, 1
	input.QueryConcurrency, input.QueryPerEngineConcurrency = 8, 3
	if _, err := policy.Update(ctx, "platform", nil, input, 1); err != nil {
		t.Fatal(err)
	}
	// A different service instance must observe changes without any local notification.
	other := NewQueryPolicyService(repository.NewQueryPolicyRepository(db))
	total, perEngine, err := other.ResolveConcurrency(ctx)
	if err != nil || total != 8 || perEngine != 3 || notifications != 2 {
		t.Fatalf("limits=%d/%d notify=%d err=%v", total, perEngine, notifications, err)
	}
	frozenCtx := contextWithQuerySnapshot(ctx, frozen)
	timeout, err := executor.sqlEngine.normalizedTimeoutForTenant(frozenCtx, tenant, frozen.Timeout)
	if err != nil || timeout != 90 || frozen.QueryResultLimit != 2 {
		t.Fatalf("snapshot changed: %d %#v %v", timeout, frozen, err)
	}
	_, ttl, err := executor.sqlEngine.sqlExecutionAuthorizationRequest("SELECT 1", timeout)
	if err != nil || ttl != 120 {
		t.Fatalf("authorization ttl=%d %v", ttl, err)
	}
	newTask := &models.DevTask{}
	if err := executor.freezeQueryPolicy(ctx, newTask, tenant); err != nil {
		t.Fatal(err)
	}
	if newTask.Timeout != 20 || newTask.QueryResultLimit != 1 {
		t.Fatalf("new policy not consumed: %#v", newTask)
	}
	if _, err := policy.Update(ctx, "platform", nil, input, 1); !errors.Is(err, repository.ErrQueryPolicyVersionConflict) {
		t.Fatalf("want version conflict: %v", err)
	}
	if notifications != 2 {
		t.Fatal("failed update notified supervisor")
	}
}

func TestQueryPolicyRejectsTenantLimitOverridesAndInvalidLimits(t *testing.T) {
	policy := NewQueryPolicyService(repository.NewQueryPolicyRepository(newQueryPolicyServiceTestDB(t)))
	tenant := uint(7)
	for _, input := range []UpdateQueryPolicyInput{
		{DefaultQueryTimeout: 30, QueryConcurrency: 100},
		{DefaultQueryTimeout: 30, QueryPerEngineConcurrency: 100},
		{DefaultQueryTimeout: 30, MaxQueryTimeout: 100},
		{DefaultQueryTimeout: 30, QueryResultLimit: 100},
	} {
		if _, err := policy.Update(context.Background(), "tenant", &tenant, input, 1); err == nil {
			t.Fatal("tenant changed platform limit")
		}
	}
	for _, limits := range [][2]int{{0, 5}, {20, 0}, {5, 6}, {-1, 1}} {
		input := UpdateQueryPolicyInput{DefaultQueryTimeout: 30, MaxQueryTimeout: 300, QueryResultLimit: 500, QueryConcurrency: limits[0], QueryPerEngineConcurrency: limits[1]}
		if _, err := policy.Update(context.Background(), "platform", nil, input, 1); err == nil {
			t.Fatalf("invalid limits accepted: %v", limits)
		}
	}
}

func TestQueryPolicyReadFailureDoesNotUseStartupDefaults(t *testing.T) {
	db := newQueryPolicyServiceTestDB(t)
	policy := NewQueryPolicyService(repository.NewQueryPolicyRepository(db))
	sqlDB, _ := db.DB()
	sqlDB.Close()
	executor := &DevExecutor{sqlEngine: NewSQLEngineService(&config.Config{}, nil, nil, policy)}
	if err := executor.freezeQueryPolicy(context.Background(), &models.DevTask{}, 7); err == nil {
		t.Fatal("policy error was ignored")
	}
}
