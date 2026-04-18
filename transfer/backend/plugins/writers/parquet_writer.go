package writers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	parquetgo "github.com/parquet-go/parquet-go"
)

// ParquetWriter 将数据写入 Parquet 格式并上传到 S3/MinIO
type ParquetWriter struct {
	client   *s3.S3
	bucket   string
	prefix   string
	fileName string

	tempFile     string
	parquetFile  *os.File
	parquetWriter *parquetgo.GenericWriter[map[string]any]
	schema       *parquetgo.Schema
	fieldNames   []string // 保持字段顺序
	initialized  bool
}

// NewParquetWriter 创建 Parquet 写入器
func NewParquetWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	return &ParquetWriter{}, nil
}

// Open 打开连接
func (w *ParquetWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	endpoint := utils.GetStringConfig(config, "endpoint", "")
	accessKey := utils.GetStringConfig(config, "access_key", "")
	secretKey := utils.GetStringConfig(config, "secret_key", "")
	w.bucket = utils.GetStringConfig(config, "bucket", "")
	w.prefix = utils.GetStringConfig(config, "prefix", "")
	w.fileName = utils.GetStringConfig(config, "file_name", "output.parquet")
	region := utils.GetStringConfig(config, "region", "us-east-1")
	useSSL := utils.GetBoolConfig(config, "use_ssl", false)

	if w.bucket == "" {
		return fmt.Errorf("bucket is required")
	}

	// 确保文件名有 .parquet 扩展名
	if !strings.HasSuffix(strings.ToLower(w.fileName), ".parquet") {
		w.fileName = strings.TrimSuffix(w.fileName, filepath.Ext(w.fileName)) + ".parquet"
	}

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

	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "parquet_writer_*.parquet")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	w.tempFile = tmpFile.Name()
	w.parquetFile = tmpFile

	return nil
}

// Write 写入一批数据
func (w *ParquetWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}

	// 第一批数据时初始化 schema 和 writer
	if !w.initialized {
		if err := w.initWriter(batch); err != nil {
			return fmt.Errorf("failed to init parquet writer: %w", err)
		}
	}

	// 写入行
	rows := make([]map[string]any, 0, len(batch.Rows))
	for _, row := range batch.Rows {
		normalized := make(map[string]any, len(w.fieldNames))
		for _, name := range w.fieldNames {
			normalized[name] = normalizeParquetValue(row[name])
		}
		rows = append(rows, normalized)
	}

	if _, err := w.parquetWriter.Write(rows); err != nil {
		return fmt.Errorf("failed to write parquet rows: %w", err)
	}

	return nil
}

// Flush 刷新缓冲区
func (w *ParquetWriter) Flush(ctx context.Context) error {
	if w.parquetWriter != nil {
		return w.parquetWriter.Flush()
	}
	return nil
}

// Close 关闭并上传
func (w *ParquetWriter) Close() error {
	defer func() {
		if w.tempFile != "" {
			os.Remove(w.tempFile)
		}
	}()

	if w.parquetWriter != nil {
		if err := w.parquetWriter.Close(); err != nil {
			return fmt.Errorf("failed to close parquet writer: %w", err)
		}
	}
	if w.parquetFile != nil {
		w.parquetFile.Close()
	}

	if w.tempFile == "" || !w.initialized {
		return nil
	}

	return w.uploadToS3()
}

// initWriter 根据第一批数据初始化 parquet schema 和 writer
func (w *ParquetWriter) initWriter(batch *pipeline.DataBatch) error {
	// 从 schema 或第一行数据推断字段
	if batch.Schema != nil && len(batch.Schema.Fields) > 0 {
		w.fieldNames = make([]string, 0, len(batch.Schema.Fields))
		for _, f := range batch.Schema.Fields {
			w.fieldNames = append(w.fieldNames, f.Name)
		}
		w.schema = buildSchemaFromPipelineFields(batch.Schema.Fields)
	} else if len(batch.Rows) > 0 {
		w.fieldNames, w.schema = inferSchemaFromRow(batch.Rows[0])
	} else {
		return fmt.Errorf("cannot infer schema: no schema and no rows")
	}

	w.parquetWriter = parquetgo.NewGenericWriter[map[string]any](w.parquetFile, w.schema)
	w.initialized = true
	return nil
}

