package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresDeadLetterMigrationAndIdempotentUpsert(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "016_create_dead_letters.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatalf("apply dead_letters migration: %v", err)
	}
	if err := db.AutoMigrate(&models.DeadLetter{}); err != nil {
		t.Fatalf("GORM model differs from SQL-migrated dead_letters: %v", err)
	}

	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	observation := deadLetterObservation(observedAt)
	observation.Identity = "a59a0ac0-f042-5277-834d-0f982d26b7e5"
	observation.ApplyIdentity = "69f13ac3-8f85-45db-96e3-63c83cfb753b"
	observation.SourceIdentity = "integration://dead-letter/" + observation.Identity
	repo := NewDeadLetterRepository(db)
	t.Cleanup(func() {
		_ = db.WithContext(context.Background()).Where("identity = ?", observation.Identity).Delete(&models.DeadLetter{}).Error
	})
	first, err := repo.UpsertObservation(context.Background(), observation)
	if err != nil {
		t.Fatal(err)
	}
	repeated := *observation
	repeated.LastExecutionID = "execution-2"
	repeated.LastObservedAt = observedAt.Add(time.Second)
	repeated.PayloadOffset = first.PayloadOffset + 1
	second, err := repo.UpsertObservation(context.Background(), &repeated)
	if err != nil {
		t.Fatal(err)
	}
	if second.OccurrenceCount != 2 || second.FirstExecutionID != "execution-1" || second.LastExecutionID != "execution-2" {
		t.Fatalf("PostgreSQL idempotent upsert = %#v", second)
	}
	staleUpdated, err := repo.MarkPayloadUnavailable(context.Background(), models.DeadLetterPayloadReference{
		Identity: second.Identity, Topic: second.PayloadTopic, Partition: second.PayloadPartition, Offset: first.PayloadOffset,
	}, observedAt.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if staleUpdated {
		t.Fatal("stale PostgreSQL payload reference changed current availability")
	}
	updated, err := repo.MarkPayloadUnavailable(context.Background(), models.DeadLetterPayloadReference{
		Identity: second.Identity, Topic: second.PayloadTopic, Partition: second.PayloadPartition, Offset: second.PayloadOffset,
	}, observedAt.Add(3*time.Second))
	if err != nil || !updated {
		t.Fatalf("current PostgreSQL payload reference update=%v err=%v", updated, err)
	}
	stored, err := repo.Get(context.Background(), second.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PayloadAvailable || !stored.LastObservedAt.Equal(repeated.LastObservedAt) {
		t.Fatalf("PostgreSQL availability reconciliation changed audit facts: %#v", stored)
	}
}
