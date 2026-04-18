package readers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	parquetgo "github.com/parquet-go/parquet-go"
)

// ParquetReader 从 S3/MinIO 读取 Parquet 文件
// 支持单文件或目录（读取目录下所有 .parquet 文件）
type ParquetReader struct {
	client     *s3.S3
	bucket     string
	prefix     string // 文件路径或目录前缀
	batchSize  int

	objectKeys   []string // 待读取的 .parquet 文件列表
	currentIndex int

	// 当前文件状态
	currentFile   *parquetgo.File
	currentRG     int    // 当前 RowGroup 索引
	currentRows   parquetgo.Rows
	fieldNames    []string
	schema        *pipeline.Schema
	rowsRead      int64
}

// NewParquetReader 创建 Parquet 读取器
func NewParquetReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	return &ParquetReader{batchSize: 1000}, nil
}

// Open 打开连接并列出文件
func (r *ParquetReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	endpoint := utils.GetStringConfig(config, "endpoint", "")
	accessKey := utils.GetStringConfig(config, "access_key", "")
	secretKey := utils.GetStringConfig(config, "secret_key", "")
	r.bucket = utils.GetStringConfig(config, "bucket", "")
	r.prefix = utils.GetStringConfig(config, "prefix", "")
	r.batchSize = utils.GetIntConfig(config, "batch_size", 1000)
	region := utils.GetStringConfig(config, "region", "us-east-1")
	useSSL := utils.GetBoolConfig(config, "use_ssl", false)

	// 支持 file_name 作为单文件路径
	if fileName := utils.GetStringConfig(config, "file_name", ""); fileName != "" {
		r.prefix = fileName
	}

	if r.bucket == "" {
		return fmt.Errorf("bucket is required")
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
	r.client = s3.New(sess)

	if err := r.listParquetFiles(ctx); err != nil {
		return fmt.Errorf("failed to list parquet files: %w", err)
	}

	if len(r.objectKeys) == 0 {
		return fmt.Errorf("no .parquet files found at prefix: %s", r.prefix)
	}

	return r.openNextFile(ctx)
}

// listParquetFiles 列出所有 .parquet 文件
func (r *ParquetReader) listParquetFiles(ctx context.Context) error {
	// 如果 prefix 直接指向一个 .parquet 文件
	if strings.HasSuffix(strings.ToLower(r.prefix), ".parquet") {
		r.objectKeys = []string{r.prefix}
		return nil
	}

	// 否则列出目录下所有 .parquet 文件
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket),
		Prefix: aws.String(r.prefix),
	}

	return r.client.ListObjectsV2PagesWithContext(ctx, input, func(page *s3.ListObjectsV2Output, _ bool) bool {
		for _, obj := range page.Contents {
			key := aws.StringValue(obj.Key)
			if strings.HasSuffix(key, "/") {
				continue
			}
			if strings.HasSuffix(strings.ToLower(key), ".parquet") {
				r.objectKeys = append(r.objectKeys, key)
			}
		}
		return true
	})
}

// openNextFile 下载并打开下一个 parquet 文件
func (r *ParquetReader) openNextFile(ctx context.Context) error {
	r.closeCurrentFile()

	if r.currentIndex >= len(r.objectKeys) {
		return io.EOF
	}

	key := r.objectKeys[r.currentIndex]
	r.currentIndex++

	// 下载到内存
	output, err := r.client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to get S3 object %s: %w", key, err)
	}
	defer output.Body.Close()

	data, err := io.ReadAll(output.Body)
	if err != nil {
		return fmt.Errorf("failed to read S3 object %s: %w", key, err)
	}

	pf, err := parquetgo.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("failed to open parquet file %s: %w", key, err)
	}

	r.currentFile = pf
	r.currentRG = 0
	r.fieldNames = extractLeafNames(pf.Schema())

	// 构建 pipeline.Schema（仅第一个文件时）
	if r.schema == nil {
		r.schema = buildPipelineSchema(pf.Schema())
	}

	return r.openNextRowGroup()
}

// openNextRowGroup 打开下一个 RowGroup
func (r *ParquetReader) openNextRowGroup() error {
	if r.currentFile == nil {
		return io.EOF
	}
	rgs := r.currentFile.RowGroups()
	if r.currentRG >= len(rgs) {
		return io.EOF
	}
	if r.currentRows != nil {
		r.currentRows.Close()
	}
	r.currentRows = rgs[r.currentRG].Rows()
	r.currentRG++
	return nil
}

