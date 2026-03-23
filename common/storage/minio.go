package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// MinIOClient MinIO 客户端封装
type MinIOClient struct {
	client *s3.S3
}

// MinIOConfig MinIO 连接配置
type MinIOConfig struct {
	Endpoint  string // MinIO 端点 (如 http://localhost:19000)
	AccessKey string // 访问密钥
	SecretKey string // 密钥
	Region    string // 区域 (默认 us-east-1)
}

// NewMinIOClient 创建 MinIO 客户端
func NewMinIOClient(config MinIOConfig) (*MinIOClient, error) {
	if config.Region == "" {
		config.Region = "us-east-1"
	}

	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(config.Endpoint),
		Region:           aws.String(config.Region),
		Credentials:      credentials.NewStaticCredentials(config.AccessKey, config.SecretKey, ""),
		S3ForcePathStyle: aws.Bool(true), // MinIO 需要路径风格
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &MinIOClient{
		client: s3.New(sess),
	}, nil
}

// DownloadFilesResult 下载结果
type DownloadFilesResult struct {
	LocalDir      string            // 本地目录路径
	DownloadedFiles map[string]string // key: 对象键, value: 本地文件路径
}

// DownloadFiles 下载指定前缀下的所有文件到本地目录
// bucket: 桶名
// prefix: 对象前缀 (如 "temp/uuid123/")
// localDir: 本地目录路径 (如 "/tmp/shapefile_uuid123")
// 返回: 下载结果和错误
func (c *MinIOClient) DownloadFiles(ctx context.Context, bucket, prefix, localDir string) (*DownloadFilesResult, error) {
	// 确保前缀以 / 结尾
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	// 创建本地目录
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create local directory: %w", err)
	}

	// 列出所有对象
	objects, err := c.listObjects(ctx, bucket, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("no objects found with prefix: %s", prefix)
	}

	result := &DownloadFilesResult{
		LocalDir:        localDir,
		DownloadedFiles: make(map[string]string),
	}

	// 下载每个对象
	for _, key := range objects {
		// 计算相对路径 (去掉前缀)
		relativePath := strings.TrimPrefix(key, prefix)
		if relativePath == "" {
			continue // 跳过目录本身
		}

		// 本地文件路径
		localPath := filepath.Join(localDir, relativePath)

		// 确保父目录存在
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create parent directory for %s: %w", localPath, err)
		}

		// 下载文件
		if err := c.downloadFile(ctx, bucket, key, localPath); err != nil {
			return nil, fmt.Errorf("failed to download %s: %w", key, err)
		}

		result.DownloadedFiles[key] = localPath
	}

	return result, nil
}

// listObjects 列出指定前缀下的所有对象键
func (c *MinIOClient) listObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	var keys []string
	err := c.client.ListObjectsV2PagesWithContext(ctx, input, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, obj := range page.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
		return true // 继续分页
	})

	if err != nil {
		return nil, err
	}

	return keys, nil
}

// downloadFile 下载单个文件
func (c *MinIOClient) downloadFile(ctx context.Context, bucket, key, localPath string) error {
	// 获取对象
	result, err := c.client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to get object: %w", err)
	}
	defer result.Body.Close()

	// 创建本地文件
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer file.Close()

	// 复制内容
	if _, err := io.Copy(file, result.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// DownloadSingleFile 下载单个文件
// bucket: 桶名
// key: 对象键
// localPath: 本地文件路径
func (c *MinIOClient) DownloadSingleFile(ctx context.Context, bucket, key, localPath string) error {
	// 确保父目录存在
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	return c.downloadFile(ctx, bucket, key, localPath)
}
