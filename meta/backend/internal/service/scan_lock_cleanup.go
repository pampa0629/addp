package service

import (
	"context"

	"github.com/addp/meta/internal/models"
)

// cleanupStaleScanLocks 清理所有残留的扫描锁。
func (s *ScanService) cleanupStaleScanLocks() {
	if s.dedupService == nil {
		return
	}

	ctx := context.Background()

	var staleNodes []models.MetaNode
	if err := s.db.Where("scan_status = ?", "running").Find(&staleNodes).Error; err != nil {
		s.log.Warn("查询残留扫描节点失败", "error", err)
		return
	}

	if len(staleNodes) == 0 {
		s.log.Info("无残留扫描锁，跳过清理")
		return
	}

	s.log.Info("开始清理残留扫描锁", "stale_nodes_count", len(staleNodes))

	cleanedCount := 0
	for _, node := range staleNodes {
		lockKey := s.dedupService.GenerateNamespaceLockKey(node.TenantID, node.EngineID, node.Name)
		if err := s.dedupService.ClearTask(ctx, lockKey); err != nil {
			s.log.Warn("清理残留锁失败",
				"node_id", node.ID,
				"engine_id", node.EngineID,
				"schema", node.Name,
				"error", err)
			continue
		}

		if err := s.db.Model(&node).Updates(map[string]interface{}{
			"scan_status": "pending",
			"scanned_at":  nil,
		}).Error; err != nil {
			s.log.Warn("重置节点状态失败",
				"node_id", node.ID,
				"error", err)
			continue
		}

		cleanedCount++
		s.log.Info("清理残留锁成功",
			"node_id", node.ID,
			"engine_id", node.EngineID,
			"tenant_id", node.TenantID,
			"schema", node.Name)
	}

	s.log.Info("残留扫描锁清理完成",
		"total", len(staleNodes),
		"cleaned", cleanedCount)
}
