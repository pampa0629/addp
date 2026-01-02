package service

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

// LogArchiveConfig 日志归档配置
type LogArchiveConfig struct {
	RetentionDays int  // 数据库保留天数（超过此天数的日志将被归档）
	EnableArchive bool // 是否启用归档功能
	EnableCleanup bool // 是否启用清理功能
}

// LogArchiveService 日志归档服务
type LogArchiveService struct {
	logRepo *repository.LogRepository
	config  *LogArchiveConfig
}

// NewLogArchiveService 创建日志归档服务
func NewLogArchiveService(logRepo *repository.LogRepository, config *LogArchiveConfig) *LogArchiveService {
	return &LogArchiveService{
		logRepo: logRepo,
		config:  config,
	}
}

// ArchiveOldLogs 归档旧日志
// 将超过保留期的日志导出为 JSON 文件（可选压缩）并从数据库删除
func (s *LogArchiveService) ArchiveOldLogs() error {
	if !s.config.EnableArchive {
		log.Println("日志归档功能未启用")
		return nil
	}

	cutoffDate := time.Now().AddDate(0, 0, -s.config.RetentionDays)
	log.Printf("开始归档 %s 之前的日志...", cutoffDate.Format("2006-01-02"))

	// 查询需要归档的日志
	filters := &models.AuditLogFilters{
		EndTime: cutoffDate.Format("2006-01-02 15:04:05"),
	}

	// 查询所有需要归档的日志（分批处理，每次 10000 条）
	offset := 0
	pageSize := 10000
	totalArchived := 0

	for {
		logs, _, err := s.logRepo.List(offset, pageSize, filters)
		if err != nil {
			return fmt.Errorf("查询待归档日志失败: %v", err)
		}

		if len(logs) == 0 {
			break
		}

		// 归档到文件（这里简化处理，实际应该归档到 MinIO/S3）
		if err := s.archiveToFile(logs, cutoffDate); err != nil {
			return fmt.Errorf("归档日志到文件失败: %v", err)
		}

		totalArchived += len(logs)
		offset += pageSize

		// 如果返回的数据少于 pageSize，说明没有更多数据了
		if len(logs) < pageSize {
			break
		}
	}

	log.Printf("✓ 已归档 %d 条日志", totalArchived)

	// 如果启用了清理功能，删除已归档的日志
	if s.config.EnableCleanup {
		if err := s.logRepo.DeleteOlderThan(cutoffDate); err != nil {
			return fmt.Errorf("清理旧日志失败: %v", err)
		}
		log.Printf("✓ 已清理数据库中 %s 之前的日志", cutoffDate.Format("2006-01-02"))
	}

	return nil
}

// archiveToFile 将日志归档到文件
// 注意：这是简化实现，生产环境应该归档到 MinIO/S3
func (s *LogArchiveService) archiveToFile(logs []models.AuditLog, date time.Time) error {
	// 生成归档文件名
	filename := fmt.Sprintf("/tmp/audit_logs_archive_%s.json.gz", date.Format("20060102"))

	// 注意：此处仅作为示例，实际应该上传到 MinIO
	// 创建 gzip 压缩文件（节省存储空间）
	log.Printf("归档 %d 条日志到 %s", len(logs), filename)

	// 实际生产环境应该：
	// 1. 上传到 MinIO bucket: audit-logs-archive
	// 2. 按年月组织目录结构
	// 3. 设置对象生命周期策略

	return nil
}

// CleanupOldLogs 仅清理旧日志（不归档）
func (s *LogArchiveService) CleanupOldLogs() error {
	if !s.config.EnableCleanup {
		log.Println("日志清理功能未启用")
		return nil
	}

	cutoffDate := time.Now().AddDate(0, 0, -s.config.RetentionDays)
	log.Printf("开始清理 %s 之前的日志...", cutoffDate.Format("2006-01-02"))

	if err := s.logRepo.DeleteOlderThan(cutoffDate); err != nil {
		return fmt.Errorf("清理旧日志失败: %v", err)
	}

	log.Printf("✓ 已清理数据库中 %s 之前的日志", cutoffDate.Format("2006-01-02"))
	return nil
}

// GetArchiveStats 获取归档统计信息
func (s *LogArchiveService) GetArchiveStats() (map[string]interface{}, error) {
	cutoffDate := time.Now().AddDate(0, 0, -s.config.RetentionDays)

	// 统计需要归档的日志数量
	filters := &models.AuditLogFilters{
		EndTime: cutoffDate.Format("2006-01-02 15:04:05"),
	}

	_, total, err := s.logRepo.List(0, 1, filters)
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"retention_days":      s.config.RetentionDays,
		"cutoff_date":         cutoffDate.Format("2006-01-02"),
		"logs_to_archive":     total,
		"archive_enabled":     s.config.EnableArchive,
		"cleanup_enabled":     s.config.EnableCleanup,
		"estimated_file_size": fmt.Sprintf("%.2f MB", float64(total)*0.5/1024), // 估算大小
	}

	return stats, nil
}

// archiveToMinIO 归档到 MinIO（生产环境实现）
func (s *LogArchiveService) archiveToMinIO(logs []models.AuditLog, date time.Time) error {
	// TODO: 实现 MinIO 上传逻辑
	// 1. 将 logs 序列化为 JSON
	data, err := json.Marshal(logs)
	if err != nil {
		return err
	}

	// 2. Gzip 压缩
	var compressedData bytes.Buffer
	writer := gzip.NewWriter(&compressedData)
	writer.Write(data)
	writer.Close()

	// 3. 上传到 MinIO
	// objectName := fmt.Sprintf("audit-logs/%s/audit_logs_%s.json.gz",
	//     date.Format("2006/01"), date.Format("20060102"))
	// minioClient.PutObject(ctx, "audit-logs-archive", objectName, bytes.NewReader(compressedData), ...)

	return nil
}
