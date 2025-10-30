package writers

import (
	"github.com/addp/transfer/plugins/utils"
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// S3Writer S3 对象存储写入器
type S3Writer struct {
	client         *s3.S3
	bucket         string
	prefix         string
	fileType       string // 用户配置的类型：csv、csv-wkt、geojson、shapefile...
	writerFileType string // 实际写入实现: csv、json、geojson、shapefile
	fileName       string

	tempFile      string
	tempDir       string
	shapefileBase string

	geometryFields []string
	geometryField  string
	spatialFormat  string

	fileWriter    pipeline.Writer
	uploadOnClose bool
}

// NewS3Writer 创建 S3 写入器（工厂函数）
func NewS3Writer(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	return &S3Writer{
		uploadOnClose: true,
	}, nil
}

// Open 打开 S3 连接
func (w *S3Writer) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	endpoint := utils.GetStringConfig(config, "endpoint", "")
	accessKey := utils.GetStringConfig(config, "access_key", "")
	secretKey := utils.GetStringConfig(config, "secret_key", "")
	w.bucket = utils.GetStringConfig(config, "bucket", "")
	w.prefix = utils.GetStringConfig(config, "prefix", "")
	w.fileName = utils.GetStringConfig(config, "file_name", "output.json")
	w.fileType = strings.ToLower(utils.GetStringConfig(config, "file_type", "json"))
	region := utils.GetStringConfig(config, "region", "us-east-1")
	useSSL := utils.GetBoolConfig(config, "use_ssl", false)

	if w.bucket == "" {
		return fmt.Errorf("bucket is required")
	}

	// 解析空间字段配置
	w.geometryFields = utils.GetStringSliceConfig(config, "geometry_fields")
	geometryField := strings.TrimSpace(utils.GetStringConfig(config, "geometry_field", ""))
	if geometryField != "" {
		w.geometryField = geometryField
	}
	if w.geometryField == "" && len(w.geometryFields) > 0 {
		w.geometryField = w.geometryFields[0]
	}
	if w.geometryField == "" {
		w.geometryField = "geometry"
	}
	w.spatialFormat = strings.ToLower(utils.GetStringConfig(config, "spatial_format", ""))

	// 标准化写入类型
	switch w.fileType {
	case "csv-wkt":
		w.writerFileType = "csv"
	case "geojson", "json", "jsonl", "csv", "parquet":
		w.writerFileType = w.fileType
	case "shapefile":
		w.writerFileType = "shapefile"
	default:
		w.writerFileType = w.fileType
	}

	// Shapefile 上传时默认使用 zip 扩展名（保留目录结构）
	if w.fileType == "shapefile" && !strings.HasSuffix(strings.ToLower(w.fileName), ".zip") {
		dir := filepath.Dir(w.fileName)
		if dir == "." {
			dir = ""
		}
		base := strings.TrimSuffix(filepath.Base(w.fileName), filepath.Ext(w.fileName))
		if base == "" {
			base = "dataset"
		}
		newName := base + ".zip"
		if dir != "" {
			w.fileName = filepath.Join(dir, newName)
		} else {
			w.fileName = newName
		}
	}

	// 创建 AWS Session
	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(endpoint),
		Region:           aws.String(region),
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		S3ForcePathStyle: aws.Bool(true),
		DisableSSL:       aws.Bool(!useSSL),
	})
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %w", err)
	}
	w.client = s3.New(sess)

	fmt.Printf("[S3Writer] Config received: %+v\n", config.Config)
	fmt.Printf("[S3Writer] Parsed: bucket=%s, prefix=%s, fileName=%s, fileType=%s, writerFileType=%s\n",
		w.bucket, w.prefix, w.fileName, w.fileType, w.writerFileType)

	switch w.writerFileType {
	case "shapefile":
		return w.openShapefileWriter(ctx, config)
	case "geojson":
		return w.openGeoJSONWriter(ctx, config)
	default:
		return w.openFileWriter(ctx, config)
	}
}

