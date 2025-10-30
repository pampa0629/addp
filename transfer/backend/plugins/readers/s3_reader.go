package readers

import (
	"github.com/addp/transfer/plugins/utils"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// S3Reader S3 对象存储读取器
type S3Reader struct {
	client       *s3.S3
	bucket       string
	prefix       string
	pattern      string // 文件匹配模式（如 *.json）
	fileType     string // json, jsonl, csv
	batchSize    int
	offset       int64
	currentFile  string
	fileReader   pipeline.Reader // 复用 FileReader
	objectKeys   []string        // 待处理的对象列表
	currentIndex int
	tempFiles    []string        // 临时文件列表，用于清理
}

// NewS3Reader 创建 S3 读取器
func NewS3Reader() *S3Reader {
	return &S3Reader{
		batchSize: 1000,
	}
}

// Open 打开 S3 连接
func (r *S3Reader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	// 解析配置
	endpoint := utils.GetStringConfig(config, "endpoint", "")
	accessKey := utils.GetStringConfig(config, "access_key", "")
	secretKey := utils.GetStringConfig(config, "secret_key", "")
	r.bucket = utils.GetStringConfig(config, "bucket", "")
	r.prefix = utils.GetStringConfig(config, "prefix", "")
	r.pattern = utils.GetStringConfig(config, "pattern", "*")
	r.fileType = utils.GetStringConfig(config, "file_type", "json")
	r.batchSize = utils.GetIntConfig(config, "batch_size", 1000)
	region := utils.GetStringConfig(config, "region", "us-east-1")

	if r.bucket == "" {
		return fmt.Errorf("bucket is required")
	}

	// 创建 AWS Session
	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(endpoint),
		Region:           aws.String(region),
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		S3ForcePathStyle: aws.Bool(true), // MinIO 需要
	})
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %w", err)
	}

	r.client = s3.New(sess)

	// 列出所有匹配的对象
	if err := r.listObjects(ctx); err != nil {
		return fmt.Errorf("failed to list S3 objects: %w", err)
	}

	if len(r.objectKeys) == 0 {
		return fmt.Errorf("no objects found with prefix: %s", r.prefix)
	}

	// 打开第一个文件
	return r.openNextFile(ctx)
}

// listObjects 列出所有匹配的对象
func (r *S3Reader) listObjects(ctx context.Context) error {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket),
		Prefix: aws.String(r.prefix),
	}

	err := r.client.ListObjectsV2PagesWithContext(ctx, input, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, obj := range page.Contents {
			key := aws.StringValue(obj.Key)

			// 跳过目录
			if strings.HasSuffix(key, "/") {
				continue
			}

			// 匹配文件模式
			if r.matchPattern(key, r.pattern) {
				r.objectKeys = append(r.objectKeys, key)
			}
		}
		return true
	})

	return err
}

// matchPattern 简单的文件名匹配
func (r *S3Reader) matchPattern(filename, pattern string) bool {
	if pattern == "*" {
		return true
	}

	// 简单的后缀匹配
	if strings.HasPrefix(pattern, "*.") {
		ext := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(filename, ext)
	}

	// 精确匹配
	return filename == pattern
}

// openNextFile 打开下一个文件
func (r *S3Reader) openNextFile(ctx context.Context) error {
	if r.currentIndex >= len(r.objectKeys) {
		return io.EOF
	}

	key := r.objectKeys[r.currentIndex]
	r.currentFile = key
	r.currentIndex++

	// 创建临时文件（使用 os.CreateTemp 确保并发安全）
	tempFile, err := os.CreateTemp("", "s3_reader_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close() // 关闭文件句柄，稍后由 FileReader 打开

	// 记录临时文件路径用于清理
	r.tempFiles = append(r.tempFiles, tempPath)

	output, err := r.client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		os.Remove(tempPath) // 清理临时文件
		return fmt.Errorf("failed to get S3 object %s: %w", key, err)
	}
	defer output.Body.Close()

	// 根据文件类型创建对应的 Reader
	switch r.fileType {
	case "json", "jsonl", "csv":
		// 使用 FileReader 处理
		fileReader := NewFileReader()
		fileConfig := pipeline.ConnectorConfig{
			Config: map[string]interface{}{
				"file_path":  tempPath,
				"file_type":  r.fileType,
				"batch_size": r.batchSize,
			},
			BatchSize: r.batchSize,
		}

		// 先下载到本地临时文件
		if err := r.downloadToFile(output.Body, tempPath); err != nil {
			os.Remove(tempPath) // 清理临时文件
			return err
		}

		if err := fileReader.Open(ctx, fileConfig); err != nil {
			os.Remove(tempPath) // 清理临时文件
			return err
		}

		r.fileReader = fileReader

	default:
		os.Remove(tempPath) // 清理临时文件
		return fmt.Errorf("unsupported file type: %s", r.fileType)
	}

	return nil
}

// downloadToFile 下载到本地文件
func (r *S3Reader) downloadToFile(body io.Reader, filePath string) error {
	// 这里简化处理，实际应该使用流式处理避免内存占用
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// Read 读取一批数据
func (r *S3Reader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	for {
		// 从当前文件读取
		if r.fileReader != nil {
			batch, err := r.fileReader.Read(ctx)
			if err == nil {
				return batch, nil
			}

			if err != io.EOF {
				return nil, err
			}

			// 当前文件读完，关闭
			r.fileReader.Close()
			r.fileReader = nil
		}

		// 打开下一个文件
		if err := r.openNextFile(ctx); err != nil {
			return nil, err
		}
	}
}

// Schema 返回数据 Schema
func (r *S3Reader) Schema() (*pipeline.Schema, error) {
	if r.fileReader != nil {
		return r.fileReader.Schema()
	}
	return nil, fmt.Errorf("no active file reader")
}

// SeekTo 跳转到指定位置
func (r *S3Reader) SeekTo(offset int64) error {
	// 简化实现：重新开始
	r.currentIndex = 0
	r.offset = 0
	return nil
}

// Close 关闭连接并清理所有临时文件
func (r *S3Reader) Close() error {
	// 关闭当前文件读取器
	if r.fileReader != nil {
		r.fileReader.Close()
	}

	// 清理所有临时文件
	for _, tempPath := range r.tempFiles {
		if err := os.Remove(tempPath); err != nil {
			// 记录错误但继续清理其他文件
			fmt.Printf("warning: failed to remove temp file %s: %v\n", tempPath, err)
		}
	}
	r.tempFiles = nil

	return nil
}

// Mode 返回读取模式
func (r *S3Reader) Mode() pipeline.ReaderMode {
	return pipeline.ModeBatch
}
