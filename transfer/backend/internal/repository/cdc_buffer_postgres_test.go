package repository

import (
	"context"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestIntegrationPostgresCDCBufferObservation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard PostgreSQL gate")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.TransferTask{}, &models.SyncState{}, &models.CaptureResource{}, &models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.OracleCaptureResource{}); err != nil {
		t.Fatal(err)
	}
	task := models.TransferTask{TenantID: 991231, Name: "cdc-buffer-" + uuid.NewString(), TaskType: "sync", Config: models.JSONMap{}}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, model := range []any{&models.CaptureResource{}, &models.SyncState{}} {
			if e := db.Where("task_id = ?", task.ID).Delete(model).Error; e != nil {
				t.Error(e)
			}
		}
		if e := db.Unscoped().Delete(&task).Error; e != nil {
			t.Error(e)
		}
	})
	resource := models.CaptureResource{TaskID: task.ID, TenantID: task.TenantID, Generation: 1, ConnectorName: task.Name, TopicName: task.Name, ConsumerGroup: task.Name, SourceType: models.CaptureSourcePostgreSQL, SourceIdentity: "addp://engine/2/path/public/own?type=table", SourceConnectionFingerprint: "test", SourceEngineID: 2, SourceDatabase: "fixture", SourceSchema: "public", SourceTable: "own", Status: models.CaptureStatusRunning, ResourceVersion: 1}
	if err = db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewCaptureRepository(db)
	ctx := context.Background()
	if value, _, e := repo.BufferCommittedPosition(ctx, &resource); e != nil || value != nil {
		t.Fatalf("no commit: %v %v", value, e)
	}
	state := models.SyncState{TaskID: task.ID, SourceIdentity: resource.SourceIdentity, Partition: "0", PositionType: "kafka_offset", PositionVersion: "v1", Position: models.JSONMap{"type": "kafka_offset", "version": "v1", "partition": "0", "values": map[string]string{"next_offset": "9007199254740993", "initial_snapshot_completed": "true"}}}
	if err = db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	if value, completed, e := repo.BufferCommittedPosition(ctx, &resource); e != nil || !completed || value == nil || *value != 9007199254740993 {
		t.Fatalf("exact commit: %v %v", value, e)
	}
	foreign := resource
	foreign.SourceIdentity = "foreign"
	if value, _, e := repo.BufferCommittedPosition(ctx, &foreign); e != nil || value != nil {
		t.Fatalf("foreign identity: %v %v", value, e)
	}
	observation := models.JSONMap{"status": "unavailable", "recovery_state": "unknown", "sampled_at": "2026-11-12T00:00:00Z", "expires_at": "2026-11-12T00:01:00Z"}
	if err = repo.Update(ctx, resource.ID, 1, map[string]interface{}{"health_observation": models.JSONMap{"state": "failed", "reason": "task_not_running", "failed_since": "2026-11-12T00:00:00Z", "sampled_at": "2026-11-12T00:01:00Z", "expires_at": "2026-11-12T00:01:30Z"}, "buffer_observation": observation, "postgresql_observation": models.JSONMap{"status": "available", "retained_wal_bytes": "9007199254740993", "risk_state": "reserved"}, "delay_observation": models.JSONMap{"status": "available", "connected": true, "delay_seconds": 0.125}}); err != nil {
		t.Fatal(err)
	}
	loaded, e := repo.GetLatest(ctx, task.ID, task.TenantID)
	if e != nil {
		t.Fatal(e)
	}
	summary := models.NewCaptureSummary(loaded)
	if summary.HealthObservation == nil || summary.HealthObservation.State != "failed" || summary.HealthObservation.FailedSince == nil || summary.HealthObservation.SampledAt.Sub(*summary.HealthObservation.FailedSince) != time.Minute {
		t.Fatal("capture failure timing did not persist")
	}
	if summary.PostgreSQLObservation == nil || summary.PostgreSQLObservation.RetainedWALBytes == nil || *summary.PostgreSQLObservation.RetainedWALBytes != "9007199254740993" || summary.DelayObservation == nil || summary.DelayObservation.DelaySeconds == nil || *summary.DelayObservation.DelaySeconds != 0.125 {
		t.Fatal("task source snapshots did not persist precisely")
	}
	if summary.BufferObservation.Status != "unavailable" {
		t.Fatal("snapshot did not persist")
	}
	if err = repo.Update(ctx, resource.ID, 1, map[string]interface{}{"buffer_observation": models.JSONMap{}}); err == nil {
		t.Fatal("old lifecycle CAS accepted")
	}
	provider := models.PostgreSQLCaptureResource{CaptureResourceID: resource.ID, SlotName: task.Name, PublicationName: task.Name + "_pub"}
	if err = db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.First(&provider, "capture_resource_id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if provider.SlotOwned || provider.PublicationOwned {
		t.Fatal("name registration acquired ownership")
	}
	if err = repo.ClaimPostgreSQLResources(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	if err = repo.ClaimPostgreSQLResources(ctx, loaded); err == nil {
		t.Fatal("stale reservation CAS accepted")
	}
	if err = db.First(&provider, "capture_resource_id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !provider.SlotOwned || !provider.PublicationOwned {
		t.Fatal("reservation not persisted atomically")
	}

}
