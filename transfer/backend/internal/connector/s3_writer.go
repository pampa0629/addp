package connector

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// S3Writer S3 对象存储写入器
type S3Writer struct {
	client       *s3.S3
	bucket       string
	prefix       string
	fileType     string // json, jsonl, csv
	fileName     string
	tempFile     string
	fileWriter   pipeline.Writer // 复用 FileWriter
	uploadOnClose bool
}

// NewS3Writer 创建 S3 写入器（工厂函数）
func NewS3Writer(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	writer := &S3Writer{
		uploadOnClose: true,
	}
	return writer, nil
}

// Open 打开 S3 连接
func (w *S3Writer) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	// 解析配置
	endpoint := getStringConfig(config, "endpoint", "")
	accessKey := getStringConfig(config, "access_key", "")
	secretKey := getStringConfig(config, "secret_key", "")
	w.bucket = getStringConfig(config, "bucket", "")
	w.prefix = getStringConfig(config, "prefix", "")
	w.fileName = getStringConfig(config, "file_name", "output.json")
	w.fileType = getStringConfig(config, "file_type", "json")
	region := getStringConfig(config, "region", "us-east-1")
	useSSL := getBoolConfig(config, "use_ssl", false) // 默认不使用 SSL（适用于本地 MinIO）

	// Debug logging
	fmt.Printf("[S3Writer] Config received: %+v\n", config.Config)
	fmt.Printf("[S3Writer] Parsed: bucket=%s, prefix=%s, fileName=%s, fileType=%s\n",
		w.bucket, w.prefix, w.fileName, w.fileType)

	if w.bucket == "" {
		return fmt.Errorf("bucket is required")
	}

	// 创建 AWS Session
	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(endpoint),
		Region:           aws.String(region),
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		S3ForcePathStyle: aws.Bool(true), // MinIO 需要
		DisableSSL:       aws.Bool(!useSSL), // 根据配置决定是否使用 SSL
	})
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %w", err)
	}

	w.client = s3.New(sess)

	// 创建临时文件（使用 os.CreateTemp 确保并发安全）
	tempFile, err := os.CreateTemp("", "s3_writer_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	w.tempFile = tempFile.Name()
	tempFile.Close() // 关闭文件句柄，稍后由 FileWriter 打开

	// 使用 FileWriter 写入临时文件
	fileWriter := NewFileWriter()
	fileConfig := pipeline.ConnectorConfig{
		Config: map[string]interface{}{
			"file_path": w.tempFile,
			"file_type": w.fileType,
			"overwrite": true,
		},
	}

	if err := fileWriter.Open(ctx, fileConfig); err != nil {
		os.Remove(w.tempFile) // 清理临时文件
		return err
	}

	w.fileWriter = fileWriter

	return nil
}

// Write 写入一批数据
func (w *S3Writer) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	// 写入到临时文件
	return w.fileWriter.Write(ctx, batch)
}

// Flush 刷新缓冲区
func (w *S3Writer) Flush(ctx context.Context) error {
	// 刷新文件写入器
	if err := w.fileWriter.Flush(ctx); err != nil {
		return err
	}

	// 立即上传到 S3
	return w.uploadToS3(ctx)
}

// Close 关闭连接并清理临时文件
func (w *S3Writer) Close() error {
	// 确保清理临时文件（使用 defer 保证执行）
	defer func() {
		if w.tempFile != "" {
			if err := os.Remove(w.tempFile); err != nil {
				fmt.Printf("warning: failed to remove temp file %s: %v\n", w.tempFile, err)
			}
		}
	}()

	// 关闭文件写入器
	if w.fileWriter != nil {
		if err := w.fileWriter.Close(); err != nil {
			return err
		}
	}

	// 上传到 S3
	if w.uploadOnClose && w.tempFile != "" {
		if err := w.uploadToS3(context.Background()); err != nil {
			return fmt.Errorf("failed to upload to S3: %w", err)
		}
	}

	return nil
}

// uploadToS3 上传文件到 S3
func (w *S3Writer) uploadToS3(ctx context.Context) error {
	// 读取临时文件
	data, err := os.ReadFile(w.tempFile)
	if err != nil {
		return fmt.Errorf("failed to read temp file: %w", err)
	}

	// 构建 S3 Key
	key := filepath.Join(w.prefix, w.fileName)

	fmt.Printf("[S3Writer.uploadToS3] Uploading to S3:\n")
	fmt.Printf("  Bucket: %s\n", w.bucket)
	fmt.Printf("  Prefix: %s\n", w.prefix)
	fmt.Printf("  FileName: %s\n", w.fileName)
	fmt.Printf("  Final Key: %s\n", key)
	fmt.Printf("  Data size: %d bytes\n", len(data))

	// 上传到 S3
	_, err = w.client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})

	if err != nil {
		fmt.Printf("[S3Writer.uploadToS3] Upload FAILED: %v\n", err)
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	fmt.Printf("[S3Writer.uploadToS3] Upload SUCCESS!\n")
	return nil
}
