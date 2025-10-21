package connector

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
)

// FileReader 文件读取器（支持 CSV、JSON、JSONL）
type FileReader struct {
	filePath   string
	fileType   string // csv, json, jsonl
	file       *os.File
	csvReader  *csv.Reader
	jsonDecoder *json.Decoder
	scanner    *bufio.Scanner
	batchSize  int
	offset     int64
	schema     *pipeline.Schema
	headers    []string // CSV 表头
	delimiter  rune     // CSV 分隔符
}

// NewFileReader 创建文件读取器
func NewFileReader() *FileReader {
	return &FileReader{
		batchSize: 1000,
		delimiter: ',',
	}
}

// Open 打开文件
func (r *FileReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	// 解析配置
	r.filePath = getStringConfig(config, "file_path", "")
	r.fileType = getStringConfig(config, "file_type", "csv")
	r.batchSize = getIntConfig(config, "batch_size", 1000)
	r.delimiter = rune(getStringConfig(config, "delimiter", ",")[0])

	if r.filePath == "" {
		return fmt.Errorf("file_path is required")
	}

	// 检查文件是否存在
	if _, err := os.Stat(r.filePath); os.IsNotExist(err) {
		slog.Error("file not found", "path", r.filePath, "error", err)
		return fmt.Errorf("file not found: %s", r.filePath)
	}

	// 打开文件
	file, err := os.Open(r.filePath)
	if err != nil {
		slog.Error("failed to open file", "path", r.filePath, "error", err)
		return fmt.Errorf("failed to open file: %w", err)
	}
	r.file = file
	slog.Info("file opened successfully", "path", r.filePath, "type", r.fileType)

	// 根据文件类型初始化读取器
	switch r.fileType {
	case "csv":
		r.csvReader = csv.NewReader(r.file)
		r.csvReader.Comma = r.delimiter
		r.csvReader.LazyQuotes = true
		r.csvReader.TrimLeadingSpace = true

		// 读取表头
		headers, err := r.csvReader.Read()
		if err != nil {
			return fmt.Errorf("failed to read CSV headers: %w", err)
		}
		r.headers = headers

		// 构建 Schema
		r.schema = r.inferSchemaFromCSV()

	case "json":
		// JSON 数组格式 [{"id": 1}, {"id": 2}]
		r.jsonDecoder = json.NewDecoder(r.file)

		// 读取开始的 [
		t, err := r.jsonDecoder.Token()
		if err != nil {
			return fmt.Errorf("failed to read JSON start token: %w", err)
		}
		if delim, ok := t.(json.Delim); !ok || delim != '[' {
			return fmt.Errorf("expected JSON array, got: %v", t)
		}

	case "jsonl":
		// JSON Lines 格式，每行一个 JSON 对象
		r.scanner = bufio.NewScanner(r.file)

	default:
		return fmt.Errorf("unsupported file type: %s", r.fileType)
	}

	return nil
}

// Read 读取一批数据
func (r *FileReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	switch r.fileType {
	case "csv":
		return r.readCSV(ctx)
	case "json":
		return r.readJSON(ctx)
	case "jsonl":
		return r.readJSONL(ctx)
	default:
		return nil, fmt.Errorf("unsupported file type: %s", r.fileType)
	}
}

// readCSV 读取 CSV 数据
func (r *FileReader) readCSV(ctx context.Context) (*pipeline.DataBatch, error) {
	var rows []map[string]interface{}
	count := 0

	for count < r.batchSize {
		record, err := r.csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV record: %w", err)
		}

		// 将 CSV 行转换为 map
		row := make(map[string]interface{})
		for i, value := range record {
			if i < len(r.headers) {
				// 尝试推断类型
				row[r.headers[i]] = inferValue(value)
			}
		}

		rows = append(rows, row)
		count++
		r.offset++
	}

	if len(rows) == 0 {
		return nil, io.EOF
	}

	return &pipeline.DataBatch{
		Rows:   rows,
		Schema: r.schema,
		Offset: r.offset - int64(len(rows)),
	}, nil
}

