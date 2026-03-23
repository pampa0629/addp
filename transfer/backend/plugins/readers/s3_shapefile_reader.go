package readers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/storage"
	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
	"github.com/google/uuid"
)

// S3ShapefileReader 从 S3/MinIO 读取 Shapefile 的 Reader
// 工作流程：
// 1. 从 MinIO 下载所有 Shapefile 相关文件（.shp/.dbf/.shx/.prj 等）到本地临时目录
// 2. 使用 ShapefileReader 读取本地文件
// 3. 读取完成后清理临时目录
type S3ShapefileReader struct {
	localDir    string // 临时目录路径
	shapeReader pipeline.Reader
}

// NewS3ShapefileReader 创建 S3 Shapefile Reader
func NewS3ShapefileReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	return &S3ShapefileReader{}, nil
}

// Open 打开 S3 连接并下载 Shapefile 文件
func (r *S3ShapefileReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	endpoint := utils.GetStringConfig(config, "endpoint", "")
	accessKey := utils.GetStringConfig(config, "access_key", "")
	secretKey := utils.GetStringConfig(config, "secret_key", "")
	bucket := utils.GetStringConfig(config, "bucket", "")
	prefix := utils.GetStringConfig(config, "prefix", "")
	region := utils.GetStringConfig(config, "region", "us-east-1")
	batchSize := utils.GetIntConfig(config, "batch_size", 1000)
	geometryField := utils.GetStringConfig(config, "geometry_field", "geom")

	if bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if prefix == "" {
		return fmt.Errorf("prefix is required")
	}

	// 创建 MinIO 客户端
	minioClient, err := storage.NewMinIOClient(storage.MinIOConfig{
		Endpoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Region:    region,
	})
	if err != nil {
		return fmt.Errorf("failed to create MinIO client: %w", err)
	}

	// 创建临时目录
	r.localDir = filepath.Join(os.TempDir(), fmt.Sprintf("shapefile_%s", uuid.New().String()))

	// 下载所有文件
	downloadResult, err := minioClient.DownloadFiles(ctx, bucket, prefix, r.localDir)
	if err != nil {
		return fmt.Errorf("failed to download files from MinIO: %w", err)
	}

	// 查找 .shp 文件
	var shpFile string
	for _, localPath := range downloadResult.DownloadedFiles {
		if strings.HasSuffix(strings.ToLower(localPath), ".shp") {
			shpFile = localPath
			break
		}
	}
	if shpFile == "" {
		return fmt.Errorf("no .shp file found in prefix: %s", prefix)
	}

	// 验证必需文件存在
	baseName := strings.TrimSuffix(shpFile, filepath.Ext(shpFile))
	for _, ext := range []string{".dbf", ".shx"} {
		if _, err := os.Stat(baseName + ext); os.IsNotExist(err) {
			return fmt.Errorf("required shapefile component not found: %s%s", baseName, ext)
		}
	}

	// 创建并打开 ShapefileReader
	shapefileConfig := pipeline.ConnectorConfig{
		Type: "shapefile",
		Config: map[string]interface{}{
			"file_path":      shpFile,
			"geometry_field": geometryField,
		},
		BatchSize: batchSize,
	}

	shapeReader, err := NewShapefileReader(shapefileConfig)
	if err != nil {
		return fmt.Errorf("failed to create shapefile reader: %w", err)
	}
	if err := shapeReader.Open(ctx, shapefileConfig); err != nil {
		return fmt.Errorf("failed to open shapefile: %w", err)
	}

	r.shapeReader = shapeReader
	return nil
}

// Read 读取数据（委托给 ShapefileReader）
func (r *S3ShapefileReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	if r.shapeReader == nil {
		return nil, fmt.Errorf("reader not opened")
	}
	return r.shapeReader.Read(ctx)
}

// Schema 返回 Schema（委托给 ShapefileReader）
func (r *S3ShapefileReader) Schema() (*pipeline.Schema, error) {
	if r.shapeReader == nil {
		return nil, fmt.Errorf("reader not opened")
	}
	return r.shapeReader.Schema()
}

// SeekTo 跳转到指定偏移量（委托给 ShapefileReader）
func (r *S3ShapefileReader) SeekTo(offset int64) error {
	if r.shapeReader == nil {
		return fmt.Errorf("reader not opened")
	}
	return r.shapeReader.SeekTo(offset)
}

// Mode 返回读取模式
func (r *S3ShapefileReader) Mode() pipeline.ReaderMode {
	return pipeline.ModeBatch
}

// Close 关闭 Reader 并清理临时目录
func (r *S3ShapefileReader) Close() error {
	if r.shapeReader != nil {
		r.shapeReader.Close()
	}
	if r.localDir != "" {
		if err := os.RemoveAll(r.localDir); err != nil {
			return fmt.Errorf("failed to remove temp directory %s: %w", r.localDir, err)
		}
	}
	return nil
}
