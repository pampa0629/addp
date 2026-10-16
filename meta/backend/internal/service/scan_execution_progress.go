package service

import (
	"context"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

func (s *ScanExecutionService) updateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	fields["updated_at"] = time.Now()
	lease, ok := s.boundedLease(context.Background(), executionID)
	if !ok || lease.TenantID != tenantID {
		s.log.Warn("拒绝无租约的执行进度写入", "execution_id", executionID)
		return
	}
	counters := map[string]int64{}
	for _, key := range []string{"progress", "items_scanned", "catalog_nodes_scanned", "fields_scanned"} {
		switch value := fields[key].(type) {
		case int:
			counters[key] = int64(value)
		case int64:
			counters[key] = value
		}
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if metadata, ok := fields["metadata"].(commonModels.JSONMap); ok {
			var current commonExecution.TaskExecution
			if err := tx.Select("source", "metadata").Where("execution_id = ? AND tenant_id = ?", executionID, tenantID).Take(&current).Error; err != nil {
				return err
			}
			if current.Source == commonClient.MetaScanSourceManagerMosaicProducedTarget {
				metadata = metadata.Clone()
				metadata["produced_target"] = current.Metadata["produced_target"]
				fields["metadata"] = metadata
			}
		}
		return commonExecution.UpdateWithEvent(context.Background(), tx, lease, fields, commonExecution.EventInput{Kind: "progress", Counters: counters})
	})
	if err != nil {
		s.log.Warn("更新执行进度失败", "execution_id", executionID, "error", err)
	}
}

func (s *ScanExecutionService) UpdateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	s.updateExecutionProgress(executionID, tenantID, fields)
}
