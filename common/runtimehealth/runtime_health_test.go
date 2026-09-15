package runtimehealth

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRepositoryPublishesAndStopsOneRuntimeInstance(t *testing.T) {
	db := newRuntimeHealthTestDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)
	heartbeat := &Heartbeat{
		InstanceID: "worker-1", Module: "meta", Role: RoleExecutionWorker,
		RuntimeName: "scan", Capacity: 4, ActiveCount: 2,
		StartedAt: now.Add(-time.Hour), HeartbeatAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := repo.Publish(context.Background(), heartbeat); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	heartbeat.ActiveCount = 3
	heartbeat.HeartbeatAt = now.Add(10 * time.Second)
	heartbeat.ExpiresAt = now.Add(70 * time.Second)
	if err := repo.Publish(context.Background(), heartbeat); err != nil {
		t.Fatalf("Publish update: %v", err)
	}
	items, err := repo.ListSince(context.Background(), now.Add(-time.Minute))
	if err != nil || len(items) != 1 || items[0].ActiveCount != 3 {
		t.Fatalf("ListSince = %#v, %v", items, err)
	}
	stoppedAt := now.Add(20 * time.Second)
	if err := repo.Stop(context.Background(), "worker-1", stoppedAt); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	items, err = repo.ListSince(context.Background(), now.Add(-time.Minute))
	if err != nil || len(items) != 1 || items[0].StoppedAt == nil || items[0].ActiveCount != 0 || !items[0].ExpiresAt.Equal(stoppedAt) {
		t.Fatalf("stopped ListSince = %#v, %v", items, err)
	}
}

func TestReporterAcceptsEmbeddedExecutionSupervisorRole(t *testing.T) {
	repo := NewRepository(newRuntimeHealthTestDB(t))
	reporter, err := NewReporter(repo, ReporterConfig{
		InstanceID: "develop-query-1", Module: "develop", Role: RoleExecutionSupervisor,
		RuntimeName: "query", Capacity: 1, Interval: time.Second, TTL: 3 * time.Second,
		ActiveCount: func() int { return 1 },
	})
	if err != nil || reporter == nil {
		t.Fatalf("NewReporter() = %#v, %v", reporter, err)
	}
	now := time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC)
	reporter.publish(context.Background(), now)
	items, err := repo.ListSince(context.Background(), now.Add(-time.Second))
	if err != nil || len(items) != 1 || items[0].Role != RoleExecutionSupervisor ||
		items[0].Capacity != 1 || items[0].ActiveCount != 1 {
		t.Fatalf("published supervisor heartbeat = %#v, %v", items, err)
	}
}

func TestReporterPublishesLiveCapacityDuringDrain(t *testing.T) {
	repo := NewRepository(newRuntimeHealthTestDB(t))
	capacity := 20
	reporter, err := NewReporter(repo, ReporterConfig{
		InstanceID: "dynamic", Module: "develop", Role: RoleExecutionSupervisor, RuntimeName: "query",
		CurrentCapacity: func() int { return capacity }, ActiveCount: func() int { return 12 },
		Interval: time.Second, TTL: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	reporter.publish(context.Background(), now)
	capacity = 5
	reporter.publish(context.Background(), now.Add(time.Second))
	items, err := repo.ListSince(context.Background(), now.Add(-time.Second))
	if err != nil || len(items) != 1 || items[0].Capacity != 5 || items[0].ActiveCount != 12 {
		t.Fatalf("draining heartbeat=%#v %v", items, err)
	}
}

func newRuntimeHealthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get SQLite database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE common.background_runtime_heartbeats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		instance_id TEXT NOT NULL UNIQUE,
		module TEXT NOT NULL,
		role TEXT NOT NULL,
		runtime_name TEXT NOT NULL,
		capacity INTEGER NOT NULL,
		active_count INTEGER NOT NULL,
		started_at DATETIME NOT NULL,
		heartbeat_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		stopped_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create runtime heartbeat table: %v", err)
	}
	return db
}
