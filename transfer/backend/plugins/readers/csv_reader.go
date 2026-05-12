package readers

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/addp/common/format"
	csvParser "github.com/addp/common/format/plugins/csv"
	"github.com/addp/transfer/pkg/pipeline"
)

// CSVReader reads data from CSV files
// Refactored to use shared common/format/plugins/csv parser
type CSVReader struct {
	parser    *csvParser.Plugin
	file      *os.File
	filePath  string
	schema    *pipeline.Schema
	batchSize int
	rowOffset int64
	mode      pipeline.ReaderMode

	// Parser options
	opts *format.ParseOptions
}

// NewCSVReader creates a CSV reader factory function
func NewCSVReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	return &CSVReader{
		batchSize: batchSize,
		mode:      pipeline.ModeBatch,
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
	r.filePath = filePath

	// Build parse options from config
	opts := format.DefaultParseOptions()

	// Delimiter
	if delimiter, ok := cfg["delimiter"].(string); ok && len(delimiter) > 0 {
		opts.Delimiter = rune(delimiter[0])
	}

	// Has header
	if hasHeader, ok := cfg["has_header"].(bool); ok {
		opts.HasHeader = hasHeader
	}

	// Encoding
	if encoding, ok := cfg["encoding"].(string); ok {
		opts.Encoding = encoding
	}

	// Skip rows
	if skipRows, ok := cfg["skip_rows"].(float64); ok {
		opts.SkipRows = int(skipRows)
	}

	// Sample size for type detection
	if sampleSize, ok := cfg["sample_size"].(float64); ok {
		opts.SampleSize = int(sampleSize)
	} else {
		opts.SampleSize = 100 // Default sample size
	}

	r.opts = opts

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open CSV file: %w", err)
	}
	r.file = file

	// Create parser
	r.parser = csvParser.NewPlugin(opts)

	// Parse schema using the shared CSV parser
	tableInfo, err := r.parser.DescribeTable(ctx, r.file, opts)
	if err != nil {
		r.file.Close()
		return fmt.Errorf("failed to parse CSV schema: %w", err)
	}

	// Convert to pipeline schema
	r.schema = r.convertSchemaFromTableInfo(tableInfo)

	// Reset file for reading data
	if _, err := r.file.Seek(0, 0); err != nil {
		r.file.Close()
		return fmt.Errorf("failed to reset file: %w", err)
	}

	return nil
}

// Read reads a batch of records
func (r *CSVReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	if r.file == nil || r.parser == nil {
		return nil, fmt.Errorf("reader not opened")
	}

	// Read a batch using the shared CSV parser
	records, err := r.parser.SampleTable(ctx, r.file, r.rowOffset, int64(r.batchSize), r.opts)
	if err != nil {
		if err == io.EOF && len(records) == 0 {
			return nil, io.EOF
		}
		// Partial read is OK
		if len(records) == 0 {
			return nil, fmt.Errorf("failed to read CSV records: %w", err)
		}
	}

	// Check if EOF
	if len(records) == 0 {
		return nil, io.EOF
	}

	// Update offset
	r.rowOffset += int64(len(records))

	return &pipeline.DataBatch{
		Rows:           records,
		Schema:         r.schema,
		Offset:         r.rowOffset,
		SequenceNumber: r.rowOffset / int64(r.batchSize),
		Timestamp:      time.Now(),
	}, nil
}

// convertSchemaFromTableInfo converts format.TableInfo to pipeline.Schema
func (r *CSVReader) convertSchemaFromTableInfo(tableInfo *format.TableInfo) *pipeline.Schema {
	pipelineFields := make([]pipeline.Field, len(tableInfo.Fields))

	for i, field := range tableInfo.Fields {
		pipelineFields[i] = pipeline.Field{
			Name:     field.Name,
			Type:     string(field.Type),
			Nullable: field.Nullable,
		}
	}

	schema := &pipeline.Schema{
		Fields: pipelineFields,
		Metadata: map[string]interface{}{
			"source_type": "csv",
			"file_path":   r.filePath,
		},
	}

	if tableInfo.RowCount != nil {
		schema.Metadata["estimated_rows"] = *tableInfo.RowCount
	}

	return schema
}

// Schema returns the inferred schema
func (r *CSVReader) Schema() (*pipeline.Schema, error) {
	if r.schema == nil {
		return nil, fmt.Errorf("schema not initialized, call Open first")
	}
	return r.schema, nil
}

// SeekTo seeks to a specific row offset
func (r *CSVReader) SeekTo(offset int64) error {
	// For CSV, we need to reopen and skip rows
	if r.file != nil {
		r.file.Close()
	}

	// Reopen file
	file, err := os.Open(r.filePath)
	if err != nil {
		return fmt.Errorf("failed to reopen CSV file: %w", err)
	}
	r.file = file

	// Create new parser with same options
	r.parser = csvParser.NewPlugin(r.opts)

	// Skip to offset (including header if present)
	ctx := context.Background()
	skipCount := offset
	if r.opts.HasHeader {
		skipCount++ // Skip header row
	}

	_, err = r.parser.SampleTable(ctx, r.file, 0, skipCount, r.opts)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to seek to offset %d: %w", offset, err)
	}

	r.rowOffset = offset
	return nil
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
