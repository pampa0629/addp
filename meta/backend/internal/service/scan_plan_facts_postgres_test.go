package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The standard gate validates addp_test ownership. All schema and fixture
// changes below are rolled back, including execution-store initialization.
func TestScanPlanFactsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("run make test-meta-postgres")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() {
		if err := tx.Rollback().Error; err != nil {
			t.Error("rollback Meta scan plan fixture", err)
		}
	}()
	if err := tx.Exec("DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}, &models.ScanTask{}); err != nil {
		t.Fatal(err)
	}
	if err := execution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	t.Run("startup_backfill_preserves_newer_schedule", func(t *testing.T) {
		ctx := context.Background()
		task := scantask.NewEngineScanTask(998911, 998912, 9, "Schedule backfill", "0 3 * * *", "basic", time.Now(), nil)
		if err := tx.Create(task).Error; err != nil {
			t.Fatal(err)
		}

		due := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
		later := due.Add(time.Hour)
		// Both replicas read this version while its next run is still missing.
		replicaA, replicaB := NewScanTaskScheduler(NewScanTaskService(tx), nil).initializeNextRunAt, NewScanTaskScheduler(NewScanTaskService(tx), nil).initializeNextRunAt
		assertNext := func(want *time.Time) {
			t.Helper()
			var current models.ScanTask
			if err := tx.First(&current, task.ID).Error; err != nil {
				t.Fatal(err)
			}
			got := current.NextRunAt
			if (want == nil && got != nil) || (want != nil && (got == nil || !got.Equal(*want))) {
				t.Fatalf("next_run_at = %v, want %v", got, want)
			}
		}
		update := func(values map[string]interface{}) {
			t.Helper()
			if err := tx.Model(&models.ScanTask{}).Where("id = ?", task.ID).Updates(values).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := replicaA(ctx, *task, due); err != nil {
			t.Fatal(err)
		}
		if err := replicaB(ctx, *task, later); err != nil {
			t.Fatal(err)
		}
		assertNext(&due)

		// A late initializer must preserve a position already advanced by a claim.
		update(map[string]interface{}{"next_run_at": later})
		if err := replicaB(ctx, *task, due); err != nil {
			t.Fatal(err)
		}
		assertNext(&later)

		update(map[string]interface{}{"enabled": false, "next_run_at": nil})
		if err := replicaB(ctx, *task, due); err != nil {
			t.Fatal(err)
		}
		assertNext(nil)
		update(map[string]interface{}{"enabled": true, "schedule": "0 7 * * *", "next_run_at": nil})
		if err := replicaB(ctx, *task, due); err != nil {
			t.Fatal(err)
		}
		assertNext(nil)
	})
	t.Run("disable_schedule_preserves_plan_and_execution", func(t *testing.T) {
		rollback := errors.New("rollback schedule fixture")
		err := tx.Transaction(func(fixture *gorm.DB) error {
			exerciseDisablePGEngineSchedule(t, fixture)
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	})
	exerciseCollectionRetirement(t, tx)
	exerciseCollectionDeniedAttempt(t, tx)
	exerciseScanCancellation(t, tx)
	exerciseScanPlanFacts(t, tx)
	exerciseProductionScanPlanFacts(t, tx)
	exerciseScanPlanTaskBinding(t, tx)
}
