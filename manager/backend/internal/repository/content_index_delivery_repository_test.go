package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/testfixture"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newContentDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	testfixture.ContentIndexSQLite(t, db)
	return db
}

func openActiveContentIndex(t *testing.T, repo *ContentIndexDeliveryRepository) string {
	t.Helper()
	epoch, err := repo.Open(t.Context(), "endpoint", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateIfSettled(t.Context(), epoch); err != nil {
		t.Fatal(err)
	}
	return epoch
}

func registerContentWrite(t *testing.T, db *gorm.DB, repo *ContentIndexDeliveryRepository, epoch string, tenant int64, document string) *models.ContentIndexDelivery {
	t.Helper()
	op := &models.ContentIndexDelivery{TenantID: tenant, DocumentID: document, Kind: IndexDeliveryWrite}
	if err := db.Transaction(func(tx *gorm.DB) error { return repo.Register(t.Context(), tx, epoch, op) }); err != nil {
		t.Fatal(err)
	}
	return op
}

func TestContentDeliveryPersistsZeroUIDAndFencesOldGeneration(t *testing.T) {
	db := newContentDeliveryTestDB(t)
	repo := NewContentIndexDeliveryRepository(db, "content")
	epoch := openActiveContentIndex(t, repo)
	op := registerContentWrite(t, db, repo, epoch, 7, "item")
	if !errors.Is(repo.RequireActive(t.Context(), epoch), ErrContentIndexIsolated) {
		t.Fatal("uncertain submit was readable")
	}
	at := time.Date(2026, 10, 4, 0, 0, 0, 123456789, time.UTC)
	if err := repo.Receipt(t.Context(), op, 0, at); err != nil {
		t.Fatal(err)
	}
	if err := repo.RequireActive(t.Context(), epoch); err != nil {
		t.Fatal(err)
	}
	restarted := NewContentIndexDeliveryRepository(db, "content")
	next, err := restarted.Open(t.Context(), "endpoint", true)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(repo.ActivateIfSettled(t.Context(), epoch), ErrContentIndexIsolated) {
		t.Fatal("old generation reopened outlet")
	}
	if !errors.Is(restarted.ActivateIfSettled(t.Context(), next), ErrContentIndexIsolated) {
		t.Fatal("restart ignored pending task")
	}
	stored, err := restarted.Get(t.Context(), 7, op.ID)
	if err != nil || stored.TaskUID == nil || *stored.TaskUID != 0 || stored.TaskEnqueuedAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("lost receipt: %#v, %v", stored, err)
	}
	if err := restarted.Finish(t.Context(), stored, IndexDeliverySucceeded); err != nil {
		t.Fatal(err)
	}
	if err := restarted.ActivateIfSettled(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Get(t.Context(), 8, op.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cross-tenant receipt readable")
	}
	if err := restarted.Receipt(t.Context(), stored, 9, at); !errors.Is(err, ErrContentIndexDeliveryConflict) {
		t.Fatal("receipt overwritten")
	}
}

func TestContentDeliveryUnknownNeverExpiresOrReplays(t *testing.T) {
	db := newContentDeliveryTestDB(t)
	repo := NewContentIndexDeliveryRepository(db, "content")
	epoch := openActiveContentIndex(t, repo)
	op := registerContentWrite(t, db, repo, epoch, 7, "item")
	if err := repo.Unknown(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	// Simulate a historical submission made before correlation was available.
	if err := db.Model(op).Update("task_correlation", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(op).Update("updated_at", time.Now().Add(-365*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return repo.QueueProtectionPurges(t.Context(), tx, 7, []string{"item"}) }); err != nil {
		t.Fatal(err)
	}
	ops, err := repo.Recoverable(t.Context())
	if err != nil || len(ops) != 1 || ops[0].Kind != IndexDeliveryDelete {
		t.Fatalf("unknown task recovered for replay: %#v, %v", ops, err)
	}
	claimed, err := repo.ClaimDelete(t.Context(), epoch, &ops[0])
	if err != nil || claimed {
		t.Fatalf("delete preceded uncertain late write: %t, %v", claimed, err)
	}
	if !errors.Is(repo.ActivateIfSettled(t.Context(), epoch), ErrContentIndexIsolated) {
		t.Fatal("unknown aged into completion")
	}
	next, err := repo.Open(t.Context(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := repo.State(t.Context(), next)
	if err != nil || !state.Isolated || state.Configured {
		t.Fatal("disabled outlet not fenced")
	}
	stored, err := repo.Get(t.Context(), 7, op.ID)
	if err != nil || stored.Status != IndexDeliveryUnknown {
		t.Fatal("disable discarded unknown task")
	}
}

func TestContentDeliveryPurgeRollbackAndLateWriteOrdering(t *testing.T) {
	db := newContentDeliveryTestDB(t)
	repo := NewContentIndexDeliveryRepository(db, "content")
	epoch := openActiveContentIndex(t, repo)
	sentinel := errors.New("rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.QueueProtectionPurges(t.Context(), tx, 7, []string{"item"}); err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := repo.RequireActive(t.Context(), epoch); err != nil {
		t.Fatal("rolled back fence retained")
	}
	op := registerContentWrite(t, db, repo, epoch, 7, "item")
	if err := repo.Receipt(t.Context(), op, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return repo.QueueProtectionPurges(t.Context(), tx, 7, []string{"item", "item"})
	}); err != nil {
		t.Fatal(err)
	}
	ops, err := repo.Recoverable(t.Context())
	if err != nil || len(ops) != 2 {
		t.Fatalf("duplicate purge: %#v, %v", ops, err)
	}
	var purge models.ContentIndexDelivery
	for _, item := range ops {
		if item.Kind == IndexDeliveryDelete {
			purge = item
		}
	}
	if claimed, err := repo.ClaimDelete(t.Context(), epoch, &purge); err != nil || claimed {
		t.Fatal("purge overtook old write")
	}
	if err := repo.Finish(t.Context(), op, IndexDeliverySucceeded); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimDelete(t.Context(), epoch, &purge); err != nil || !claimed {
		t.Fatalf("purge claim: %t %v", claimed, err)
	}
	if claimed, err := repo.ClaimDelete(t.Context(), epoch, &purge); err != nil || claimed {
		t.Fatal("purge submitted twice")
	}
	if err := repo.Receipt(t.Context(), &purge, 2, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(t.Context(), &purge, IndexDeliveryFailed); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(t.Context(), 7, purge.ID)
	if err != nil || stored.Status != IndexDeliveryFailed || stored.TaskUID == nil || *stored.TaskUID != 2 {
		t.Fatal("failed task evidence lost")
	}
	retries, err := repo.Recoverable(t.Context())
	if err != nil || len(retries) != 1 || retries[0].ID == purge.ID || retries[0].TaskUID != nil {
		t.Fatal("failed delete did not create new delivery")
	}
	if !errors.Is(repo.RequireActive(t.Context(), epoch), ErrContentIndexIsolated) {
		t.Fatal("failed delete reopened search")
	}
}

func TestContentDeliveryRequiresTransactionAndValidTerminal(t *testing.T) {
	db := newContentDeliveryTestDB(t)
	repo := NewContentIndexDeliveryRepository(db, "content")
	epoch := openActiveContentIndex(t, repo)
	op := &models.ContentIndexDelivery{TenantID: 7, DocumentID: "item", Kind: IndexDeliveryWrite}
	for _, tx := range []*gorm.DB{nil, db} {
		if err := repo.Register(context.Background(), tx, epoch, op); err == nil {
			t.Fatal("independent registration accepted")
		}
		if err := repo.QueueProtectionPurges(context.Background(), tx, 7, []string{"item"}); err == nil {
			t.Fatal("independent purge accepted")
		}
	}
	op = registerContentWrite(t, db, repo, epoch, 7, "item")
	if err := repo.Finish(t.Context(), op, IndexDeliverySucceeded); !errors.Is(err, ErrContentIndexDeliveryConflict) {
		t.Fatal("success without receipt")
	}
	if err := repo.Receipt(t.Context(), op, -1, time.Now()); !errors.Is(err, ErrContentIndexDeliveryConflict) {
		t.Fatal("negative UID accepted")
	}
	if err := repo.Receipt(t.Context(), op, 1, time.Time{}); !errors.Is(err, ErrContentIndexDeliveryConflict) {
		t.Fatal("missing enqueue identity accepted")
	}
}
