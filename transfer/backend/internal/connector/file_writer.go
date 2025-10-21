package connector

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/addp/transfer/pkg/pipeline"
)

// FileWriter 文件写入器（支持 CSV、JSON、JSONL）
type FileWriter struct {
	filePath   string
	fileType   string // csv, json, jsonl
	file       *os.File
	csvWriter  *csv.Writer
	jsonEncoder *json.Encoder
	delimiter  rune
	headers    []string
	isFirstBatch bool
	jsonArray  []map[string]interface{} // 用于 JSON 数组格式
}

// NewFileWriter 创建文件写入器
func NewFileWriter() *FileWriter {
	return &FileWriter{
		delimiter:    ',',
		isFirstBatch: true,
	}
}

// Open 打开文件
func (w *FileWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	// 解析配置
	w.filePath = getStringConfig(config, "file_path", "")
	w.fileType = getStringConfig(config, "file_type", "csv")
	w.delimiter = rune(getStringConfig(config, "delimiter", ",")[0])
	overwrite := getBoolConfig(config, "overwrite", false)

	if w.filePath == "" {
		return fmt.Errorf("file_path is required")
	}

	// 确保目录存在
	dir := filepath.Dir(w.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 打开文件
	flags := os.O_CREATE | os.O_WRONLY
	if overwrite {
		flags |= os.O_TRUNC // 覆盖模式
	} else {
		flags |= os.O_APPEND // 追加模式
	}

	file, err := os.OpenFile(w.filePath, flags, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	w.file = file

	// 根据文件类型初始化写入器
	switch w.fileType {
	case "csv":
		w.csvWriter = csv.NewWriter(w.file)
		w.csvWriter.Comma = w.delimiter

	case "json":
		// JSON 数组格式需要先写入 [
		if overwrite {
			w.file.WriteString("[\n")
		}
		w.jsonArray = []map[string]interface{}{}

	case "jsonl":
		// JSON Lines 格式，每行一个 JSON
		w.jsonEncoder = json.NewEncoder(w.file)

	default:
		return fmt.Errorf("unsupported file type: %s", w.fileType)
	}

	return nil
}

// Write 写入一批数据
func (w *FileWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}

	switch w.fileType {
	case "csv":
		return w.writeCSV(batch)
	case "json":
		return w.writeJSON(batch)
	case "jsonl":
		return w.writeJSONL(batch)
	default:
		return fmt.Errorf("unsupported file type: %s", w.fileType)
	}
}

// writeCSV 写入 CSV 数据
func (w *FileWriter) writeCSV(batch *pipeline.DataBatch) error {
	// 第一次写入时，写入表头
	if w.isFirstBatch {
		if batch.Schema != nil && len(batch.Schema.Fields) > 0 {
			w.headers = make([]string, 0, len(batch.Schema.Fields))
			for _, field := range batch.Schema.Fields {
				w.headers = append(w.headers, field.Name)
			}
		} else if len(batch.Rows) > 0 {
			// 从第一行数据推断表头
			for key := range batch.Rows[0] {
				w.headers = append(w.headers, key)
			}
		}

		if len(w.headers) > 0 {
			if err := w.csvWriter.Write(w.headers); err != nil {
				return fmt.Errorf("failed to write CSV headers: %w", err)
			}
		}

		w.isFirstBatch = false
	}

	// 写入数据行
	for _, row := range batch.Rows {
		record := make([]string, len(w.headers))
		for i, header := range w.headers {
			if value, ok := row[header]; ok {
				record[i] = fmt.Sprintf("%v", value)
			}
		}

		if err := w.csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	return nil
}

// writeJSON 写入 JSON 数组数据
func (w *FileWriter) writeJSON(batch *pipeline.DataBatch) error {
	// 收集所有数据到数组
	w.jsonArray = append(w.jsonArray, batch.Rows...)
	return nil
}

// writeJSONL 写入 JSON Lines 数据
func (w *FileWriter) writeJSONL(batch *pipeline.DataBatch) error {
	for _, row := range batch.Rows {
		if err := w.jsonEncoder.Encode(row); err != nil {
			return fmt.Errorf("failed to write JSON line: %w", err)
		}
	}
	return nil
}

// Flush 刷新缓冲区
func (w *FileWriter) Flush(ctx context.Context) error {
	switch w.fileType {
	case "csv":
		w.csvWriter.Flush()
		return w.csvWriter.Error()

	case "json":
		// 写入完整的 JSON 数组
		encoder := json.NewEncoder(w.file)
		encoder.SetIndent("", "  ")

		// 先清空文件（跳过开头的 [）
		w.file.Seek(0, 0)
		w.file.WriteString("[\n")

		for i, row := range w.jsonArray {
			data, err := json.MarshalIndent(row, "  ", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}

			w.file.Write([]byte("  "))
			w.file.Write(data)

			if i < len(w.jsonArray)-1 {
				w.file.WriteString(",\n")
			} else {
				w.file.WriteString("\n")
			}
		}

		w.file.WriteString("]\n")
		return nil

	case "jsonl":
		// JSON Lines 自动刷新
		return nil
	}

	return nil
}

// Close 关闭文件
func (w *FileWriter) Close() error {
	if w.file != nil {
		// 对于 JSON 格式，关闭前需要写入结束符
		if w.fileType == "json" {
			w.Flush(context.Background())
		}

		return w.file.Close()
	}
	return nil
}