// Read 读取一批数据
func (r *ParquetReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	for {
		if r.currentRows == nil {
			return nil, io.EOF
		}

		batch, err := r.readBatch()
		if err == nil && len(batch.Rows) > 0 {
			return batch, nil
		}
		if err != nil && err != io.EOF {
			return nil, err
		}

		// 当前 RowGroup 读完，尝试下一个
		if nextErr := r.openNextRowGroup(); nextErr == io.EOF {
			// 当前文件读完，尝试下一个文件
			if nextErr2 := r.openNextFile(ctx); nextErr2 == io.EOF {
				return nil, io.EOF
			} else if nextErr2 != nil {
				return nil, nextErr2
			}
		} else if nextErr != nil {
			return nil, nextErr
		}
	}
}

// readBatch 从当前 RowGroup 读取一批数据
func (r *ParquetReader) readBatch() (*pipeline.DataBatch, error) {
	buf := make([]parquetgo.Row, r.batchSize)
	n, err := r.currentRows.ReadRows(buf)

	rows := make([]map[string]interface{}, 0, n)
	for i := 0; i < n; i++ {
		row := make(map[string]interface{}, len(r.fieldNames))
		for j, val := range buf[i] {
			if j < len(r.fieldNames) {
				row[r.fieldNames[j]] = parquetValueToInterface(val)
			}
		}
		rows = append(rows, row)
	}

	batch := &pipeline.DataBatch{
		Rows:   rows,
		Schema: r.schema,
		Offset: r.rowsRead,
	}
	r.rowsRead += int64(n)

	if err == io.EOF && n == 0 {
		return batch, io.EOF
	}
	return batch, nil
}

// Schema 返回数据 schema
func (r *ParquetReader) Schema() (*pipeline.Schema, error) {
	if r.schema != nil {
		return r.schema, nil
	}
	return nil, fmt.Errorf("schema not available yet")
}

// SeekTo 跳转（简化实现：重置到开头）
func (r *ParquetReader) SeekTo(offset int64) error {
	r.currentIndex = 0
	r.rowsRead = 0
	return nil
}

// Close 关闭并清理
func (r *ParquetReader) Close() error {
	r.closeCurrentFile()
	return nil
}

// Mode 返回读取模式
func (r *ParquetReader) Mode() pipeline.ReaderMode {
	return pipeline.ModeBatch
}

func (r *ParquetReader) closeCurrentFile() {
	if r.currentRows != nil {
		r.currentRows.Close()
		r.currentRows = nil
	}
	r.currentFile = nil
}

// extractLeafNames 提取叶子列名（与 parquet.Row 中 Value 顺序对应）
func extractLeafNames(schema *parquetgo.Schema) []string {
	if schema == nil {
		return nil
	}
	fields := schema.Fields()
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Name())
	}
	return names
}

// buildPipelineSchema 从 parquet schema 构建 pipeline.Schema
func buildPipelineSchema(schema *parquetgo.Schema) *pipeline.Schema {
	if schema == nil {
		return nil
	}
	fields := schema.Fields()
	pipelineFields := make([]pipeline.Field, 0, len(fields))
	for _, f := range fields {
		pipelineFields = append(pipelineFields, pipeline.Field{
			Name:     f.Name(),
			Nullable: f.Optional(),
			Type:     mapParquetFieldType(f),
		})
	}
	return &pipeline.Schema{Fields: pipelineFields}
}

// mapParquetFieldType 将 parquet 字段类型映射到 pipeline 类型字符串
func mapParquetFieldType(f parquetgo.Field) string {
	if f.Type() == nil {
		return "string"
	}
	lt := f.Type().LogicalType()
	if lt != nil {
		switch {
		case lt.Date != nil:
			return "date"
		case lt.Timestamp != nil:
			return "timestamp"
		case lt.UTF8 != nil:
			return "string"
		case lt.Decimal != nil:
			return "decimal"
		}
	}
	switch f.Type() {
	case parquetgo.BooleanType:
		return "bool"
	case parquetgo.Int32Type:
		return "int"
	case parquetgo.Int64Type:
		return "bigint"
	case parquetgo.FloatType:
		return "float"
	case parquetgo.DoubleType:
		return "double"
	default:
		return "string"
	}
}

// parquetValueToInterface 将 parquet.Value 转换为 Go 原生类型
func parquetValueToInterface(v parquetgo.Value) interface{} {
	if v.IsNull() {
		return nil
	}
	switch v.Kind() {
	case parquetgo.Boolean:
		return v.Boolean()
	case parquetgo.Int32:
		return v.Int32()
	case parquetgo.Int64:
		return v.Int64()
	case parquetgo.Float:
		return v.Float()
	case parquetgo.Double:
		return v.Double()
	case parquetgo.ByteArray:
		return string(v.ByteArray())
	case parquetgo.FixedLenByteArray:
		return string(v.ByteArray())
	default:
		return v.String()
	}
}

// tempFileForParquet 下载 S3 对象到临时文件（大文件时使用）
func tempFileForParquet(data []byte) (string, error) {
	f, err := os.CreateTemp("", "parquet_reader_*.parquet")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
