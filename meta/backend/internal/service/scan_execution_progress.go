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
	if err := commonExecution.UpdateWithLease(context.Background(), s.db, lease, fields); err != nil {
		s.log.Warn("更新执行进度失败", "execution_id", executionID, "error", err)
	}
}

func (s *ScanExecutionService) UpdateExecutionProgress(executionID string, tenantID int, fields map[string]interface{}) {
	s.updateExecutionProgress(executionID, tenantID, fields)
}
