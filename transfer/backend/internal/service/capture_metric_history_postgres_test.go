package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Standard gate only: no source tables, connectors, tasks or slots outside addp_test.
func TestIntegrationPostgresCDCMetricHistory(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err = db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/022_create_capture_metric_history.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = db.Exec(string(migration)).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.AutoMigrate(&models.TransferTask{}, &models.CaptureResource{}, &models.PostgreSQLCaptureResource{}, &models.CaptureMetricHistory{}); err != nil {
		t.Fatal(err)
	}
	task := models.TransferTask{TenantID: 991231, Name: t.Name(), TaskType: "sync", ApplyIdentity: uuid.NewString(), DesiredState: models.TaskDesiredStatePaused, Config: validPostgreSQLCDCTaskConfig()}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	resource := models.CaptureResource{TaskID: task.ID, TenantID: task.TenantID, Generation: 1, ConnectorName: uuid.NewString(), TopicName: uuid.NewString(), ConsumerGroup: uuid.NewString(), SourceType: models.CaptureSourcePostgreSQL, SourceIdentity: "private-source", SourceConnectionFingerprint: "private-fingerprint", SourceEngineID: 2, SourceDatabase: "private-db", SourceSchema: "private-schema", SourceTable: "private-table", Status: models.CaptureStatusRunning, ResourceVersion: 1}
	if err = db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, model := range []any{&models.CaptureMetricHistory{}, &models.CaptureResource{}} {
			if e := db.Where("task_id = ?", task.ID).Delete(model).Error; e != nil {
				t.Error(e)
			}
		}
		if e := db.Unscoped().Delete(&task).Error; e != nil {
			t.Error(e)
		}
		var remaining int64
		db.Model(&models.CaptureMetricHistory{}).Where("task_id = ?", task.ID).Count(&remaining)
		if remaining != 0 {
			t.Error("history cleanup", remaining)
		}
	})
	now := time.Now().UTC().Truncate(time.Minute)
	sampled := now.Add(-2*time.Minute + 5*time.Second)
	buffer := models.JSONMap{"status": "available", "sampled_at": sampled, "expires_at": sampled.Add(30 * time.Second), "earliest_offset": "0", "latest_offset": "9007199254740994", "committed_offset": "1", "lag_offsets": "9007199254740993", "recovery_state": "available", "private": "secret"}
	if err = db.Model(&resource).Update("buffer_observation", buffer).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewCaptureRepository(db)
	ctx := context.Background()
	if err = repo.RecordMetricHistory(ctx, resource.ID, sampled.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = repo.RecordMetricHistory(ctx, resource.ID, sampled.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(db, nil, nil)
	window, _ := NewCaptureMetricWindow(now.Add(-time.Hour), now, now)
	result, err := svc.CaptureMetricHistory(ctx, task.ID, task.TenantID, 1, window, now)
	if err != nil {
		t.Fatal(err)
	}
	point := result.Points[58]
	if point.RecordedAt == nil || !point.RecordedAt.Equal(sampled.Add(time.Second)) || point.ConsumptionState != "paused" || point.BufferObservation == nil || *point.BufferObservation.LagOffsets != "9007199254740993" || result.Points[59].RecordedAt != nil {
		t.Fatalf("minute identity/gap/exactness: %+v", result.Points[58:])
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "private-") {
		t.Fatal("private facts escaped", string(encoded))
	}
	for _, tenant := range []uint{task.TenantID + 1} {
		if _, e := svc.CaptureMetricHistory(ctx, task.ID, tenant, 1, window, now); !errors.Is(e, commonAPI.ErrNotFound) {
			t.Fatal("foreign tenant", e)
		}
	}
	other, err := svc.CaptureMetricHistory(ctx, task.ID, task.TenantID, 2, window, now)
	if err != nil || other.Points[58].RecordedAt != nil {
		t.Fatal("generation mixed", err)
	}
	originalIdentity := task.ApplyIdentity
	if err = db.Model(&task).Update("apply_identity", uuid.NewString()).Error; err != nil {
		t.Fatal(err)
	}
	other, err = svc.CaptureMetricHistory(ctx, task.ID, task.TenantID, 1, window, now)
	if err != nil || other.Points[58].RecordedAt != nil {
		t.Fatal("global task identity mixed", err)
	}
	if err = db.Model(&task).Update("apply_identity", originalIdentity).Error; err != nil {
		t.Fatal(err)
	}
	if err = repo.RecordMetricHistory(ctx, resource.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	stale, err := svc.CaptureMetricHistory(ctx, task.ID, task.TenantID, 1, window, now)
	if err != nil || stale.Points[59].RecordedAt == nil || stale.Points[59].BufferObservation != nil {
		t.Fatal("expired sample reused", err)
	}
	// Expiry maintenance is bounded; query retention does not depend on its backlog.
	expired := make([]models.CaptureMetricHistory, 1001)
	for i := range expired {
		expired[i] = models.CaptureMetricHistory{TenantID: task.TenantID, TaskID: task.ID, ApplyIdentity: originalIdentity, Generation: 1, Minute: now.Add(-8*24*time.Hour - time.Duration(i)*time.Minute), Point: models.JSONMap{}}
	}
	if err = db.CreateInBatches(expired, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err = repo.PruneMetricHistory(ctx, now); err != nil {
		t.Fatal(err)
	}
	var remainingExpired int64
	db.Model(&models.CaptureMetricHistory{}).Where("task_id = ? AND minute < ?", task.ID, now.Add(-models.CaptureMetricRetention)).Count(&remainingExpired)
	if remainingExpired != 1 {
		t.Fatal("expiry batch unbounded", remainingExpired)
	}
	if err = repo.PruneMetricHistory(ctx, now); err != nil {
		t.Fatal(err)
	}
	// Closed generations cannot publish, and retention removes only expired rows.
	if err = db.Model(&resource).Update("status", models.CaptureStatusStopped).Error; err != nil {
		t.Fatal(err)
	}
	if err = repo.RecordMetricHistory(ctx, resource.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.CaptureMetricHistory{}).Where("task_id = ?", task.ID).Count(&count)
	if count != 2 {
		t.Fatal("terminal published", count)
	}
	if err = repo.PruneMetricHistory(ctx, now.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.CaptureMetricHistory{}).Where("task_id = ?", task.ID).Count(&count)
	if count != 0 {
		t.Fatal("expired history retained", count)
	}
	if err = db.Delete(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CaptureMetricHistory(ctx, task.ID, task.TenantID, 1, window, now); !errors.Is(err, commonAPI.ErrNotFound) {
		t.Fatal("deleted task readable", err)
	}
}
