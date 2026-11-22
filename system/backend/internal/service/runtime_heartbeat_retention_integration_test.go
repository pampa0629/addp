package service

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/runtimehealth"
	"gorm.io/gorm"
)

func TestSystemRuntimeHeartbeatRetention(t *testing.T) {
	if os.Getenv("ADDP_TEST_HEARTBEAT_RETENTION") != "1" {
		t.Skip("requires the isolated Meta deployment gate")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: "127.0.0.1", Port: os.Getenv("POSTGRES_PORT"), DBName: "addp_test", Schema: "common", SSLMode: "disable"}, "common", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	publish := func(id, module string, expiry time.Time) {
		t.Helper()
		row := runtimehealth.Heartbeat{InstanceID: id, Module: module, Role: runtimehealth.RoleExecutionWorker, RuntimeName: "gate", Capacity: 1, StartedAt: expiry.Add(-time.Minute), HeartbeatAt: expiry.Add(-time.Second), ExpiresAt: expiry}
		if err := runtimehealth.NewRepository(db).Publish(t.Context(), &row); err != nil {
			t.Fatal(err)
		}
	}
	count := func(id string) int64 {
		t.Helper()
		var n int64
		if err := db.Model(&runtimehealth.Heartbeat{}).Where("instance_id = ?", id).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	cutoff := now.Add(-7 * 24 * time.Hour)
	publish("retention-old", "meta", cutoff.Add(-time.Second))
	publish("retention-edge", "transfer", cutoff)
	publish("retention-new", "develop", cutoff.Add(time.Hour))
	if err := runtimehealth.NewRepository(db).PruneExpired(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if count("retention-old") != 0 || count("retention-edge") != 1 || count("retention-new") != 1 {
		t.Fatal("unexpected rows at the seven-day cutoff")
	}
	publish("retention-system", "transfer", cutoff.Add(-time.Hour))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunExecutionEventMaintenance(ctx, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	defer func() { cancel(); <-done }()
	for deadline := time.Now().Add(10 * time.Second); count("retention-system") != 0; {
		if time.Now().After(deadline) {
			t.Fatal("System maintenance did not remove the expired heartbeat")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if count("retention-new") != 1 {
		t.Fatal("System maintenance removed a retained heartbeat")
	}
	t.Log("seven-day boundary retained; System cleaned another module's expired row")
}
