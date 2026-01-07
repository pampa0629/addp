package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type LogArchiveService struct {
	repo        *repository.LogRepository
	minioClient *minio.Client
}

func NewLogArchiveService(repo *repository.LogRepository, minioClient *minio.Client) *LogArchiveService {
	return &LogArchiveService{
		repo:        repo,
		minioClient: minioClient,
	}
}

// ArchiveOldLogsToCSV 归档超过保留期的日志到MinIO (CSV格式)
func (s *LogArchiveService) ArchiveOldLogsToCSV(retentionDays int) error {
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)
	log.Printf("⏰ 开始归档 %s 之前的日志...", cutoffDate.Format("2006-01-02"))

	batchSize := 10000 // 每批处理10000条
	offset := 0
	totalArchived := 0

	for {
		// 1. 查询需要归档的日志
		logs, err := s.repo.GetLogsBeforeDate(cutoffDate, offset, batchSize)
		if err != nil {
			return fmt.Errorf("查询待归档日志失败: %v", err)
		}

		if len(logs) == 0 {
			break // 没有更多日志
		}

		// 2. 转换为CSV
		csvBuffer := new(bytes.Buffer)
		writer := csv.NewWriter(csvBuffer)

		// 写入CSV头
		writer.Write([]string{
			"id", "created_at", "user_id", "username", "tenant_id",
			"http_method", "resource_path", "http_status", "duration_ms",
			"entity_type", "entity_id", "ip_address", "module_name", "log_level",
		})

		// 写入数据
		for _, logItem := range logs {
			writer.Write([]string{
				fmt.Sprintf("%d", logItem.ID),
				logItem.CreatedAt.Format("2006-01-02 15:04:05"),
				formatPtr(logItem.UserID),      // NULL处理
				logItem.Username,
				formatPtr(logItem.TenantID),    // NULL处理
				logItem.HTTPMethod,
				logItem.ResourcePath,
				fmt.Sprintf("%d", logItem.HTTPStatus),
				fmt.Sprintf("%d", logItem.DurationMs),
				logItem.EntityType,
				logItem.EntityID,
				logItem.IPAddress,
				logItem.ModuleName,
				logItem.LogLevel,
			})
		}
		writer.Flush()

		// 3. 上传到MinIO (按日期分组)
		archiveYear := cutoffDate.Format("2006")
		archiveMonth := cutoffDate.Format("01")
		archiveDate := cutoffDate.Format("2006-01-02")
		objectName := fmt.Sprintf("audit-logs/%s/%s/logs-%s.csv",
			archiveYear, archiveMonth, archiveDate)

		_, err = s.minioClient.PutObject(
			context.Background(),
			"system", // ✅ infra MinIO的system bucket
			objectName,
			bytes.NewReader(csvBuffer.Bytes()),
			int64(csvBuffer.Len()),
			minio.PutObjectOptions{ContentType: "text/csv"},
		)
		if err != nil {
			return fmt.Errorf("上传到MinIO失败: %v", err)
		}

		log.Printf("📦 已归档 %d 条日志到 MinIO: %s", len(logs), objectName)

		// 4. 删除已归档的日志
		logIDs := make([]uint, len(logs))
		for i, logItem := range logs {
			logIDs[i] = logItem.ID
		}

		err = s.repo.DeleteByIDs(logIDs)
		if err != nil {
			return fmt.Errorf("删除已归档日志失败: %v", err)
		}

		totalArchived += len(logs)
		offset += batchSize
	}

	if totalArchived > 0 {
		log.Printf("✅ 归档完成! 共归档 %d 条日志到 MinIO (system bucket)", totalArchived)
	} else {
		log.Printf("ℹ️  无需归档，没有超过 %d 天的日志", retentionDays)
	}

	return nil
}

// formatPtr NULL处理
func formatPtr(val *uint) string {
	if val == nil {
		return ""
	}
	return fmt.Sprintf("%d", *val)
}

// GetRetentionDaysFromEnv 从环境变量读取保留天数
func GetRetentionDaysFromEnv() int {
	retentionStr := os.Getenv("AUDIT_LOG_RETENTION_DAYS")
	if retentionStr == "" {
		return 90 // 默认90天
	}

	retention, err := strconv.Atoi(retentionStr)
	if err != nil {
		log.Printf("⚠️  AUDIT_LOG_RETENTION_DAYS配置无效: %s, 使用默认值90天", retentionStr)
		return 90
	}

	if retention < 1 {
		log.Printf("⚠️  AUDIT_LOG_RETENTION_DAYS必须大于0, 使用默认值90天")
		return 90
	}

	return retention
}

// IsArchiveEnabled 检查是否启用归档
func IsArchiveEnabled() bool {
	enabled := os.Getenv("AUDIT_LOG_ARCHIVE_ENABLED")
	return enabled == "true"
}

// InitMinIOClient 初始化MinIO客户端（用于日志归档）
func InitMinIOClient(cfg *config.Config) (*minio.Client, error) {
	// MinIO连接配置（直接从环境变量读取）
	minioHost := os.Getenv("MINIO_HOST")
	if minioHost == "" {
		minioHost = "localhost"
	}
	minioPort := os.Getenv("MINIO_API_PORT")
	if minioPort == "" {
		minioPort = "19000"
	}
	accessKeyID := os.Getenv("MINIO_ROOT_USER")
	if accessKeyID == "" {
		accessKeyID = "minioadmin"
	}
	secretAccessKey := os.Getenv("MINIO_ROOT_PASSWORD")
	if secretAccessKey == "" {
		secretAccessKey = "minioadmin"
	}

	endpoint := fmt.Sprintf("%s:%s", minioHost, minioPort)
	useSSL := false

	// 初始化MinIO客户端
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %v", err)
	}

	// 确保system bucket存在
	ctx := context.Background()
	bucketName := "system"
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

	log.Printf("✅ MinIO客户端初始化成功: %s (bucket: %s)", endpoint, bucketName)
	return minioClient, nil
}
