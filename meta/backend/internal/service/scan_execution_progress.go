package service

import (
	"context"
	"time"

	commonExecution "github.com/addp/common/execution"
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
	if err := commonExecution.UpdateWithEvent(context.Background(), s.db, lease, fields, commonExecution.EventInput{Kind: "progress", Counters: counters}); err != nil {
		s.log.Warn("更新执行进度失败", "execution_id", executionID, "error", err)
	}
}

func (s *ScanExecutionService) UpdateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	s.updateExecutionProgress(executionID, tenantID, fields)
}
