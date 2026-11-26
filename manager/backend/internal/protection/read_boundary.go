package protection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ProjectionFreshener interface {
	EnsureCurrent(context.Context, int64) error
}

type ReadBoundary struct {
	store    ProjectionFreshener
	db       *gorm.DB
	instance string
	// Only entries whose local output is finished are eligible for retry.
	finished sync.Map
}

func NewReadBoundary(store ProjectionFreshener, db *gorm.DB, instance string) *ReadBoundary {
	return &ReadBoundary{store: store, db: db, instance: instance}
}

func (b *ReadBoundary) BeginRead(ctx context.Context, tenantID int64) (func(), error) {
	if b == nil || b.store == nil || b.db == nil || b.instance == "" || tenantID <= 0 {
		return nil, ErrRequired
	}
	row := models.ProtectionRead{ReadID: uuid.NewString(), TenantID: tenantID, ProcessInstanceID: b.instance, StartedAt: time.Now().UTC()}
	inserted := false
	err := b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		inserted = true
		return nil
	})
	if err != nil {
		// Commit can lose its reply. No output has started in that case.
		if inserted {
			b.finished.Store(row.ReadID, tenantID)
		}
		return nil, fmt.Errorf("%w: %w", ErrRequired, err)
	}
	end := sync.OnceFunc(func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := b.release(releaseCtx, row.ReadID, tenantID); err != nil {
			b.finished.Store(row.ReadID, tenantID)
			slog.Warn("Manager 读取结束登记待释放", "read_id", row.ReadID, "instance_id", b.instance)
		}
	})
	if err := b.store.EnsureCurrent(ctx, tenantID); err != nil {
		end()
		return nil, fmt.Errorf("%w: %w", ErrRequired, err)
	}
	return end, nil
}

func (b *ReadBoundary) release(ctx context.Context, id string, tenant int64) error {
	return b.db.WithContext(ctx).Where("read_id = ? AND tenant_id = ? AND process_instance_id = ?", id, tenant, b.instance).Delete(&models.ProtectionRead{}).Error
}

func (b *ReadBoundary) RecoverFinished(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.retryFinished(ctx)
		}
	}
}

func (b *ReadBoundary) retryFinished(ctx context.Context) {
	b.finished.Range(func(key, value any) bool {
		if err := b.release(ctx, key.(string), value.(int64)); err != nil {
			return false
		}
		b.finished.Delete(key)
		return true
	})
}

func (b *ReadBoundary) RequireDrained(ctx context.Context, tenantID int64) error {
	if b == nil || b.db == nil {
		return ErrRequired
	}
	var active bool
	if err := b.db.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM manager.protection_reads WHERE tenant_id = ?)", tenantID).Scan(&active).Error; err != nil {
		return fmt.Errorf("check active protection reads: %w", err)
	}
	if active {
		return errors.New("manager protection reads are still active")
	}
	return nil
}
