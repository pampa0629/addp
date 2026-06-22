package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type LogArchiveService struct {
	repo        *repository.LogRepository
	minioClient *minio.Client
	bucketName  string
}

func NewLogArchiveService(repo *repository.LogRepository, minioClient *minio.Client, bucketName string) *LogArchiveService {
	return &LogArchiveService{
		repo:        repo,
		minioClient: minioClient,
		bucketName:  bucketName,
	}
}

// ArchiveOldLogsToCSV 归档超过保留期的日志到MinIO (CSV格式)
func (s *LogArchiveService) ArchiveOldLogsToCSV(retentionDays int) error {
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)
	log.Printf("⏰ 开始归档 %s 之前的日志...", cutoffDate.Format("2006-01-02"))

	batchSize := 10000 // 每批处理10000条
	totalArchived := 0

	for {
		// 1. 查询需要归档的日志
		logs, err := s.repo.GetLogsBeforeDate(cutoffDate, batchSize)
		if err != nil {
			return fmt.Errorf("查询待归档日志失败: %v", err)
		}

		if len(logs) == 0 {
			break // 没有更多日志
		}

		if err := s.archiveLogBatch(logs, cutoffDate); err != nil {
			return err
		}

		totalArchived += len(logs)
	}

	if totalArchived > 0 {
		log.Printf("✅ 归档完成! 共归档 %d 条日志到 MinIO (%s bucket)", totalArchived, s.bucketName)
	} else {
		log.Printf("ℹ️  无需归档，没有超过 %d 天的日志", retentionDays)
	}

	return nil
}

func (s *LogArchiveService) archiveLogBatch(logs []models.AuditLog, cutoffDate time.Time) error {
	logsByTenant := groupAuditLogsByTenant(logs)
	tenantIDs := make([]uint, 0, len(logsByTenant))
	for tenantID := range logsByTenant {
		tenantIDs = append(tenantIDs, tenantID)
	}
	sort.Slice(tenantIDs, func(i, j int) bool { return tenantIDs[i] < tenantIDs[j] })

	for _, tenantID := range tenantIDs {
		tenantLogs := logsByTenant[tenantID]
		if len(tenantLogs) == 0 {
			continue
		}

		csvBuffer := new(bytes.Buffer)
		writer := csv.NewWriter(csvBuffer)

		// 写入CSV头
		writer.Write(auditLogArchiveCSVHeader())

		// 写入数据
		for _, logItem := range tenantLogs {
			writer.Write(auditLogArchiveCSVRow(logItem))
		}
		writer.Flush()

		archiveYear := cutoffDate.Format("2006")
		archiveMonth := cutoffDate.Format("01")
		archiveDate := cutoffDate.Format("2006-01-02")
		objectName := auditLogArchiveObjectName(tenantID, archiveYear, archiveMonth, archiveDate, tenantLogs[0].ID, tenantLogs[len(tenantLogs)-1].ID)

		_, err := s.minioClient.PutObject(
			context.Background(),
			s.bucketName,
			objectName,
			bytes.NewReader(csvBuffer.Bytes()),
			int64(csvBuffer.Len()),
			minio.PutObjectOptions{ContentType: "text/csv"},
		)
		if err != nil {
			return fmt.Errorf("上传到MinIO失败: %v", err)
		}

		log.Printf("📦 已归档 %d 条日志到 MinIO: %s/%s", len(tenantLogs), s.bucketName, objectName)
	}

	logIDs := make([]uint, len(logs))
	for i, logItem := range logs {
		logIDs[i] = logItem.ID
	}

	if err := s.repo.DeleteByIDs(logIDs); err != nil {
		return fmt.Errorf("删除已归档日志失败: %v", err)
	}
	return nil
}

func groupAuditLogsByTenant(logs []models.AuditLog) map[uint][]models.AuditLog {
	grouped := make(map[uint][]models.AuditLog)
	for _, item := range logs {
		tenantID := uint(0)
		if item.TenantID != nil {
			tenantID = *item.TenantID
		}
		grouped[tenantID] = append(grouped[tenantID], item)
	}
	return grouped
}

func auditLogArchiveObjectName(tenantID uint, archiveYear, archiveMonth, archiveDate string, firstID, lastID uint) string {
	return fmt.Sprintf(
		"tenant_%d/audit-logs/%s/%s/logs-%s-%d-%d.csv",
		tenantID,
		archiveYear,
		archiveMonth,
		archiveDate,
		firstID,
		lastID,
	)
}

func auditLogArchiveCSVHeader() []string {
	return []string{
		"id", "created_at", "user_id", "username", "tenant_id",
		"http_method", "resource_path", "http_status", "duration_ms",
		"entity_type", "entity_id",
		"request_body", "query_params", "user_agent", "ip_address",
		"log_level", "error_message", "request_id", "module_name",
	}
}

func auditLogArchiveCSVRow(logItem models.AuditLog) []string {
	return []string{
		fmt.Sprintf("%d", logItem.ID),
		logItem.CreatedAt.Format("2006-01-02 15:04:05"),
		formatPtr(logItem.UserID),
		logItem.Username,
		formatPtr(logItem.TenantID),
		logItem.HTTPMethod,
		logItem.ResourcePath,
		fmt.Sprintf("%d", logItem.HTTPStatus),
		fmt.Sprintf("%d", logItem.DurationMs),
		logItem.EntityType,
		logItem.EntityID,
		logItem.RequestBody,
		logItem.QueryParams,
		logItem.UserAgent,
		logItem.IPAddress,
		logItem.LogLevel,
		logItem.ErrorMessage,
		logItem.RequestID,
		logItem.ModuleName,
	}
}

// formatPtr NULL处理
func formatPtr(val *uint) string {
	if val == nil {
		return ""
	}
	return fmt.Sprintf("%d", *val)
}

// InitMinIOClient 初始化MinIO客户端（用于日志归档）
func InitMinIOClient(cfg *config.Config) (*minio.Client, error) {
	// 初始化MinIO客户端
	minioClient, err := minio.New(cfg.InfraMinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.InfraMinIOAccessKey, cfg.InfraMinIOSecretKey, ""),
		Secure: cfg.InfraMinIOUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %v", err)
	}

	// 确保 infra MinIO bucket 存在
	ctx := context.Background()
	bucketName := cfg.InfraMinIOBucket
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %v", err)
	}

	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %v", err)
		}
		log.Printf("✅ Created MinIO bucket: %s", bucketName)
	}

	log.Printf("✅ Infra MinIO客户端初始化成功: %s (bucket: %s)", cfg.InfraMinIOEndpoint, bucketName)
	return minioClient, nil
}
