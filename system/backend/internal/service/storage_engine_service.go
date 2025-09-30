package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/system/internal/models"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageEngineService struct{}

func NewStorageEngineService() *StorageEngineService {
	return &StorageEngineService{}
}

// TestConnection 测试存储引擎连接
func (s *StorageEngineService) TestConnection(resource *models.Resource) error {
	switch resource.ResourceType {
	case "postgresql":
		return s.testPostgreSQLConnection(resource.ConnectionInfo)
	case "minio", "s3":
		return s.testMinIOConnection(resource.ConnectionInfo)
	default:
		return fmt.Errorf("unsupported resource type: %s", resource.ResourceType)
	}
}

// testPostgreSQLConnection 测试 PostgreSQL 连接
func (s *StorageEngineService) testPostgreSQLConnection(connInfo models.ConnectionInfo) error {
	// 构建连接字符串
	host, _ := connInfo["host"].(string)
	port, _ := connInfo["port"].(float64)
	database, _ := connInfo["database"].(string)
	user, _ := connInfo["user"].(string)
	password, _ := connInfo["password"].(string)
	sslMode, _ := connInfo["sslmode"].(string)

	if sslMode == "" {
		sslMode = "disable"
	}

	// 验证必填字段
	if host == "" || user == "" || database == "" {
		return fmt.Errorf("missing required fields: host, user, database")
	}

	if port == 0 {
		port = 5432
	}

	// 构建 DSN
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, int(port), user, password, database, sslMode)

	// 连接数据库
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	// 设置连接超时
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 执行简单查询验证
	var version string
	err = db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return fmt.Errorf("failed to query version: %w", err)
	}

	return nil
}

// testMinIOConnection 测试 MinIO/S3 连接
func (s *StorageEngineService) testMinIOConnection(connInfo models.ConnectionInfo) error {
	// 获取连接参数
	endpoint, _ := connInfo["endpoint"].(string)
	accessKey, _ := connInfo["access_key"].(string)
	secretKey, _ := connInfo["secret_key"].(string)
	useSSL := false
	if ssl, ok := connInfo["use_ssl"].(bool); ok {
		useSSL = ssl
	}
	bucket, _ := connInfo["bucket"].(string)

	// 验证必填字段
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}

	// 初始化 MinIO 客户端
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create minio client: %w", err)
	}

	// 设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 测试连接 - 列出存储桶
	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	// 如果指定了 bucket，检查是否存在
	if bucket != "" {
		found := false
		for _, b := range buckets {
			if b.Name == bucket {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("bucket '%s' not found", bucket)
		}
	}

	return nil
}

// GetConnectionInfo 获取存储引擎连接信息（用于前端展示，隐藏敏感信息）
func (s *StorageEngineService) GetConnectionInfo(resource *models.Resource) map[string]interface{} {
	result := make(map[string]interface{})
	result["type"] = resource.ResourceType

	switch resource.ResourceType {
	case "postgresql":
		result["host"] = resource.ConnectionInfo["host"]
		result["port"] = resource.ConnectionInfo["port"]
		result["database"] = resource.ConnectionInfo["database"]
		result["user"] = resource.ConnectionInfo["user"]
		result["password"] = "******" // 隐藏密码
	case "minio", "s3":
		result["endpoint"] = resource.ConnectionInfo["endpoint"]
		result["bucket"] = resource.ConnectionInfo["bucket"]
		result["access_key"] = maskString(resource.ConnectionInfo["access_key"])
		result["secret_key"] = "******" // 隐藏密钥
		result["use_ssl"] = resource.ConnectionInfo["use_ssl"]
	}

	return result
}

// maskString 部分隐藏字符串
func maskString(value interface{}) string {
	if str, ok := value.(string); ok {
		if len(str) <= 4 {
			return "****"
		}
		return str[:4] + "****" + str[len(str)-4:]
	}
	return "****"
}