// buildSchemaFromPipelineFields 从 pipeline.Field 列表构建 parquet schema
func buildSchemaFromPipelineFields(fields []pipeline.Field) *parquetgo.Schema {
	group := parquetgo.Group{}
	for _, f := range fields {
		group[f.Name] = fieldTypeToParquetNode(f.Type)
	}
	return parquetgo.NewSchema("schema", group)
}

// inferSchemaFromRow 从第一行数据推断 parquet schema
func inferSchemaFromRow(row map[string]interface{}) ([]string, *parquetgo.Schema) {
	group := parquetgo.Group{}
	names := make([]string, 0, len(row))
	for k, v := range row {
		names = append(names, k)
		group[k] = valueTypeToParquetNode(v)
	}
	return names, parquetgo.NewSchema("schema", group)
}

// fieldTypeToParquetNode 将 pipeline 字段类型映射到 parquet 节点
func fieldTypeToParquetNode(fieldType string) parquetgo.Node {
	switch strings.ToLower(fieldType) {
	case "bool", "boolean":
		return parquetgo.Optional(parquetgo.Leaf(parquetgo.BooleanType))
	case "int", "integer", "int32":
		return parquetgo.Optional(parquetgo.Int(32))
	case "bigint", "int64", "long":
		return parquetgo.Optional(parquetgo.Int(64))
	case "float", "float32":
		return parquetgo.Optional(parquetgo.Leaf(parquetgo.FloatType))
	case "double", "float64", "decimal", "numeric":
		return parquetgo.Optional(parquetgo.Leaf(parquetgo.DoubleType))
	default:
		// string, text, json, geometry, unknown 等都用 string
		return parquetgo.Optional(parquetgo.String())
	}
}

// valueTypeToParquetNode 从 Go 值推断 parquet 节点类型
func valueTypeToParquetNode(v interface{}) parquetgo.Node {
	if v == nil {
		return parquetgo.Optional(parquetgo.String())
	}
	switch v.(type) {
	case bool:
		return parquetgo.Optional(parquetgo.Leaf(parquetgo.BooleanType))
	case int, int32:
		return parquetgo.Optional(parquetgo.Int(32))
	case int64:
		return parquetgo.Optional(parquetgo.Int(64))
	case float32:
		return parquetgo.Optional(parquetgo.Leaf(parquetgo.FloatType))
	case float64:
		return parquetgo.Optional(parquetgo.Leaf(parquetgo.DoubleType))
	default:
		return parquetgo.Optional(parquetgo.String())
	}
}

// normalizeParquetValue 将值规范化为 parquet 可接受的类型
func normalizeParquetValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string, bool, int32, int64, float32, float64:
		return val
	case int:
		return int32(val)
	case int8:
		return int32(val)
	case int16:
		return int32(val)
	case uint:
		return int64(val)
	case uint32:
		return int64(val)
	case uint64:
		return int64(val)
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return string(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// uploadToS3 上传 parquet 文件到 S3/MinIO
func (w *ParquetWriter) uploadToS3() error {
	data, err := os.ReadFile(w.tempFile)
	if err != nil {
		return fmt.Errorf("failed to read temp parquet file: %w", err)
	}

	key := filepath.ToSlash(filepath.Join(w.prefix, w.fileName))

	_, err = w.client.PutObject(&s3.PutObjectInput{
		Bucket:      aws.String(w.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		return fmt.Errorf("failed to upload parquet to S3: %w", err)
	}

	fmt.Printf("[ParquetWriter] uploaded %d bytes to s3://%s/%s\n", len(data), w.bucket, key)
	return nil
}
