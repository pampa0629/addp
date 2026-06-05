package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func (s *ScanService) refreshItem(ctx context.Context, engineID, tenantID, itemID uint, token string, force bool) (*models.ScanResponse, error) {
	start := time.Now()
	_ = force
	if itemID == 0 {
		return nil, fmt.Errorf("item_id is required")
	}

	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item target not found: %w", err)
	}
	if engineID > 0 && item.EngineID != engineID {
		return nil, fmt.Errorf("item engine_id does not match request engine_id")
	}
	if engineID == 0 {
		engineID = item.EngineID
	}

	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}
	var parentNode models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, item.NodeID).First(&parentNode).Error; err != nil {
		return nil, fmt.Errorf("item parent node not found: %w", err)
	}

	result, err := s.runtimes.ItemRefresh.RefreshKnownItem(ctx, resource, tenantID, item, parentNode)
	if err != nil {
		return nil, err
	}

	return &models.ScanResponse{
		Status:        "success",
		Message:       "item refreshed",
		ItemsScanned:  1,
		FieldsScanned: result.Fields,
		DurationMs:    time.Since(start).Milliseconds(),
		StartedAt:     start.Format(time.RFC3339),
		Extraction:    scanflow.ExtractionStatsModel(result.Extraction),
	}, nil
}
