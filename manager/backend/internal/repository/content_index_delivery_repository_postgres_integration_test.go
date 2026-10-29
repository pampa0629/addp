package repository

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerContentDeliveryReceiptCASAndRestart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("PostgreSQL gate required")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateContentIndexDeliverySchema(db); err != nil {
		t.Fatal(err)
	}
	index := "delivery-test-" + uuid.NewString()
	t.Cleanup(func() {
		if err := db.Where("index_name = ?", index).Delete(&models.ContentIndexDelivery{}).Error; err != nil {
			t.Error(err)
		}
		if err := db.Where("index_name = ?", index).Delete(&models.ContentIndexOutlet{}).Error; err != nil {
			t.Error(err)
		}
		for _, model := range []any{&models.ContentIndexDelivery{}, &models.ContentIndexOutlet{}} {
			var remaining int64
			if err := db.Model(model).Where("index_name = ?", index).Count(&remaining).Error; err != nil || remaining != 0 {
				t.Errorf("content index fixture residue: %d %v", remaining, err)
			}
		}
	})
	repo := NewContentIndexDeliveryRepository(db, index)
	epoch := openActiveContentIndex(t, repo)
	guard, err := repo.ObserveInventoryGuard(t.Context(), epoch)
	if err != nil || guard.Registered != 0 {
		t.Fatal("initial inventory guard", guard, err)
	}
	op := registerContentWrite(t, db, repo, epoch, 7, "item")
	if op.TaskCorrelation != op.ID {
		t.Fatal("new write did not bind correlation before submission")
	}
	if err := repo.Unknown(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ObserveInventoryGuard(t.Context(), epoch); !errors.Is(err, ErrContentIndexIsolated) {
		t.Fatal("unknown write allowed inventory", err)
	}
	recoverable, err := repo.Recoverable(t.Context())
	if err != nil || len(recoverable) != 1 || recoverable[0].TaskCorrelation != op.ID {
		t.Fatal("correlated unknown excluded from recovery", err)
	}
	at := time.Date(2026, 10, 4, 0, 0, 0, 123456789, time.UTC)
	results := make(chan error, 2)
	for range 2 {
		copy := *op
		go func() { results <- repo.Receipt(t.Context(), &copy, 0, at) }()
	}
	success, conflict := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, ErrContentIndexDeliveryConflict):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("receipt CAS %d/%d", success, conflict)
	}
	// Use a separately opened connection pool, not only a second Go object.
	restartedDB, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	restartedPool, err := restartedDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer restartedPool.Close()
	restarted := NewContentIndexDeliveryRepository(restartedDB, index)
	next, err := restarted.Open(t.Context(), "endpoint", true)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.Get(t.Context(), 7, op.ID)
	if err != nil || stored.TaskUID == nil || *stored.TaskUID != 0 || stored.TaskEnqueuedAt != at.Format(time.RFC3339Nano) || stored.TaskCorrelation != op.ID {
		t.Fatalf("receipt changed across real PG restart: %#v %v", stored, err)
	}
	if !errors.Is(restarted.ActivateIfSettled(t.Context(), next), ErrContentIndexIsolated) {
		t.Fatal("pending receipt ignored")
	}
	if err := restarted.Finish(t.Context(), stored, IndexDeliverySucceeded); err != nil {
		t.Fatal(err)
	}
	if err := restarted.ActivateIfSettled(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(repo.ActivateIfSettled(t.Context(), epoch), ErrContentIndexIsolated) {
		t.Fatal("stale instance reopened index")
	}

	settledGuard, err := restarted.ObserveInventoryGuard(t.Context(), next)
	if err != nil || settledGuard.Registered != 1 || settledGuard.Epoch == guard.Epoch {
		t.Fatal("settled/restarted observation", settledGuard, err)
	}
	rollback := errors.New("rollback purge")
	if err := restartedDB.Transaction(func(tx *gorm.DB) error {
		if err := restarted.QueueProtectionPurges(t.Context(), tx, 7, []string{"item"}); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if err := restarted.RequireActive(t.Context(), next); err != nil {
		t.Fatalf("rolled-back purge left a fence: %v", err)
	}
	for range 2 {
		go func() {
			results <- restartedDB.Transaction(func(tx *gorm.DB) error {
				return restarted.QueueProtectionPurges(t.Context(), tx, 7, []string{"item"})
			})
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	purges, err := restarted.Recoverable(t.Context())
	if err != nil || len(purges) != 1 || purges[0].Kind != IndexDeliveryPurge {
		t.Fatalf("concurrent purge was not deduplicated: %#v %v", purges, err)
	}
	claims := make(chan bool, 2)
	for range 2 {
		copy := purges[0]
		go func() {
			claimed, err := restarted.ClaimMaintenance(t.Context(), next, &copy)
			claims <- claimed
			results <- err
		}()
	}
	winners := 0
	for range 2 {
		if <-claims {
			winners++
		}
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("purge submission winners: %d", winners)
	}
	purge, err := restarted.Get(t.Context(), 7, purges[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Receipt(t.Context(), purge, 1, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Finish(t.Context(), purge, IndexDeliverySucceeded); err != nil {
		t.Fatal(err)
	}
	if err := restarted.ActivateIfSettled(t.Context(), next); err != nil {
		t.Fatal(err)
	}

	zero, negative := int64(0), int64(-1)
	for _, invalid := range []models.ContentIndexDelivery{
		{Status: IndexDeliveryUnknown, TaskCorrelation: "another-delivery"},
		{Status: "invalid"},
		{Status: IndexDeliverySucceeded},
		{Status: IndexDeliverySubmitted, TaskUID: &negative, TaskEnqueuedAt: at.Format(time.RFC3339Nano)},
		{Status: IndexDeliveryUnknown, TaskUID: &zero, TaskEnqueuedAt: at.Format(time.RFC3339Nano)},
		// Receipt 0 already belongs to the first completed write, across tenant IDs.
		{Status: IndexDeliverySubmitted, TaskUID: &zero, TaskEnqueuedAt: at.Format(time.RFC3339Nano)},
	} {
		invalid.ID, invalid.IndexName, invalid.TenantID = uuid.NewString(), index, 8
		invalid.EndpointID, invalid.Kind = "endpoint", IndexDeliveryWrite
		if err := restartedDB.Create(&invalid).Error; err == nil {
			t.Fatalf("database accepted invalid or duplicate task receipt: %#v", invalid)
		}
	}

	t.Run("cleanup_hold_survives_restart_and_result_rollback", func(t *testing.T) {
		ctx, lease := indexCleanupLease(t, db)
		observed, err := restarted.ObserveInventoryGuard(ctx, next)
		if err != nil {
			t.Fatal(err)
		}
		var operation *models.CleanupIndexOperation
		if err := db.Transaction(func(tx *gorm.DB) error {
			var e error
			operation, e = restarted.BeginCleanupIndex(ctx, tx, lease, observed, strings.Repeat("a", 64), []uint{9})
			return e
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for _, model := range []any{&models.CleanupIndexTarget{}, &models.CleanupIndexOperation{}} {
				if e := db.Where("index_name=?", index).Delete(model).Error; e != nil {
					t.Error(e)
				}
			}
		})
		// A separate process generation cannot reopen or dispatch a generic delete.
		reopened, err := repo.Open(ctx, "endpoint", true)
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(repo.ActivateIfSettled(ctx, reopened), ErrContentIndexIsolated) {
			t.Fatal("restart removed Cleanup hold")
		}
		if !errors.Is(repo.RequireNoCleanupHold(ctx, reopened), ErrContentIndexIsolated) {
			t.Fatal("restart allowed index reinitialization")
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			_, e := restarted.QueueCleanupDelete(ctx, tx, lease, operation.ID, 9)
			return e
		}); !errors.Is(err, ErrContentIndexIsolated) {
			t.Fatal("previous generation dispatched", err)
		}
		// Restore this test's original outlet generation to exercise final-result
		// rollback independently; production has no such recovery/reset operation.
		if err := db.Model(&models.ContentIndexOutlet{}).Where("index_name=?", index).Update("epoch", next).Error; err != nil {
			t.Fatal(err)
		}
		var deletion *models.ContentIndexDelivery
		if err := db.Transaction(func(tx *gorm.DB) error {
			var e error
			deletion, e = restarted.QueueCleanupDelete(ctx, tx, lease, operation.ID, 9)
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if err := restarted.Receipt(ctx, deletion, 2, at.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
		deletion, err = restarted.Get(ctx, 7, deletion.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := restarted.Finish(ctx, deletion, IndexDeliverySucceeded); err != nil {
			t.Fatal(err)
		}
		sentinel := errors.New("professional result write failed")
		if err := db.Transaction(func(tx *gorm.DB) error {
			if e := restarted.CompleteCleanupIndex(ctx, tx, lease, operation.ID); e != nil {
				return e
			}
			return sentinel
		}); !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
		if !errors.Is(restarted.ActivateIfSettled(ctx, next), ErrContentIndexIsolated) {
			t.Fatal("failed result commit released hold")
		}
		stale := lease
		stale.Token = "stale"
		if err := db.Transaction(func(tx *gorm.DB) error { return restarted.CompleteCleanupIndex(ctx, tx, stale, operation.ID) }); err == nil {
			t.Fatal("stale lease finalized")
		}
		if err := db.Transaction(func(tx *gorm.DB) error { return restarted.CompleteCleanupIndex(ctx, tx, lease, operation.ID) }); err != nil {
			t.Fatal(err)
		}
		if err := restarted.ActivateIfSettled(ctx, next); err != nil {
			t.Fatal(err)
		}
		late := &models.ContentIndexDelivery{TenantID: 7, EngineIDs: []uint{9}, DocumentIDs: []string{"late"}, Kind: IndexDeliveryWrite}
		if err := db.Transaction(func(tx *gorm.DB) error { return restarted.Register(ctx, tx, next, late) }); !errors.Is(err, ErrContentIndexIsolated) {
			t.Fatal("late write was accepted", err)
		}
	})

	// A historical submission never sent a marker. The schema default and
	// subsequent migrations must preserve that missing evidence, not invent it.
	legacyID := uuid.NewString()
	if err := restartedDB.Exec(`INSERT INTO manager.content_index_deliveries
 (id,index_name,tenant_id,document_id,kind,"filter",endpoint_id,status,task_enqueued_at)
 VALUES (?,?,7,'legacy-item','write','','endpoint','unknown','')`, legacyID, index).Error; err != nil {
		t.Fatal(err)
	}
	if err := restartedDB.AutoMigrate(&models.ContentIndexDelivery{}); err != nil {
		t.Fatal(err)
	}
	legacy, err := restarted.Get(t.Context(), 7, legacyID)
	if err != nil || legacy.TaskCorrelation != "" || legacy.TaskUID != nil || legacy.Status != IndexDeliveryUnknown {
		t.Fatalf("migration fabricated legacy receipt evidence: %#v %v", legacy, err)
	}
	if ops, err := restarted.Recoverable(t.Context()); err != nil || len(ops) != 0 {
		t.Fatalf("unmarked historical submission entered automatic recovery: %#v %v", ops, err)
	}
	if !errors.Is(restarted.ActivateIfSettled(t.Context(), next), ErrContentIndexIsolated) {
		t.Fatal("legacy unresolved submission no longer fences the outlet")
	}
}
