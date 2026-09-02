package service

import (
	"context"
	"fmt"

	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

type ProtectionDerivedDataPurger struct {
	db    *gorm.DB
	cache *TileCacheService
}

func NewProtectionDerivedDataPurger(db *gorm.DB, cache *TileCacheService) *ProtectionDerivedDataPurger {
	return &ProtectionDerivedDataPurger{db: db, cache: cache}
}

// PurgeProtectionDerivedData removes tenant tile caches before Service
// acknowledges a new projection cursor. Cached results predate the new
// protection decision and cannot be served without re-executing the source
// gate.
func (p *ProtectionDerivedDataPurger) PurgeProtectionDerivedData(ctx context.Context, tenantID int64) error {
	if p == nil || p.db == nil || p.cache == nil || tenantID <= 0 {
		return fmt.Errorf("service protection derived-data purger is not configured")
	}
	var services []models.TileService
	if err := p.db.WithContext(ctx).Select("id", "tenant_id").Where("tenant_id = ?", tenantID).Find(&services).Error; err != nil {
		return fmt.Errorf("list tenant tile services for protection purge: %w", err)
	}
	for _, tileService := range services {
		if err := p.cache.ClearServiceCache(ctx, uint(tenantID), tileService.ID); err != nil {
			return fmt.Errorf("purge tile service %d for protection cursor: %w", tileService.ID, err)
		}
	}
	return nil
}
