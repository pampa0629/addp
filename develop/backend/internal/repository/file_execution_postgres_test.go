package repository

import (
	"bytes"
	"github.com/google/uuid"
	"os"
	"reflect"
	"testing"
	"time"

	commonexecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// This is selected by the existing Develop PostgreSQL gate, with all DDL and
// execution mutations rolled back in its disposable database.
func TestFileExecutionAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() {
		if err := tx.Rollback().Error; err != nil {
			t.Errorf("rollback file execution test: %v", err)
		}
	})
	if err := commonexecution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.FileExecution{}, &models.DevTask{}); err != nil {
		t.Fatal(err)
	}
	t.Run("original file cutover", func(t *testing.T) { runOriginalFileCutover(t, tx) })
	t.Run("per file grant delivery", func(t *testing.T) { runFileProductionDelivery(t, tx) })
	repo := NewFileExecutionRepository(tx)
	item, sources := newFilePendingValues()
	runFileQueueCase(t, tx, repo, item, sources, "101")
	facts, err := repo.Read(t.Context(), 7, item.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fileExecutionRawConfig(t.Context(), tx, item.ExecutionID, 7)
	if err != nil || !bytes.Equal(raw, facts.ConfigJSON) || facts.Validate() != nil {
		t.Fatalf("PostgreSQL frozen config changed: %v", err)
	}
	// A malformed configuration fails inside the Owner transaction and cannot
	// leave a generic claimable row behind.
	bad, badSources := newFilePendingValues()
	bad.ExecutionConfig["timeout"] = 1.5
	if _, err := repo.CreatePending(t.Context(), bad, badSources, nil); err == nil {
		t.Fatal("noninteger policy accepted")
	}
	for _, model := range []any{&commonexecution.TaskExecution{}, &models.FileExecution{}} {
		var count int64
		if err := tx.Model(model).Where("execution_id = ?", bad.ExecutionID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("atomic rollback failed: %d %v", count, err)
		}
	}
	rejected, rejectedSources := newFilePendingValues()
	if _, err := repo.CreatePending(t.Context(), rejected, rejectedSources, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.RejectUnissued(t.Context(), 7, rejected.ExecutionID); err != nil {
		t.Fatal(err)
	}
	value, err := repo.Read(t.Context(), 7, rejected.ExecutionID)
	if err != nil || value.AdmissionState != "rejected" || value.AuthorizationID != "" {
		t.Fatalf("PostgreSQL rejection fabricated issuance: %v", err)
	}

	for _, trigger := range []string{"manual", "scheduled"} {
		t.Run(trigger+" parent freezes claim and shares pending admission queue", func(t *testing.T) {
			parent, _ := newFilePendingValues()
			stamp := time.Now().UTC()
			expiry := stamp.Add(time.Minute)
			token, owner := uuid.NewString(), "parent-fixture"
			parent.Module, parent.Source, parent.TaskType = "orchestrator", "orchestrator", "orchestration"
			parent.TriggerType = trigger
			parent.Status, parent.Attempt, parent.StartedAt = "running", 1, &stamp
			parent.LeaseOwner, parent.LeaseToken, parent.LeaseExpiresAt = &owner, &token, &expiry
			if err := tx.Create(parent).Error; err != nil {
				t.Fatal(err)
			}
			child, childSources := newFilePendingValues()
			child.Source = "orchestrator"
			child.TriggerType = trigger
			child.ParentExecutionID = &parent.ExecutionID
			runFileQueueCase(t, tx, repo, child, childSources, map[string]string{"manual": "104", "scheduled": "105"}[trigger])
			original, err := repo.Read(t.Context(), 7, child.ExecutionID)
			expected, _ := commonexecution.NewDevelopFileParent(parent.ExecutionID, parent.Attempt, token)
			if err != nil || !reflect.DeepEqual(original.Intent.Parent, expected) {
				t.Fatal("parent claim not frozen", err)
			}
			denied, deniedSources := newFilePendingValues()
			denied.Source = "orchestrator"
			denied.TriggerType = trigger
			denied.ParentExecutionID = &parent.ExecutionID
			if err := tx.Model(parent).UpdateColumn("trigger_type", map[string]string{"manual": "scheduled", "scheduled": "manual"}[trigger]).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CreatePending(t.Context(), denied, deniedSources, nil); err == nil {
				t.Fatal("mismatched parent trigger entered file chain")
			}
			var count int64
			if err := tx.Model(&commonexecution.TaskExecution{}).Where("execution_id=?", denied.ExecutionID).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("rejected child left claimable execution", err)
			}
		})
	}
	root, rootSources := newFilePendingValues()
	root.TriggerType = "scheduled"
	if _, err := repo.CreatePending(t.Context(), root, rootSources, nil); err == nil {
		t.Fatal("scheduled Develop root entered file chain")
	}
	runPrivatePublicationCase(t, tx, repo, 102, false)
	runPrivatePublicationCase(t, tx, repo, 103, true)
}
