package execution

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestExecutionEventsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_EXECUTION_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_EXECUTION_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	t.Run("continuous_attempt_migration_and_owner_event_budget", func(t *testing.T) {
		content, err := migrationFiles.ReadFile("migrations/014_continuous_execution_attempt.sql")
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		items := []TaskExecution{
			{TenantID: 7, ExecutionID: uuid.NewString(), Module: ModuleTransfer, TaskType: TaskTypeSync, Source: ModuleTransfer, Status: ExecutionStatusRunning, TriggerType: TriggerTypeManual, ExecutionBoundary: ExecutionBoundaryContinuous, StartedAt: &now},
			{TenantID: 7, ExecutionID: uuid.NewString(), Module: ModuleTransfer, TaskType: TaskTypeSync, Source: ModuleTransfer, Status: ExecutionStatusPending, TriggerType: TriggerTypeManual, ExecutionBoundary: ExecutionBoundaryContinuous},
			{TenantID: 7, ExecutionID: uuid.NewString(), Module: ModuleMeta, TaskType: TaskTypeScan, Source: ModuleMeta, Status: ExecutionStatusRunning, TriggerType: TriggerTypeManual, ExecutionBoundary: ExecutionBoundaryBounded, StartedAt: &now},
		}
		if err := db.Create(&items).Error; err != nil {
			t.Fatal(err)
		}
		ids := []string{items[0].ExecutionID, items[1].ExecutionID, items[2].ExecutionID}
		t.Cleanup(func() {
			for _, model := range []interface{}{&Event{}, &TaskExecution{}} {
				if err := db.Where("execution_id IN ?", ids).Delete(model).Error; err != nil {
					t.Error(err)
				}
				var count int64
				if err := db.Model(model).Where("execution_id IN ?", ids).Count(&count).Error; err != nil || count != 0 {
					t.Errorf("continuous fixture remaining=%d err=%v", count, err)
				}
			}
		})
		if err := db.Exec(string(content)).Error; err != nil {
			t.Fatal(err)
		}
		for i := range items {
			if err := db.First(&items[i], items[i].ID).Error; err != nil {
				t.Fatal(err)
			}
			want := 0
			if i == 0 {
				want = 1
			}
			if items[i].Attempt != want {
				t.Fatalf("migration item %d attempt=%d want=%d", i, items[i].Attempt, want)
			}
		}
		seed := make([]Event, MaxDailyAttemptEvents-1)
		for i := range seed {
			seed[i] = Event{ExecutionID: items[0].ExecutionID, TenantID: 7, Module: ModuleTransfer, TaskType: TaskTypeSync, Attempt: 1, OccurredAt: now, Kind: "progress", Counters: models.JSONMap{}}
		}
		if err := db.CreateInBatches(seed, 100).Error; err != nil {
			t.Fatal(err)
		}
		admit := func(item TaskExecution, writeTime time.Time) error {
			if item.ExecutionID != items[0].ExecutionID || item.Attempt != 1 || writeTime.Before(now) {
				return errors.New("owner rejected locked identity")
			}
			return nil
		}
		var wg sync.WaitGroup
		results := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results <- db.Transaction(func(tx *gorm.DB) error {
					var locked TaskExecution
					if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, items[0].ID).Error; err != nil {
						return err
					}
					return AppendContinuousEvent(t.Context(), tx, locked.ExecutionID, EventInput{Kind: "progress", Counters: map[string]int64{"records_written": 5}}, admit)
				})
			}()
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal(err)
			}
		}
		var count int64
		if err := db.Model(&Event{}).Where("execution_id = ?", items[0].ExecutionID).Count(&count).Error; err != nil || count != MaxDailyAttemptEvents {
			t.Fatalf("continuous budget=%d err=%v", count, err)
		}
		denied := errors.New("owner revoked fencing")
		if err := AppendContinuousEvent(t.Context(), db, items[0].ExecutionID, EventInput{Kind: "progress"}, func(TaskExecution, time.Time) error { return denied }); !errors.Is(err, denied) {
			t.Fatalf("budget bypassed admission=%v", err)
		}
	})
	item := TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: ModuleMeta, TaskType: TaskTypeScan, Source: ModuleMeta,
		Status: ExecutionStatusPending, TriggerType: TriggerTypeManual, ExecutionBoundary: ExecutionBoundaryBounded, SourceTaskID: NewSourceTaskIDFromUint(42)}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Delete(&TaskExecution{}, item.ID).Error })
	_, lease, err := ClaimNext(t.Context(), db, ClaimOptions{Module: ModuleMeta, TaskType: TaskTypeScan, WorkerID: "event-pg", Now: time.Now().UTC(), LeaseDuration: time.Minute})
	if err != nil || lease == nil || lease.ExecutionID != item.ExecutionID {
		t.Fatalf("claim=%#v err=%v", lease, err)
	}
	// Admission rolls back with a rejected protected update.
	if err := UpdateWithEvent(t.Context(), db, *lease, map[string]interface{}{"status": "failed"}, EventInput{Kind: "progress"}); err == nil {
		t.Fatal("protected update succeeded")
	}
	var count int64
	if err := db.Model(&Event{}).Where("execution_id = ?", item.ExecutionID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	seed := make([]Event, MaxDailyAttemptEvents-1)
	for i := range seed {
		seed[i] = Event{ExecutionID: item.ExecutionID, TenantID: 7, Module: ModuleMeta, TaskType: TaskTypeScan, Attempt: lease.Attempt, OccurredAt: time.Now().UTC(), Kind: "progress", Counters: models.JSONMap{}}
	}
	if err := db.CreateInBatches(seed, 100).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failures <- AppendBoundedEvent(t.Context(), db, *lease, EventInput{Kind: "progress", Counters: map[string]int64{"items_scanned": 10}})
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&Event{}).Where("execution_id = ?", item.ExecutionID).Count(&count).Error; err != nil || count != MaxDailyAttemptEvents {
		t.Fatalf("concurrent cap count=%d err=%v", count, err)
	}
	var last Event
	if err := db.Where("execution_id = ?", item.ExecutionID).Order("id DESC").First(&last).Error; err != nil || last.Kind != "truncated" {
		t.Fatalf("last=%#v err=%v", last, err)
	}
	repo := NewTaskExecutionRepository(db)
	denied := WithReadScopes(t.Context(), []ReadScope{})
	if _, err := repo.ListEvents(denied, item.ExecutionID, 7, 0, 100); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("scope denial=%v", err)
	}
	if _, err := repo.ListEvents(t.Context(), item.ExecutionID, 8, 0, 100); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("tenant denial=%v", err)
	}
	allowed := WithReadScopes(t.Context(), []ReadScope{{Module: ModuleMeta, TenantID: 7, PrincipalID: 9, Grants: []ReadGrant{{TaskType: TaskTypeScan, TaskHistory: true}}}})
	page, err := repo.ListEvents(allowed, item.ExecutionID, 7, 0, 100)
	if err != nil || len(page.Items) != 100 || !page.HasMore {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	next, err := repo.ListEvents(allowed, item.ExecutionID, 7, page.NextCursor, 100)
	if err != nil || len(next.Items) != 100 || next.Items[0].ID <= page.NextCursor {
		t.Fatalf("next=%#v err=%v", next, err)
	}
	// Expiry still denies a saturated writer: the cap cannot bypass fencing.
	if err := db.Model(&TaskExecution{}).Where("id = ?", item.ID).Update("lease_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := AppendBoundedEvent(t.Context(), db, *lease, EventInput{Kind: "progress"}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired append=%v", err)
	}
	if err := db.Model(&Event{}).Where("id IN ?", []int64{seed[0].ID, seed[1].ID}).Update("occurred_at", time.Now().Add(-EventRetention-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	// A second maintenance instance skips the row already owned by another cleaner.
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("SELECT id FROM common.execution_events WHERE id = ? FOR UPDATE", seed[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	pruned, err := PruneEvents(ctx, db, time.Now(), 1)
	if err != nil || pruned.Deleted != 1 {
		t.Fatalf("skip locked prune=%#v err=%v", pruned, err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	pruned, err = PruneEvents(t.Context(), db, time.Now(), 1000)
	if err != nil || pruned.Deleted != 1 {
		t.Fatalf("prune=%#v err=%v", pruned, err)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal("overview was deleted", err)
	}
	t.Run("concurrent_expired_failure_has_one_terminal_event", func(t *testing.T) {
		recovery := TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: ModuleOrchestrator, TaskType: TaskTypeOrchestration, Source: ModuleOrchestrator,
			Status: ExecutionStatusPending, TriggerType: TriggerTypeManual, ExecutionBoundary: ExecutionBoundaryBounded, Progress: 50}
		if err := db.Create(&recovery).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for _, model := range []interface{}{&Event{}, &TaskExecution{}} {
				if err := db.Where("execution_id=?", recovery.ExecutionID).Delete(model).Error; err != nil {
					t.Error(err)
				}
				var count int64
				if err := db.Model(model).Where("execution_id=?", recovery.ExecutionID).Count(&count).Error; err != nil || count != 0 {
					t.Errorf("recovery fixture remaining=%d err=%v", count, err)
				}
			}
		})
		_, recoveringLease, err := ClaimNext(t.Context(), db, ClaimOptions{Module: ModuleOrchestrator, TaskType: TaskTypeOrchestration, WorkerID: "recovery-pg", LeaseDuration: time.Minute})
		if err != nil || recoveringLease == nil || recoveringLease.ExecutionID != recovery.ExecutionID {
			t.Fatalf("recovery claim=%v err=%v", recoveringLease, err)
		}
		now := time.Now().UTC()
		if err := db.Model(&recovery).Update("lease_expires_at", now.Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		outcomes := make(chan error, 8)
		var workers sync.WaitGroup
		for range 8 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				outcomes <- FailExpiredWithEvent(t.Context(), db, *recoveringLease, now, nil)
			}()
		}
		workers.Wait()
		close(outcomes)
		succeeded := 0
		for err := range outcomes {
			if err == nil {
				succeeded++
			} else if !errors.Is(err, commonapi.ErrConflict) {
				t.Fatal(err)
			}
		}
		if succeeded != 1 {
			t.Fatalf("successful recoveries=%d", succeeded)
		}
		var terminal []Event
		if err := db.Where("execution_id=?", recovery.ExecutionID).Find(&terminal).Error; err != nil || len(terminal) != 1 || terminal[0].Kind != "failed" {
			t.Fatalf("terminal evidence=%v err=%v", terminal, err)
		}
		if err := db.First(&recovery, recovery.ID).Error; err != nil || recovery.Status != "failed" || recovery.Progress != 50 {
			t.Fatalf("recovery status=%s progress=%d err=%v", recovery.Status, recovery.Progress, err)
		}
	})
}
