package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/transfer/internal/models"
)

func TestCaptureRepositoryReusesGenerationAndRejectsStoppedRestart(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	if err := db.Exec(`
		CREATE TABLE transfer.capture_resources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			generation INTEGER NOT NULL,
			connector_name TEXT NOT NULL UNIQUE,
			topic_name TEXT NOT NULL UNIQUE,
			consumer_group TEXT NOT NULL UNIQUE,
			slot_name TEXT NOT NULL UNIQUE,
			publication_name TEXT NOT NULL UNIQUE,
			source_identity TEXT NOT NULL,
			source_connection_fingerprint TEXT NOT NULL,
			source_engine_id INTEGER NOT NULL,
			source_database TEXT NOT NULL,
			source_schema TEXT NOT NULL,
			source_table TEXT NOT NULL,
			status TEXT NOT NULL,
			connector_status TEXT,
			connector_error TEXT,
			topic_created BOOLEAN NOT NULL DEFAULT FALSE,
			connector_created BOOLEAN NOT NULL DEFAULT FALSE,
			slot_owned BOOLEAN NOT NULL DEFAULT TRUE,
			publication_owned BOOLEAN NOT NULL DEFAULT TRUE,
			resource_version INTEGER NOT NULL DEFAULT 1,
			last_observed_at DATETIME,
			stopped_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE(task_id, generation)
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	task := createTaskRepositoryTestTask(t, db, 7, "cdc")
	repo := NewCaptureRepository(db)
	identity := CaptureIdentity{
		TaskID: task.ID, TenantID: task.TenantID, SourceIdentity: "addp://engine/12/path/public/orders?type=table",
		SourceConnectionFingerprint: "fingerprint",
		SourceEngineID:              12, SourceDatabase: "business", SourceSchema: "public", SourceTable: "orders",
	}
	first, err := repo.BeginGeneration(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.BeginGeneration(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Generation != 1 {
		t.Fatalf("generation not reused: first=%+v second=%+v", first, second)
	}
	if err := repo.ForceUpdate(context.Background(), first.ID, map[string]interface{}{"status": models.CaptureStatusStopped}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginGeneration(context.Background(), identity); !errors.Is(err, ErrCaptureTerminal) {
		t.Fatalf("BeginGeneration() error = %v, want ErrCaptureTerminal", err)
	}
}

func TestCaptureResourceNamesAreStableAndSeparated(t *testing.T) {
	if got := captureTopicName(2, 3, 4); got != "__addp_cdc.2.3.4" {
		t.Fatalf("topic = %q", got)
	}
	if captureSlotName(2, 3, 4) == capturePublicationName(2, 3, 4) {
		t.Fatal("slot and publication names must differ")
	}
}