func (w *S3Writer) openFileWriter(ctx context.Context, config pipeline.ConnectorConfig) error {
	tempFile, err := os.CreateTemp("", "s3_writer_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	w.tempFile = tempFile.Name()
	tempFile.Close()

	fileConfig := pipeline.ConnectorConfig{
		Config: map[string]interface{}{
			"file_path": w.tempFile,
			"file_type": w.writerFileType,
			"overwrite": true,
		},
	}

	if delimiter, ok := config.Config["delimiter"]; ok {
		fileConfig.Config["delimiter"] = delimiter
	}

	fileWriter := NewFileWriter()
	if err := fileWriter.Open(ctx, fileConfig); err != nil {
		os.Remove(w.tempFile)
		return err
	}

	w.fileWriter = fileWriter
	return nil
}

func (w *S3Writer) openGeoJSONWriter(ctx context.Context, config pipeline.ConnectorConfig) error {
	tempFile, err := os.CreateTemp("", "s3_writer_geojson_*.geojson")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	w.tempFile = tempFile.Name()
	tempFile.Close()

	fileConfig := pipeline.ConnectorConfig{
		Config: map[string]interface{}{
			"file_path":      w.tempFile,
			"geometry_field": w.geometryField,
			"pretty":         utils.GetBoolConfig(config, "pretty", false),
		},
	}

	geoWriter, err := NewGeoJSONWriter(fileConfig)
	if err != nil {
		os.Remove(w.tempFile)
		return err
	}

	if err := geoWriter.Open(ctx, fileConfig); err != nil {
		os.Remove(w.tempFile)
		return err
	}

	w.fileWriter = geoWriter
	return nil
}

func (w *S3Writer) openShapefileWriter(ctx context.Context, config pipeline.ConnectorConfig) error {
	tempDir, err := os.MkdirTemp("", "s3_writer_shapefile_*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	w.tempDir = tempDir

	baseName := strings.TrimSuffix(filepath.Base(w.fileName), filepath.Ext(w.fileName))
	if baseName == "" {
		baseName = "dataset"
	}
	shapefilePath := filepath.Join(tempDir, baseName+".shp")
	w.shapefileBase = strings.TrimSuffix(shapefilePath, ".shp")

	fileConfig := pipeline.ConnectorConfig{
		Config: map[string]interface{}{
			"file_path":      shapefilePath,
			"geometry_field": w.geometryField,
		},
		BatchSize: config.BatchSize,
	}

	writer, err := NewShapefileWriter(fileConfig)
	if err != nil {
		os.RemoveAll(tempDir)
		return err
	}

	if err := writer.Open(ctx, fileConfig); err != nil {
		os.RemoveAll(tempDir)
		return err
	}

	w.fileWriter = writer
	return nil
}

// Write 写入一批数据
func (w *S3Writer) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	return w.fileWriter.Write(ctx, batch)
}

// Flush 刷新缓冲区
func (w *S3Writer) Flush(ctx context.Context) error {
	if err := w.fileWriter.Flush(ctx); err != nil {
		return err
	}

	// Shapefile 上传延迟到 Close 阶段，确保所有文件写入并关闭
	if w.fileType == "shapefile" {
		return nil
	}
	return w.uploadToS3(ctx)
}

// Close 关闭连接并清理临时文件
func (w *S3Writer) Close() error {
	defer func() {
		if w.tempFile != "" {
			if err := os.Remove(w.tempFile); err != nil {
				fmt.Printf("warning: failed to remove temp file %s: %v\n", w.tempFile, err)
			}
		}
		if w.tempDir != "" {
			if err := os.RemoveAll(w.tempDir); err != nil {
				fmt.Printf("warning: failed to remove temp dir %s: %v\n", w.tempDir, err)
			}
		}
	}()

	if w.fileWriter != nil {
		if err := w.fileWriter.Close(); err != nil {
			return err
		}
	}

	if !w.uploadOnClose {
		return nil
	}

	if w.fileType == "shapefile" {
		if err := w.uploadToS3(context.Background()); err != nil {
			return fmt.Errorf("failed to upload shapefile to S3: %w", err)
		}
		return nil
	}

	if w.tempFile != "" {
		if err := w.uploadToS3(context.Background()); err != nil {
			return fmt.Errorf("failed to upload file to S3: %w", err)
		}
	}

	return nil
}

// uploadToS3 上传文件到 S3
func (w *S3Writer) uploadToS3(ctx context.Context) error {
	var (
		data []byte
		err  error
	)

	if w.fileType == "shapefile" {
		data, err = w.buildShapefileArchive()
		if err != nil {
			return err
		}
	} else {
		if w.tempFile == "" {
			return fmt.Errorf("temp file is empty")
		}
		data, err = os.ReadFile(w.tempFile)
		if err != nil {
			return fmt.Errorf("failed to read temp file: %w", err)
		}
	}

	key := filepath.ToSlash(filepath.Join(w.prefix, w.fileName))

	fmt.Printf("[S3Writer.uploadToS3] Uploading to S3:\n")
	fmt.Printf("  Bucket: %s\n", w.bucket)
	fmt.Printf("  Prefix: %s\n", w.prefix)
	fmt.Printf("  FileName: %s\n", w.fileName)
	fmt.Printf("  Final Key: %s\n", key)
	fmt.Printf("  Data size: %d bytes\n", len(data))

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

func (w *S3Writer) buildShapefileArchive() ([]byte, error) {
	if w.shapefileBase == "" {
		return nil, fmt.Errorf("shapefile base path not initialized")
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	components := []string{"shp", "shx", "dbf", "prj", "cpg"}
	filesIncluded := 0

	for _, ext := range components {
		path := w.shapefileBase + "." + ext
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := addFileToZip(zipWriter, path, filepath.Base(path)); err != nil {
			zipWriter.Close()
			return nil, fmt.Errorf("failed to add %s to zip: %w", path, err)
		}
		filesIncluded++
	}

	if filesIncluded == 0 {
		zipWriter.Close()
		return nil, fmt.Errorf("no shapefile components found for base %s", w.shapefileBase)
	}

	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize shapefile zip: %w", err)
	}

	return buf.Bytes(), nil
}

func addFileToZip(zipWriter *zip.Writer, filePath, entryName string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer, err := zipWriter.Create(entryName)
	if err != nil {
		return err
	}

	if _, err := io.Copy(writer, file); err != nil {
		return err
	}

	return nil
}
