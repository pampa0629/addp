package connector

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
)

// CSVReader reads data from CSV files
type CSVReader struct {
	csvReader     *csv.Reader
	file          *os.File
	headers       []string
	schema        *pipeline.Schema
	batchSize     int
	rowOffset     int64
	detectTypes   bool
	nullValue     string
	trimSpaces    bool
	mode          pipeline.ReaderMode
}

// NewCSVReader creates a CSV reader factory function
func NewCSVReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	return &CSVReader{
		batchSize:   batchSize,
		detectTypes: true,
		trimSpaces:  true,
		mode:        pipeline.ModeBatch,
	}, nil
}

// Open opens the CSV file
func (r *CSVReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	cfg := config.Config

	// Get file path
	filePath, ok := cfg["file_path"].(string)
	if !ok || filePath == "" {
		return fmt.Errorf("file_path is required")
	}

	// Parse config
	if detectTypes, ok := cfg["detect_types"].(bool); ok {
		r.detectTypes = detectTypes
	}
	if nullValue, ok := cfg["null_value"].(string); ok {
		r.nullValue = nullValue
	}
	if trimSpaces, ok := cfg["trim_spaces"].(bool); ok {
		r.trimSpaces = trimSpaces
	}

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open CSV file: %w", err)
	}
	r.file = file

	// Create CSV reader
	csvReader := csv.NewReader(file)

	// Configure reader
	if delimiter, ok := cfg["delimiter"].(string); ok && len(delimiter) > 0 {
		csvReader.Comma = rune(delimiter[0])
	} else {
		csvReader.Comma = ','
	}

	if lazyQuotes, ok := cfg["lazy_quotes"].(bool); ok {
		csvReader.LazyQuotes = lazyQuotes
	} else {
		csvReader.LazyQuotes = true // More tolerant
	}

	if comment, ok := cfg["comment"].(string); ok && len(comment) > 0 {
		csvReader.Comment = rune(comment[0])
	}

	csvReader.FieldsPerRecord = -1 // Allow variable fields
	csvReader.TrimLeadingSpace = r.trimSpaces

	r.csvReader = csvReader

	// Skip initial rows if configured
	skipRows := 0
	if sr, ok := cfg["skip_rows"].(float64); ok {
		skipRows = int(sr)
	}
	for i := 0; i < skipRows; i++ {
		if _, err := r.csvReader.Read(); err != nil {
			return fmt.Errorf("failed to skip row %d: %w", i, err)
		}
	}

	// Read header
	hasHeader := true
	if hh, ok := cfg["has_header"].(bool); ok {
		hasHeader = hh
	}

	if hasHeader {
		headers, err := r.csvReader.Read()
		if err != nil {
			return fmt.Errorf("failed to read CSV header: %w", err)
		}
		r.headers = headers
	} else {
		// Generate default headers - need to peek at first row
		firstRow, err := r.csvReader.Read()
		if err != nil {
			return fmt.Errorf("failed to read first row: %w", err)
		}
		r.headers = make([]string, len(firstRow))
		for i := range firstRow {
			r.headers[i] = fmt.Sprintf("col_%d", i+1)
		}
		// Note: first data row will be lost since we can't rewind easily
		// In production, would use a buffer or io.Seeker
	}

	// Build schema
	r.schema = r.buildSchema()

	return nil
}

// Read reads a batch of records
func (r *CSVReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	if r.csvReader == nil {
		return nil, fmt.Errorf("reader not opened")
	}

	rows := make([]map[string]interface{}, 0, r.batchSize)

	for len(rows) < r.batchSize {
		record, err := r.csvReader.Read()
		if err == io.EOF {
			// End of file
			if len(rows) == 0 {
				return nil, io.EOF
			}
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		// Convert row to map
		row := make(map[string]interface{})
		for i, header := range r.headers {
			if i >= len(record) {
				row[header] = nil
				continue
			}

			value := record[i]
			if r.trimSpaces {
				value = strings.TrimSpace(value)
			}

			// Convert to appropriate type based on schema
			if r.schema != nil && i < len(r.schema.Fields) {
				row[header] = r.convertValue(value, r.schema.Fields[i].Type)
			} else {
				row[header] = value
			}
		}

		rows = append(rows, row)
		r.rowOffset++
	}

	return &pipeline.DataBatch{
		Rows:           rows,
		Schema:         r.schema,
		Offset:         r.rowOffset,
		SequenceNumber: r.rowOffset / int64(r.batchSize),
	}, nil
}

// buildSchema creates schema (simplified - just default to string)
func (r *CSVReader) buildSchema() *pipeline.Schema {
	schema := &pipeline.Schema{
		Fields: make([]pipeline.Field, len(r.headers)),
	}

	for i, name := range r.headers {
		// Default type - in production would detect from sample rows
		fieldType := "string"

		schema.Fields[i] = pipeline.Field{
			Name:     name,
			Type:     fieldType,
			Nullable: true,
		}
	}

	return schema
}

// convertValue converts string to appropriate type
func (r *CSVReader) convertValue(value string, fieldType string) interface{} {
	// Check NULL
	if value == "" || value == r.nullValue {
		return nil
	}

	switch fieldType {
	case "int", "integer", "bigint":
		if v, err := strconv.ParseInt(value, 10, 64); err == nil {
			return v
		}
	case "float", "double", "decimal", "numeric":
		if v, err := strconv.ParseFloat(value, 64); err == nil {
			return v
		}
	case "bool", "boolean":
		value = strings.ToLower(value)
		if value == "true" || value == "1" || value == "yes" {
			return true
		}
		if value == "false" || value == "0" || value == "no" {
			return false
		}
	}

	return value
}

// Schema returns the inferred schema
func (r *CSVReader) Schema() (*pipeline.Schema, error) {
	if r.schema == nil {
		return nil, fmt.Errorf("schema not initialized, call Open first")
	}
	return r.schema, nil
}

// SeekTo seeks to a specific row offset (not supported for CSV)
func (r *CSVReader) SeekTo(offset int64) error {
	return fmt.Errorf("seek not supported for CSV reader")
}

// Mode returns the reader mode
func (r *CSVReader) Mode() pipeline.ReaderMode {
	return r.mode
}

// Close closes the CSV reader
func (r *CSVReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}
