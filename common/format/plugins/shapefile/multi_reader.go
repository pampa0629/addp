package shapefile

import (
	"context"
	"fmt"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
)

var _ format.MultiTableReaderProvider = (*Plugin)(nil)

func (plugin *Plugin) OpenMultiTableReader(ctx context.Context, refs contentio.MultiReader, options *format.ParseOptions) (format.TableReader, error) {
	opts := plugin.resolveOptions(options)

	if rangeReader, ok := refs.(contentio.MultiRangeReader); ok {
		source, indexed, err := newIndexedMultiTableReadSource(ctx, plugin, refs.Refs(), rangeReader, opts)
		if err != nil {
			return nil, err
		}
		if indexed {
			schema, err := source.describeTable(ctx, refs.Refs(), opts)
			if err != nil {
				return nil, fmt.Errorf("describe indexed shapefile ref table: %w", err)
			}
			return &indexedMultiTableReader{
				source: source,
				schema: schema,
			}, nil
		}
	}

	schema, err := plugin.DescribeMultiTable(ctx, refs, opts)
	if err != nil {
		return nil, err
	}

	_, basePath, cleanup, err := materializeRefs(ctx, refs)
	if err != nil {
		return nil, err
	}
	if opts.Encoding == "" || NormalizeDBFEncoding(opts.Encoding) == "utf-8" {
		if cpgEncoding := readCPGEncoding(basePath); cpgEncoding != "" {
			opts.Encoding = cpgEncoding
		}
	}
	reader, err := OpenWithEncoding(basePath+".shp", opts.Encoding)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("open shapefile ref table reader: %w", err)
	}
	return &sequentialMultiTableReader{
		reader:        reader,
		schema:        schema,
		cleanup:       cleanup,
		geometryField: plugin.getGeometryFieldName(),
	}, nil
}

type indexedMultiTableReader struct {
	source *indexedMultiTableReadSource
	schema *format.TableInfo
	offset int64
	done   bool
}

func (r *indexedMultiTableReader) Schema() *format.TableInfo {
	return r.schema
}

func (r *indexedMultiTableReader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if r.done {
		return []map[string]interface{}{}, nil
	}
	if limit < 0 {
		return nil, fmt.Errorf("read shapefile rows limit must be non-negative")
	}
	rows, _, err := r.source.readRows(ctx, r.offset, int64(limit))
	if err != nil {
		if isIndexedSampleFallbackError(err) {
			return nil, fmt.Errorf("indexed shapefile reader does not support this geometry type: %w", err)
		}
		return nil, err
	}
	r.offset += int64(len(rows))
	if len(rows) < limit {
		r.done = true
	}
	return rows, nil
}

func (r *indexedMultiTableReader) Close(context.Context) error {
	return nil
}

type sequentialMultiTableReader struct {
	reader        *Reader
	schema        *format.TableInfo
	cleanup       func()
	geometryField string
	recordIndex   int
	closed        bool
}

func (r *sequentialMultiTableReader) Schema() *format.TableInfo {
	return r.schema
}

func (r *sequentialMultiTableReader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if r.closed {
		return []map[string]interface{}{}, nil
	}
	if limit < 0 {
		return nil, fmt.Errorf("read shapefile rows limit must be non-negative")
	}
	rows := make([]map[string]interface{}, 0, limit)
	fields := r.reader.Fields()
	for len(rows) < limit && r.reader.Next() {
		select {
		case <-ctx.Done():
			return rows, ctx.Err()
		default:
		}
		_, shape := r.reader.Shape()
		row := make(map[string]interface{}, len(fields)+1)
		for i, field := range fields {
			fieldName := r.reader.TrimDBFFieldName(field.Name)
			rawValue := r.reader.ReadAttributeDecoded(r.recordIndex, i)
			if rawValue == "" {
				row[fieldName] = nil
				continue
			}
			row[fieldName] = ParseDBFAttribute(field.Fieldtype, rawValue)
		}
		if shape != nil {
			if wktValue, err := ShapeToWKT(shape); err == nil {
				row[r.geometryField] = wktValue
			} else {
				row[r.geometryField] = nil
			}
		} else {
			row[r.geometryField] = nil
		}
		rows = append(rows, row)
		r.recordIndex++
	}
	if err := r.reader.Err(); err != nil {
		return rows, fmt.Errorf("read shapefile rows: %w", err)
	}
	return rows, nil
}

func (r *sequentialMultiTableReader) Close(context.Context) error {
	if r.closed {
		return nil
	}
	r.closed = true
	var err error
	if r.reader != nil {
		err = r.reader.Close()
	}
	if r.cleanup != nil {
		r.cleanup()
	}
	return err
}
