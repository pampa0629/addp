package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

func TestManagerProgressEventsRequireLeaseAndExcludeRuntimeText(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	item := &commonExecution.TaskExecution{ExecutionID: "safe-manager-progress", TenantID: 7, Module: "manager", TaskType: "vector_tile_set_generation", Status: "running", ExecutionBoundary: "bounded", TriggerType: "manual", Progress: 10}
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	ctx := leaseStoredManagerExecutionForServiceTest(t, db, item.ExecutionID, 7)
	repo := commonExecution.NewTaskExecutionRepository(db)
	for _, invalid := range []struct {
		ctx      context.Context
		id       string
		tenant   uint
		progress int
		fields   map[string]interface{}
	}{
		{t.Context(), item.ExecutionID, 7, 60, map[string]interface{}{"progress": 60}},
		{ctx, "other", 7, 60, map[string]interface{}{"progress": 60}},
		{ctx, item.ExecutionID, 8, 60, map[string]interface{}{"progress": 60}},
		{ctx, item.ExecutionID, 7, 101, map[string]interface{}{"progress": 101}},
		{ctx, item.ExecutionID, 7, 60, map[string]interface{}{"progress": "60"}},
	} {
		if err := updateManagerExecutionProgress(invalid.ctx, repo, invalid.tenant, invalid.id, invalid.progress, invalid.fields); err == nil {
			t.Fatal("invalid progress accepted")
		}
	}
	fields := map[string]interface{}{"progress": 60, "current_step": "runtime-private-sentinel", "metadata": commonModels.JSONMap{"runtime_response": "runtime-private-sentinel"}}
	if err := updateManagerExecutionProgress(ctx, repo, 7, item.ExecutionID, 60, fields); err != nil {
		t.Fatal(err)
	}
	var events []commonExecution.Event
	if err := db.Where("execution_id = ?", item.ExecutionID).Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatalf("events: %v %v", events, err)
	}
	payload, _ := json.Marshal(events[0])
	progress, ok := events[0].Counters.GetInt("progress")
	if !ok || progress != 60 || len(events[0].Counters) != 1 || events[0].Kind != "progress" || events[0].StepID != "" || strings.Contains(string(payload), "private-sentinel") {
		t.Fatalf("unsafe progress event: %s", payload)
	}
	const callback = "test:manager-progress-store-failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*commonExecution.Event); ok {
			tx.AddError(errors.New("event store unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove(callback) })
	if err := updateManagerExecutionProgress(ctx, repo, 7, item.ExecutionID, 70, map[string]interface{}{"progress": 70}); err == nil {
		t.Fatal("progress ignored failed event storage")
	}
	var stored commonExecution.TaskExecution
	if err := db.First(&stored, item.ID).Error; err != nil || stored.Progress != 60 {
		t.Fatalf("progress did not roll back: %v", err)
	}
	db.Callback().Create().Remove(callback)
	if err := db.Model(item).Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := updateManagerExecutionProgress(ctx, repo, 7, item.ExecutionID, 80, map[string]interface{}{"progress": 80}); err == nil {
		t.Fatal("expired lease wrote progress")
	}
	var count int64
	if err := db.Model(&commonExecution.Event{}).Where("execution_id = ?", item.ExecutionID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("unexpected late event: %d %v", count, err)
	}
}
