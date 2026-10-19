package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
	"github.com/addp/common/spatial"
)

var _ format.TableReaderProvider = (*GeoPackagePlugin)(nil)
var _ format.TableSpatialInfoProvider = (*tableReader)(nil)

func (p *GeoPackagePlugin) OpenTableReader(ctx context.Context, input io.Reader, options *format.ParseOptions) (format.TableReader, error) {
	return p.openTableReader(ctx, input, options, 0, -1)
}

// Both windowed samples and continuous reads use this cursor and value decoder.
func (p *Plugin) openTableReader(ctx context.Context, input io.Reader, options *format.ParseOptions, offset, limit int64) (*tableReader, error) {
	if input == nil {
		return nil, fmt.Errorf("sqlite table reader requires input")
	}
	if options == nil {
		options = p.options
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "sqlite.table_reader"); err != nil {
			return nil, err
		}
	}
	db, cleanup, err := p.openDatabase(input)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()
	tableName := tableNameFromOptions(options)
	if tableName == "" {
		result, err := Analyze(ctx, db, p.analysisOptions(options))
		if err != nil {
			return nil, err
		}
		if p.Format() == format.FormatGeoPackage {
			visible := result.Metadata.Tables[:0]
			for _, table := range result.Metadata.Tables {
				if !isGeoPackageSystemTable(table.Name) {
					visible = append(visible, table)
				}
			}
			result.Metadata.Tables = visible
		}
		table := firstSQLiteTable(result)
		if table == nil {
			success = true
			return &tableReader{cleanup: cleanup}, nil
		}
		tableName = table.Name
	}
	info, err := p.describeTable(ctx, db, tableName)
	if err != nil {
		return nil, err
	}
	encoding := format.GeometryEncodingEWKB
	if options != nil && options.GeometryEncoding != "" {
		encoding = options.GeometryEncoding
	}
	geometryColumns := map[string]int{}
	if info.Spatial != nil {
		if spatial.NormalizeGeometryEncoding(string(encoding)) == "" {
			return nil, fmt.Errorf("unsupported geometry encoding %q", encoding)
		}
		for _, column := range info.Spatial.GeometryColumns {
			srid := 0
			if column.SRID != nil {
				srid = *column.SRID
			}
			found := false
			for _, field := range info.Table.Fields {
				if strings.EqualFold(field.Name, column.Name) {
					geometryColumns[field.Name] = srid
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("GeoPackage geometry column %q is missing", column.Name)
			}
		}
	}
	if offset < 0 {
		offset = 0
	}
	query := "SELECT * FROM " + escapeIdentifier(tableName)
	if limit >= 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}
	success = true
	return &tableReader{
		rows: rows, columns: columns, tableInfo: info.Table, spatialInfo: info.Spatial,
		geometryColumns: geometryColumns, encoding: encoding, cleanup: cleanup,
	}, nil
}

type tableReader struct {
	rows            *sql.Rows
	columns         []string
	tableInfo       *datatype.TableInfo
	spatialInfo     *datatype.SpatialInfo
	geometryColumns map[string]int
	encoding        format.GeometryEncoding
	cleanup         func()
	closed          bool
}

func (r *tableReader) Fields() []datatype.FieldInfo {
	if r.tableInfo == nil {
		return nil
	}
	return r.tableInfo.Clone().Fields
}

func (r *tableReader) SpatialInfo() *datatype.SpatialInfo { return r.spatialInfo.Clone() }

func (r *tableReader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if r.closed {
		return nil, fmt.Errorf("sqlite table reader is closed")
	}
	if limit < 0 {
		return nil, fmt.Errorf("sqlite table reader limit cannot be negative")
	}
	if limit == 0 {
		limit = 1
	}
	result := make([]map[string]interface{}, 0, limit)
	if r.rows == nil {
		return result, nil
	}
	for len(result) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !r.rows.Next() {
			break
		}
		values := make([]interface{}, len(r.columns))
		pointers := make([]interface{}, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := r.rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := make(map[string]interface{}, len(values))
		for i, name := range r.columns {
			value := values[i]
			if srid, geometry := r.geometryColumns[name]; geometry && value != nil {
				data, ok := value.([]byte)
				if !ok {
					return nil, fmt.Errorf("GeoPackage geometry column %q is not binary", name)
				}
				shape, err := spatial.ParseGeometryBytes(data)
				if err != nil {
					return nil, fmt.Errorf("parse GeoPackage geometry column %q: %w", name, err)
				}
				encoded, err := spatial.EncodeGeometryValue(shape, string(r.encoding), srid)
				if err != nil {
					return nil, fmt.Errorf("encode GeoPackage geometry column %q: %w", name, err)
				}
				row[name] = encoded
			} else {
				row[name] = normalizeSQLValue(value)
			}
		}
		result = append(result, row)
	}
	return result, r.rows.Err()
}

func (r *tableReader) Close(context.Context) error {
	if r.closed {
		return nil
	}
	r.closed = true
	var err error
	if r.rows != nil {
		err = r.rows.Close()
	}
	if r.cleanup != nil {
		r.cleanup()
	}
	return err
}
