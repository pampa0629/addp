package shapefile

import (
	"context"
	"fmt"
	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	"github.com/jonas-p/go-shp"
	"strings"
)

type reader struct {
	*shp.Reader
	encoding string
}

func openWithEncoding(filename string, encodingName string) (*reader, error) {
	shpReader, err := shp.Open(filename)
	if err != nil {
		return nil, err
	}
	return &reader{Reader: shpReader, encoding: NormalizeDBFEncoding(encodingName)}, nil
}

func trimDBFFieldName(name [11]byte) string {
	return decodeDBFName(name, "")
}

func (r *reader) trimDBFFieldName(name [11]byte) string {
	if r == nil {
		return trimDBFFieldName(name)
	}
	return decodeDBFName(name, r.encoding)
}

func (r *reader) readAttributeDecoded(row int, field int) string {
	if r == nil {
		return ""
	}
	return DecodeDBFText(r.ReadAttribute(row, field), r.encoding)
}

var _ format.MultiTableReaderProvider = (*Plugin)(nil)

func (plugin *Plugin) OpenMultiTableReader(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, options *format.ParseOptions) (format.TableReader, error) {
	opts := plugin.resolveOptions(options)

	if rangeReader, ok := reader.(contentio.RangeReader); ok {
		source, indexed, err := newIndexedMultiTableReadSource(ctx, plugin, refs, rangeReader, opts)
		if err != nil {
			return nil, err
		}
		if indexed {
			schema, err := source.describeTable(ctx, refs, opts)
			if err != nil {
				return nil, fmt.Errorf("describe indexed shapefile ref table: %w", err)
			}
			schema, err = format.ApplyFieldSelectionToTableInfo(schema, opts.FieldSelection)
			if err != nil {
				return nil, err
			}
			return &indexedMultiTableReader{
				source: source,
				schema: schema,
				opts:   opts,
			}, nil
		}
	}

	schema, err := plugin.DescribeMultiTable(ctx, reader, refs, opts)
	if err != nil {
		return nil, err
	}

	_, basePath, cleanup, err := materializeRefs(ctx, reader, refs)
	if err != nil {
		return nil, err
	}
	applyMaterializedSidecarOptions(basePath, opts)
	shpReader, err := openWithEncoding(basePath+extSHP, opts.Encoding)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("open shapefile ref table reader: %w", err)
	}
	return &sequentialMultiTableReader{
		reader:        shpReader,
		schema:        schema,
		cleanup:       cleanup,
		geometryField: plugin.getGeometryFieldName(opts),
		opts:          opts,
	}, nil
}

type indexedMultiTableReader struct {
	source *indexedMultiTableReadSource
	schema *format.TableInfo
	opts   *format.ParseOptions
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
	rows, _, err := r.source.readRows(ctx, r.offset, int64(limit), spatialSRID(r.schema))
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
	return format.ApplyFieldSelectionToRows(rows, r.opts.FieldSelection), nil
}

func (r *indexedMultiTableReader) Close(context.Context) error {
	return nil
}

type sequentialMultiTableReader struct {
	reader        *reader
	schema        *format.TableInfo
	cleanup       func()
	geometryField string
	opts          *format.ParseOptions
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
	for len(rows) < limit && r.reader.Next() {
		select {
		case <-ctx.Done():
			return rows, ctx.Err()
		default:
		}
		rows = append(rows, readSequentialRow(r.reader, r.recordIndex, r.geometryField, r.opts, spatialSRID(r.schema)))
		r.recordIndex++
	}
	if err := r.reader.Err(); err != nil {
		return rows, fmt.Errorf("read shapefile rows: %w", err)
	}
	return format.ApplyFieldSelectionToRows(rows, r.opts.FieldSelection), nil
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

func readSequentialRow(reader *reader, recordIndex int, geometryField string, opts *format.ParseOptions, srid int) map[string]interface{} {
	fields := reader.Fields()
	_, shape := reader.Shape()
	row := make(map[string]interface{}, len(fields)+1)
	for i, field := range fields {
		fieldName := reader.trimDBFFieldName(field.Name)
		rawValue := strings.TrimSpace(reader.readAttributeDecoded(recordIndex, i))
		if rawValue == "" {
			row[fieldName] = nil
			continue
		}
		row[fieldName] = parseDBFAttributeWithInfo(dbfFieldInfo{
			RawType:   string(field.Fieldtype),
			Size:      int(field.Size),
			Precision: int(field.Precision),
		}, rawValue)
	}
	if shape != nil {
		if geometryValue, err := shapeToRowValue(shape, opts, srid); err == nil {
			row[geometryField] = geometryValue
		} else {
			row[geometryField] = nil
		}
	} else {
		row[geometryField] = nil
	}
	return row
}