// readJSON 读取 JSON 数组数据
func (r *FileReader) readJSON(ctx context.Context) (*pipeline.DataBatch, error) {
	var rows []map[string]interface{}
	count := 0

	for count < r.batchSize && r.jsonDecoder.More() {
		var row map[string]interface{}
		if err := r.jsonDecoder.Decode(&row); err != nil {
			return nil, fmt.Errorf("failed to decode JSON: %w", err)
		}

		rows = append(rows, row)
		count++
		r.offset++
	}

	if len(rows) == 0 {
		return nil, io.EOF
	}

	// 从第一行推断 Schema
	if r.schema == nil && len(rows) > 0 {
		r.schema = r.inferSchemaFromJSON(rows[0])
	}

	return &pipeline.DataBatch{
		Rows:   rows,
		Schema: r.schema,
		Offset: r.offset - int64(len(rows)),
	}, nil
}

// readJSONL 读取 JSON Lines 数据
func (r *FileReader) readJSONL(ctx context.Context) (*pipeline.DataBatch, error) {
	var rows []map[string]interface{}
	count := 0

	for count < r.batchSize && r.scanner.Scan() {
		line := r.scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var row map[string]interface{}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("failed to parse JSON line: %w", err)
		}

		rows = append(rows, row)
		count++
		r.offset++
	}

	if err := r.scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	if len(rows) == 0 {
		return nil, io.EOF
	}

	// 从第一行推断 Schema
	if r.schema == nil && len(rows) > 0 {
		r.schema = r.inferSchemaFromJSON(rows[0])
	}

	return &pipeline.DataBatch{
		Rows:   rows,
		Schema: r.schema,
		Offset: r.offset - int64(len(rows)),
	}, nil
}

// Schema 返回数据 Schema
func (r *FileReader) Schema() (*pipeline.Schema, error) {
	if r.schema == nil {
		return nil, fmt.Errorf("schema not available yet")
	}
	return r.schema, nil
}

// SeekTo 跳转到指定位置（文件不支持高效的 seek）
func (r *FileReader) SeekTo(offset int64) error {
	// 对于文件，我们需要重新打开并跳过前面的行
	// 这不是最优的，但对于小文件可以接受
	if offset == 0 {
		return nil
	}

	// 关闭当前文件
	r.Close()

	// 重新打开
	config := pipeline.ConnectorConfig{
		Config: map[string]interface{}{
			"file_path":  r.filePath,
			"file_type":  r.fileType,
			"batch_size": r.batchSize,
			"delimiter":  string(r.delimiter),
		},
		BatchSize: r.batchSize,
	}

	if err := r.Open(context.Background(), config); err != nil {
		return err
	}

	// 跳过前 offset 行
	for i := int64(0); i < offset; i++ {
		batch, err := r.Read(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if batch == nil || len(batch.Rows) == 0 {
			break
		}
	}

	return nil
}

// Close 关闭文件
func (r *FileReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// Mode 返回读取模式
func (r *FileReader) Mode() pipeline.ReaderMode {
	return pipeline.ModeBatch
}

// inferSchemaFromCSV 从 CSV 表头推断 Schema
func (r *FileReader) inferSchemaFromCSV() *pipeline.Schema {
	var fields []pipeline.Field
	for _, header := range r.headers {
		fields = append(fields, pipeline.Field{
			Name: header,
			Type: "string", // CSV 默认都是字符串
		})
	}

	return &pipeline.Schema{
		Fields: fields,
		Metadata: map[string]interface{}{
			"source_file": filepath.Base(r.filePath),
		},
	}
}

// inferSchemaFromJSON 从 JSON 对象推断 Schema
func (r *FileReader) inferSchemaFromJSON(row map[string]interface{}) *pipeline.Schema {
	var fields []pipeline.Field
	for key, value := range row {
		fieldType := "string"
		switch value.(type) {
		case int, int64, float64:
			fieldType = "number"
		case bool:
			fieldType = "boolean"
		}

		fields = append(fields, pipeline.Field{
			Name: key,
			Type: fieldType,
		})
	}

	return &pipeline.Schema{
		Fields: fields,
		Metadata: map[string]interface{}{
			"source_file": filepath.Base(r.filePath),
		},
	}
}

// inferValue 尝试推断字符串的实际类型
func inferValue(s string) interface{} {
	s = strings.TrimSpace(s)

	// 尝试解析为数字
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	// 尝试解析为布尔值
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}

	// 默认返回字符串
	return s
}
