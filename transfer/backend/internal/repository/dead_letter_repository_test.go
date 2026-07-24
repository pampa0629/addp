package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/addp/transfer/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeadLetterRepositoryUpsertsRepeatedObservation(t *testing.T) {
	db := newDeadLetterRepositoryTestDB(t)
	repo := NewDeadLetterRepository(db)
	firstAt := time.Date(2026, 7, 23, 6, 0, 0, 0, time.UTC)
	first := deadLetterObservation(firstAt)
	stored, err := repo.UpsertObservation(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OccurrenceCount != 1 || stored.FirstExecutionID != "execution-1" {
		t.Fatalf("first stored observation = %#v", stored)
	}

	second := deadLetterObservation(firstAt.Add(time.Minute))
	second.FirstExecutionID = "execution-2"
	second.LastExecutionID = "execution-2"
	second.ErrorMessage = "latest safe message"
	second.PayloadOffset = 23
	stored, err = repo.UpsertObservation(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OccurrenceCount != 2 || stored.FirstExecutionID != "execution-1" || stored.LastExecutionID != "execution-2" {
		t.Fatalf("repeated stored observation = %#v", stored)
	}
	if stored.PayloadOffset != 23 || stored.ErrorMessage != "latest safe message" || !stored.FirstObservedAt.Equal(firstAt) {
		t.Fatalf("repeated observation did not preserve first/update latest fields: %#v", stored)
	}

	reference := models.DeadLetterPayloadReference{
		Identity: stored.Identity, Topic: stored.PayloadTopic, Partition: stored.PayloadPartition, Offset: stored.PayloadOffset,
	}
	updated, err := repo.MarkPayloadUnavailable(context.Background(), reference, firstAt.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("current payload reference was not marked unavailable")
	}
	stored, err = repo.Get(context.Background(), stored.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PayloadAvailable {
		t.Fatal("payload remained available after explicit unavailable observation")
	}
	if !stored.LastObservedAt.Equal(second.LastObservedAt) {
		t.Fatalf("availability reconciliation changed last_observed_at: %v", stored.LastObservedAt)
	}
}

func TestDeadLetterRepositoryPayloadReferenceCursorAndCAS(t *testing.T) {
	db := newDeadLetterRepositoryTestDB(t)
	repo := NewDeadLetterRepository(db)
	base := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	first := deadLetterObservation(base)
	first.Identity = "11111111-1111-5111-8111-111111111111"
	first.ApplyIdentity = "11111111-1111-4111-8111-111111111111"
	second := deadLetterObservation(base)
	second.Identity = "22222222-2222-5222-8222-222222222222"
	second.ApplyIdentity = "22222222-2222-4222-8222-222222222222"
	second.SourceOffset = 42
	second.PayloadOffset = 20
	if err := db.Create([]*models.DeadLetter{first, second}).Error; err != nil {
		t.Fatal(err)
	}

	references, err := repo.ListAvailablePayloadReferences(context.Background(), first.Identity, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 1 || references[0].Identity != second.Identity || references[0].Offset != second.PayloadOffset {
		t.Fatalf("payload reference cursor result = %#v", references)
	}

	stale := references[0]
	if err := db.Model(&models.DeadLetter{}).Where("identity = ?", second.Identity).
		Updates(map[string]interface{}{"payload_offset": int64(21), "payload_available": true}).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := repo.MarkPayloadUnavailable(context.Background(), stale, base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("stale payload reference changed the refreshed availability state")
	}
	stored, err := repo.Get(context.Background(), second.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.PayloadAvailable || stored.PayloadOffset != 21 {
		t.Fatalf("refreshed payload reference was overwritten: %#v", stored)
	}
}

func TestDeadLetterRepositoryListsAndGetsWithinTenantTaskScope(t *testing.T) {
	db := newDeadLetterRepositoryTestDB(t)
	repo := NewDeadLetterRepository(db)
	base := time.Date(2026, 7, 23, 7, 0, 0, 0, time.UTC)
	first := deadLetterObservation(base)
	first.Identity = "11111111-1111-5111-8111-111111111111"
	first.ApplyIdentity = "11111111-1111-4111-8111-111111111111"
	first.PayloadAvailable = false
	second := deadLetterObservation(base.Add(time.Minute))
	second.Identity = "22222222-2222-5222-8222-222222222222"
	second.ApplyIdentity = first.ApplyIdentity
	second.SourceOffset = 42
	otherTenant := deadLetterObservation(base.Add(2 * time.Minute))
	otherTenant.Identity = "33333333-3333-5333-8333-333333333333"
	otherTenant.ApplyIdentity = "33333333-3333-4333-8333-333333333333"
	otherTenant.TenantID = 8
	otherTask := deadLetterObservation(base.Add(3 * time.Minute))
	otherTask.Identity = "44444444-4444-5444-8444-444444444444"
	otherTask.ApplyIdentity = "44444444-4444-4444-8444-444444444444"
	otherTask.TaskID = 12
	if err := db.Create([]*models.DeadLetter{first, second, otherTenant, otherTask}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.DeadLetter{}).Where("identity = ?", first.Identity).Update("payload_available", false).Error; err != nil {
		t.Fatal(err)
	}

	unavailable := false
	items, total, err := repo.ListByTask(context.Background(), 7, 11, models.DeadLetterListRequest{
		Page: 1, PageSize: 20, SourcePartition: "2", PayloadAvailable: &unavailable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Identity != first.Identity {
		t.Fatalf("scoped filtered list total=%d items=%#v", total, items)
	}

	items, total, err = repo.ListByTask(context.Background(), 7, 11, models.DeadLetterListRequest{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 1 || items[0].Identity != second.Identity {
		t.Fatalf("stable ordered page total=%d items=%#v", total, items)
	}
	if _, err := repo.GetByTask(context.Background(), 8, 11, first.Identity); err == nil {
		t.Fatal("cross-tenant dead-letter detail was visible")
	}
}

func newDeadLetterRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:dead_letter_repository_" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE transfer.dead_letters (
		identity TEXT PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		task_id INTEGER NOT NULL,
		apply_identity TEXT NOT NULL,
		first_execution_id TEXT NOT NULL,
		last_execution_id TEXT NOT NULL,
		source_identity TEXT NOT NULL,
		source_topic TEXT NOT NULL,
		source_partition TEXT NOT NULL,
		source_offset INTEGER NOT NULL,
		source_timestamp DATETIME,
		error_code TEXT NOT NULL,
		error_category TEXT NOT NULL,
		error_message TEXT NOT NULL,
		payload_topic TEXT NOT NULL,
		payload_partition INTEGER NOT NULL,
		payload_offset INTEGER NOT NULL,
		payload_available BOOLEAN NOT NULL,
		first_observed_at DATETIME NOT NULL,
		last_observed_at DATETIME NOT NULL,
		occurrence_count INTEGER NOT NULL,
		created_at DATETIME,
		updated_at DATETIME,
		UNIQUE(apply_identity, source_identity, source_partition, source_offset)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func deadLetterObservation(observedAt time.Time) *models.DeadLetter {
	return &models.DeadLetter{
		Identity: "a220d5ad-d86e-52ca-ad4f-5ff2d8bfad1c", TenantID: 7, TaskID: 11,
		ApplyIdentity:    "8aa1d865-8d56-4ac3-b9aa-59f50e575c37",
		FirstExecutionID: "execution-1", LastExecutionID: "execution-1",
		SourceIdentity: "addp://engine/9/path/orders?type=topic", SourceTopic: "orders", SourcePartition: "2", SourceOffset: 41,
		ErrorCode: "invalid_json_object", ErrorCategory: "record_decode", ErrorMessage: "record value must be a JSON object",
		PayloadTopic: "__addp_dlq.7.11", PayloadPartition: 0, PayloadOffset: 19, PayloadAvailable: true,
		FirstObservedAt: observedAt, LastObservedAt: observedAt, OccurrenceCount: 1,
	}
}
