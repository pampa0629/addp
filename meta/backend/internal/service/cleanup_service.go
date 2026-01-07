package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// CleanupService 负责定期清理软删除的记录
type CleanupService struct {
	db              *gorm.DB
	log             *slog.Logger
	retentionDays   int
	cleanupInterval time.Duration
	enabled         bool
	stopCh          chan struct{}
}

// CleanupConfig 清理服务配置
type CleanupConfig struct {
	Enabled         bool
	RetentionDays   int
	CleanupInterval time.Duration
}

// DefaultCleanupConfig 返回默认配置
func DefaultCleanupConfig() CleanupConfig {
	return CleanupConfig{
		Enabled:         true,
		RetentionDays:   90,
		CleanupInterval: 24 * time.Hour,
	}
}

// NewCleanupService 创建清理服务
func NewCleanupService(db *gorm.DB, config CleanupConfig) *CleanupService {
	if config.RetentionDays == 0 {
		config.RetentionDays = 90
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 24 * time.Hour
	}

	return &CleanupService{
		db:              db,
		log:             logger.With("component", "cleanup_service"),
		retentionDays:   config.RetentionDays,
		cleanupInterval: config.CleanupInterval,
		enabled:         config.Enabled,
		stopCh:          make(chan struct{}),
	}
}

// Start 启动清理服务
func (s *CleanupService) Start(ctx context.Context) error {
	if !s.enabled {
		s.log.Info("清理服务已禁用")
		return nil
	}

	s.log.Info("启动清理服务",
		"retention_days", s.retentionDays,
		"cleanup_interval", s.cleanupInterval)

	go s.scheduleCleanup(ctx)
	return nil
}

// Stop 停止清理服务
func (s *CleanupService) Stop(ctx context.Context) error {
	if !s.enabled {
		return nil
	}

	s.log.Info("停止清理服务")
	close(s.stopCh)
	return nil
}

// scheduleCleanup 定时调度清理任务
func (s *CleanupService) scheduleCleanup(ctx context.Context) {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCleanup(ctx)
		}
	}
}

// runCleanup 执行清理任务
func (s *CleanupService) runCleanup(ctx context.Context) {
	startTime := time.Now()
	s.log.Info("开始清理软删除记录和历史扫描记录", "retention_days", s.retentionDays)

	expiryTime := time.Now().AddDate(0, 0, -s.retentionDays)

	// 先清理 meta_item（子表）
	var itemsDeleted int64
	result := s.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", expiryTime).
		Delete(&models.MetaItem{})

	if result.Error != nil {
		s.log.Error("清理 meta_item 失败", "error", result.Error)
	} else {
		itemsDeleted = result.RowsAffected
		s.log.Info("清理 meta_item 完成", "deleted_count", itemsDeleted)
	}

	// 再清理 meta_node（父表）
	var nodesDeleted int64
	result = s.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", expiryTime).
		Delete(&models.MetaNode{})

	if result.Error != nil {
		s.log.Error("清理 meta_node 失败", "error", result.Error)
	} else {
		nodesDeleted = result.RowsAffected
		s.log.Info("清理 meta_node 完成", "deleted_count", nodesDeleted)
	}

	// 清理历史扫描记录（激进策略）
	var scansDeleted int64
	scansDeleted = s.cleanupScanTaskRuns()

	s.log.Info("清理任务完成",
		"duration", time.Since(startTime),
		"items_deleted", itemsDeleted,
		"nodes_deleted", nodesDeleted,
		"scans_deleted", scansDeleted,
		"total_deleted", itemsDeleted+nodesDeleted+scansDeleted)
}

// cleanupScanTaskRuns 清理历史扫描记录（激进策略）
func (s *CleanupService) cleanupScanTaskRuns() int64 {
	now := time.Now()
	var totalDeleted int64

	// 1. 清理 30 天前的成功记录
	result := s.db.Where("status = ? AND completed_at < ?",
		"success", now.Add(-30*24*time.Hour)).
		Delete(&models.ScanTaskRun{})

	if result.Error != nil {
		s.log.Error("清理成功扫描记录失败", "error", result.Error)
	} else {
		totalDeleted += result.RowsAffected
		if result.RowsAffected > 0 {
			s.log.Info("清理成功扫描记录", "deleted", result.RowsAffected, "before", now.Add(-30*24*time.Hour).Format("2006-01-02"))
		}
	}

	// 2. 清理 90 天前的失败记录
	result = s.db.Where("status = ? AND completed_at < ?",
		"failed", now.Add(-90*24*time.Hour)).
		Delete(&models.ScanTaskRun{})

	if result.Error != nil {
		s.log.Error("清理失败扫描记录失败", "error", result.Error)
	} else {
		totalDeleted += result.RowsAffected
		if result.RowsAffected > 0 {
			s.log.Info("清理失败扫描记录", "deleted", result.RowsAffected, "before", now.Add(-90*24*time.Hour).Format("2006-01-02"))
		}
	}

	// 3. 清理 7 天前的取消记录
	result = s.db.Where("status = ? AND completed_at < ?",
		"canceled", now.Add(-7*24*time.Hour)).
		Delete(&models.ScanTaskRun{})

	if result.Error != nil {
		s.log.Error("清理取消扫描记录失败", "error", result.Error)
	} else {
		totalDeleted += result.RowsAffected
		if result.RowsAffected > 0 {
			s.log.Info("清理取消扫描记录", "deleted", result.RowsAffected, "before", now.Add(-7*24*time.Hour).Format("2006-01-02"))
		}
	}

	return totalDeleted
}

// ManualCleanup 手动触发清理
func (s *CleanupService) ManualCleanup(ctx context.Context, retentionDays int) (map[string]int64, error) {
	if retentionDays == 0 {
		retentionDays = s.retentionDays
	}

	expiryTime := time.Now().AddDate(0, 0, -retentionDays)

	var itemsDeleted, nodesDeleted int64

	result := s.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", expiryTime).
		Delete(&models.MetaItem{})
	if result.Error != nil {
		return nil, fmt.Errorf("清理 meta_item 失败: %w", result.Error)
	}
	itemsDeleted = result.RowsAffected

	result = s.db.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", expiryTime).
		Delete(&models.MetaNode{})
	if result.Error != nil {
		return nil, fmt.Errorf("清理 meta_node 失败: %w", result.Error)
	}
	nodesDeleted = result.RowsAffected

	return map[string]int64{
		"meta_item": itemsDeleted,
		"meta_node": nodesDeleted,
		"total":     itemsDeleted + nodesDeleted,
	}, nil
}
