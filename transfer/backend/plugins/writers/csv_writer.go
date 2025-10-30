package writers

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"

	"github.com/addp/transfer/pkg/pipeline"
)

// CSVWriter writes data to CSV files
type CSVWriter struct {
	csvWriter   *csv.Writer
	file        *os.File
	schema      *pipeline.Schema
	writeHeader bool
	nullValue   string
	rowCount    int64
	firstBatch  bool
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

	// Get file path
	filePath, ok := cfg["file_path"].(string)
	if !ok || filePath == "" {
		return fmt.Errorf("file_path is required")
	}

	// Parse config
	if writeHeader, ok := cfg["write_header"].(bool); ok {
		w.writeHeader = writeHeader
	}
	if nullValue, ok := cfg["null_value"].(string); ok {
		w.nullValue = nullValue
	}

	// Open file for writing
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	w.file = file

	// Create CSV writer
	csvWriter := csv.NewWriter(file)

	// Configure writer
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

	// Get schema from batch
	if w.schema == nil && batch.Schema != nil {
		w.schema = batch.Schema
	}

	if w.schema == nil {
		return fmt.Errorf("schema not available in batch")
	}

	// Write header on first batch
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

	// Write each record
	for _, record := range batch.Rows {
		row := make([]string, len(w.schema.Fields))

		for i, field := range w.schema.Fields {
			value := record[field.Name]

			// Convert to string
			row[i] = w.valueToString(value, field.Type)
		}

		if err := w.csvWriter.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}

		w.rowCount++
	}

	// Flush periodically (every 100 rows)
	if w.rowCount%100 == 0 {
		w.csvWriter.Flush()
		if err := w.csvWriter.Error(); err != nil {
			return fmt.Errorf("failed to flush CSV: %w", err)
		}
	}

	return nil
}

// valueToString converts a value to string for CSV output
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
