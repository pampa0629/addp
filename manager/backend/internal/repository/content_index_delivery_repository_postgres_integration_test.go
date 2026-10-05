package repository

import (
	"errors"
	"os"
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
	if err := db.AutoMigrate(&models.ContentIndexOutlet{}, &models.ContentIndexDelivery{}); err != nil {
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
	op := registerContentWrite(t, db, repo, epoch, 7, "item")
	if op.TaskCorrelation != op.ID {
		t.Fatal("new write did not bind correlation before submission")
	}
	if err := repo.Unknown(t.Context(), op); err != nil {
		t.Fatal(err)
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
	if err != nil || len(purges) != 1 || purges[0].Kind != IndexDeliveryDelete {
		t.Fatalf("concurrent purge was not deduplicated: %#v %v", purges, err)
	}
	claims := make(chan bool, 2)
	for range 2 {
		copy := purges[0]
		go func() {
			claimed, err := restarted.ClaimDelete(t.Context(), next, &copy)
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
