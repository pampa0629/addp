package service

import (
	"context"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

// publishScanCompletedEvent 发布扫描完成事件（异步）。
func (s *ScanService) publishScanCompletedEvent(engineID, tenantID uint, summary models.JSONMap) {
	if s.scanEventPublisher == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		event := scantask.ScanCompletedEvent(engineID, tenantID, commonModels.JSONMap(summary), time.Now())
		if err := s.scanEventPublisher.PublishScanCompleted(ctx, event); err != nil {
			s.log.Error("发布扫描完成事件失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"error", err)
		}
	}()
}
