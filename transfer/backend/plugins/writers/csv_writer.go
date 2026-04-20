package writers

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/pkg/vfs"
)

// CSVWriter writes data to CSV files
type CSVWriter struct {
	csvWriter   *csv.Writer
	file        io.WriteCloser
	schema      *pipeline.Schema
	writeHeader bool
	nullValue   string
	rowCount    int64
	firstBatch  bool
	vfs         vfs.VFS // nil 时使用本地文件系统
}

// NewCSVWriter creates a CSV writer factory function
func NewCSVWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	return &CSVWriter{
		writeHeader: true,
		firstBatch:  true,
	}, nil
}

// Open opens the output CSV file
func (w *CSVWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	cfg := config.Config

	filePath, ok := cfg["file_path"].(string)
	if !ok || filePath == "" {
		return fmt.Errorf("file_path is required")
	}

	if writeHeader, ok := cfg["write_header"].(bool); ok {
		w.writeHeader = writeHeader
	}
	if nullValue, ok := cfg["null_value"].(string); ok {
		w.nullValue = nullValue
	}

	fs := w.vfs
	if fs == nil {
		fs = &vfs.LocalVFS{}
	}

	file, err := fs.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	w.file = file

	csvWriter := csv.NewWriter(file)

	if delimiter, ok := cfg["delimiter"].(string); ok && len(delimiter) > 0 {
		csvWriter.Comma = rune(delimiter[0])
	} else {
		csvWriter.Comma = ','
	}

	if crlf, ok := cfg["crlf"].(bool); ok {
		csvWriter.UseCRLF = crlf
	}

	w.csvWriter = csvWriter

	return nil
}

// Write writes a batch of records
func (w *CSVWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if w.csvWriter == nil {
		return fmt.Errorf("writer not opened")
	}

	if w.schema == nil && batch.Schema != nil {
		w.schema = batch.Schema
	}

	if w.schema == nil {
		return fmt.Errorf("schema not available in batch")
	}

	if w.firstBatch && w.writeHeader {
		headers := make([]string, len(w.schema.Fields))
		for i, field := range w.schema.Fields {
			headers[i] = field.Name
		}
		if err := w.csvWriter.Write(headers); err != nil {
			return fmt.Errorf("failed to write CSV header: %w", err)
		}
		w.firstBatch = false
	}

	for _, record := range batch.Rows {
		row := make([]string, len(w.schema.Fields))
		for i, field := range w.schema.Fields {
			row[i] = w.valueToString(record[field.Name], field.Type)
		}
		if err := w.csvWriter.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
		w.rowCount++
	}

	if w.rowCount%100 == 0 {
		w.csvWriter.Flush()
		if err := w.csvWriter.Error(); err != nil {
			return fmt.Errorf("failed to flush CSV: %w", err)
		}
	}

	return nil
}

func (w *CSVWriter) valueToString(value interface{}, fieldType string) string {
	if value == nil {
		return w.nullValue
	}
	switch v := value.(type) {
	case string:
		return v
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", value)
	}
}

// Flush flushes any buffered data
func (w *CSVWriter) Flush(ctx context.Context) error {
	if w.csvWriter == nil {
		return fmt.Errorf("writer not opened")
	}
	w.csvWriter.Flush()
	if err := w.csvWriter.Error(); err != nil {
		return fmt.Errorf("failed to flush CSV: %w", err)
	}
	return nil
}

// Close closes the CSV writer
func (w *CSVWriter) Close() error {
	if w.csvWriter != nil {
		w.csvWriter.Flush()
	}
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// newCSVWriterWithFile 使用已打开的 io.WriteCloser 创建 CSV Writer（供 NFSWriter 使用）
func newCSVWriterWithFile(file io.WriteCloser, delimiter string, writeHeader bool) *CSVWriter {
	csvWriter := csv.NewWriter(file)
	if len(delimiter) > 0 {
		csvWriter.Comma = rune(delimiter[0])
	}
	return &CSVWriter{
		csvWriter:   csvWriter,
		file:        file,
		writeHeader: writeHeader,
		firstBatch:  true,
	}
}
