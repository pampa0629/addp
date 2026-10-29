package repository

import (
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

func newCleanupTargetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newContentDeliveryTestDB(t)
	// This process-local fixture does not claim production migration coverage.
	if err := db.Exec(`CREATE TABLE manager.cleanup_targets (
	 key TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL,kind TEXT NOT NULL,identity TEXT NOT NULL,
	 generation INTEGER NOT NULL DEFAULT 0,operation_id TEXT NOT NULL DEFAULT '',updated_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCleanupTargetObservationDoesNotWriteAndRejectsMalformedKeys(t *testing.T) {
	db := newCleanupTargetTestDB(t)
	key := CleanupTargetKey{TenantID: 7, Kind: CleanupTargetPreviewState, Identity: "exact-fingerprint"}
	token, err := ObserveCleanupTarget(t.Context(), db, key)
	if err != nil || token.Generation != 0 || token.Key != key {
		t.Fatal("unexpected initial observation", token, err)
	}
	var count int64
	if err := db.Model(&models.CleanupTarget{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("observation wrote target state", count, err)
	}
	for _, wrong := range []CleanupTargetKey{{}, {TenantID: 7, Kind: "arbitrary", Identity: "item"}, {TenantID: 7, Kind: CleanupTargetEmbedding, Identity: " item"}} {
		if _, err := ObserveCleanupTarget(t.Context(), db, wrong); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatal("invalid target accepted", wrong, err)
		}
	}
	if err := GuardCleanupTargetWrite(t.Context(), db, token); err == nil {
		t.Fatal("nontransactional guard accepted")
	}
}

func TestCleanupTargetRejectsLateWritersAndRetainsTenantBoundary(t *testing.T) {
	for _, kind := range []string{CleanupTargetPreviewState, CleanupTargetEmbedding, CleanupTargetGeneration} {
		t.Run(kind, func(t *testing.T) {
			db := newCleanupTargetTestDB(t)
			ctx, lease := indexCleanupLease(t, db)
			key := CleanupTargetKey{TenantID: 7, Kind: kind, Identity: "same-item"}
			old, err := ObserveCleanupTarget(ctx, db, key)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return FenceCleanupTarget(ctx, tx, lease, old) }); err != nil {
				t.Fatal(err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return GuardCleanupTargetWrite(ctx, tx, old) }); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatal("old request resurrected target", err)
			}
			fresh, err := ObserveCleanupTarget(ctx, db, key)
			if err != nil || fresh.Generation != 1 {
				t.Fatal("fresh generation unavailable", fresh, err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return GuardCleanupTargetWrite(ctx, tx, fresh) }); err != nil {
				t.Fatal("fresh authorized request blocked", err)
			}
			other, err := ObserveCleanupTarget(ctx, db, CleanupTargetKey{TenantID: 8, Kind: kind, Identity: "same-item"})
			if err != nil || other.Generation != 0 {
				t.Fatal("cross-tenant generation leaked", other, err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return FenceCleanupTarget(ctx, tx, lease, other) }); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatal("wrong tenant cleanup accepted", err)
			}
		})
	}
}

func TestCleanupTargetRollbackAndUnresolvedOperationNeverReopen(t *testing.T) {
	db := newCleanupTargetTestDB(t)
	ctx, lease := indexCleanupLease(t, db)
	key := CleanupTargetKey{TenantID: 7, Kind: CleanupTargetEmbedding, Identity: "item"}
	token, err := ObserveCleanupTarget(ctx, db, key)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("private result transaction failure")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := FenceCleanupTarget(ctx, tx, lease, token); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return GuardCleanupTargetWrite(ctx, tx, token) }); err != nil {
		t.Fatal("rollback retained a committed generation", err)
	}
	id, _ := key.digest()
	if err := db.Model(&models.CleanupTarget{}).Where("key = ?", id).Update("operation_id", "unresolved-original-operation").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveCleanupTarget(ctx, db, key); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal("read observation reopened unresolved effect", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return GuardCleanupTargetWrite(ctx, tx, token) }); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal("restart/ordinary writer bypassed operation hold", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return FenceCleanupTarget(ctx, tx, lease, token) }); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal("new cleanup replaced unresolved original operation", err)
	}
}
