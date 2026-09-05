package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/mysql"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
)

func (p *MySQLPlugin) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	return p.openTableReadSession(ctx, connInfo, path, opts, 0, 0)
}

func (p *MySQLPlugin) readBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	session, err := p.openTableReadSession(ctx, connInfo, path, plugin.TableReadSessionOptions{
		Query: opts.Query, Args: opts.Args, Hints: opts.Hints,
	}, opts.Limit, opts.Offset)
	if err != nil {
		return nil, err
	}
	defer session.Close(context.Background())
	return session.ReadBatch(ctx, opts.Limit)
}

func (p *MySQLPlugin) openTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions, limit int, offset int64) (plugin.TableReadSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, "mysql.table_read_session"); err != nil {
		return nil, err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build mysql dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql connection: %w", err)
	}
	query, fields, spatialInfo, encoding, err := mysqlReadSessionQuery(ctx, db, path, opts, limit, offset)
	if err != nil {
		db.Close()
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin mysql table read session: %w", err)
	}
	rows, err := tx.QueryContext(ctx, query, opts.Args...)
	if err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("query mysql table read session: %w", err)
	}
	return &mysqlTableReadSession{db: db, tx: tx, rows: rows, fields: fields, spatialInfo: spatialInfo, geometryEncoding: encoding}, nil
}

func mysqlReadSessionQuery(ctx context.Context, db *sql.DB, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions, limit int, offset int64) (string, []datatype.FieldInfo, *datatype.SpatialInfo, format.GeometryEncoding, error) {
	encoding := mysqlGeometryEncodingHint(opts.Hints)
	query := strings.TrimSpace(opts.Query)
	if query != "" {
		if encoding != "" {
			return "", nil, nil, "", fmt.Errorf("mysql spatial row encoding requires a catalog table read, not custom SQL")
		}
		if limit > 0 {
			query = commonquery.PaginateQuerySQL(query, limit, int(offset))
		}
		return query, nil, nil, "", nil
	}
	database, table, err := mysqlTablePathParts(path)
	if err != nil {
		return "", nil, nil, "", err
	}
	columns, err := mysqlTableColumns(ctx, db, database, table)
	if err != nil {
		return "", nil, nil, "", err
	}
	fields := mysqlFieldsFromColumns(columns)
	fields, err = mysqlSelectedFields(fields, opts.Hints)
	if err != nil {
		return "", nil, nil, "", err
	}
	selectExpr, err := mysqlSelectExpr(columns, fields, opts.Hints, encoding)
	if err != nil {
		return "", nil, nil, "", err
	}
	query = commonquery.ForDialect("mysql").SelectTableSQL(selectExpr, database, table, "", "", limit, int(offset))
	spatialInfo := mysqlSpatialInfoFromColumns(columns, fields, opts.Hints, encoding)
	return query, fields, spatialInfo, encoding, nil
}

func mysqlFieldsFromColumns(columns []mysqlColumnInfo) []datatype.FieldInfo {
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		field := datatype.FieldInfo{Name: column.Name, Type: mysqlCommonFieldType(column), NativeType: mysqlColumnNativeType(column), Nullable: column.Nullable, PrimaryKey: column.PrimaryKey, Comment: column.Comment}
		if field.Type == datatype.FieldTypeDecimal && column.NumericPrecision.Valid {
			field.Precision = int(column.NumericPrecision.Int64)
			if column.NumericScale.Valid {
				field.Scale = int(column.NumericScale.Int64)
			}
		}
		fields = append(fields, field)
	}
	return plugin.NormalizeFieldInfos(fields)
}

func mysqlSelectedFields(fields []datatype.FieldInfo, hints map[string]interface{}) ([]datatype.FieldInfo, error) {
	selection := mysqlFieldSelection(hints)
	if selection == nil || len(selection.Include) == 0 {
		return fields, nil
	}
	selected, err := format.ApplyFieldSelectionToTableInfo(&datatype.TableInfo{Fields: fields}, selection)
	if err != nil {
		return nil, err
	}
	return selected.Fields, nil
}

func mysqlFieldSelection(hints map[string]interface{}) *format.FieldSelectionOptions {
	if hints == nil {
		return nil
	}
	switch selection := hints[format.FieldSelectionOptionKey].(type) {
	case *format.FieldSelectionOptions:
		return selection
	case format.FieldSelectionOptions:
		return &selection
	default:
		return nil
	}
}

func mysqlSelectExpr(columns []mysqlColumnInfo, fields []datatype.FieldInfo, hints map[string]interface{}, encoding format.GeometryEncoding) (string, error) {
	dialect := commonquery.ForDialect("mysql")
	selected := make(map[string]bool, len(fields))
	for _, field := range fields {
		selected[field.Name] = true
	}
	geometryField := strings.TrimSpace(mysqlHintString(hints, plugin.TableReadHintGeometryField))
	expressions := make([]string, 0, len(fields))
	for _, column := range columns {
		if !selected[column.Name] {
			continue
		}
		quoted := dialect.QuoteIdentifier(column.Name)
		if datatype.IsSpatialFieldType(mysqlCommonFieldType(column)) && encoding != "" {
			spatialHints := hints
			if geometryField != "" && !strings.EqualFold(geometryField, column.Name) {
				spatialHints = mysqlHintsWithoutTransform(hints)
			}
			expression, err := mysqlSpatialReadExpression(quoted, column, spatialHints, encoding)
			if err != nil {
				return "", err
			}
			expressions = append(expressions, expression+" AS "+quoted)
			continue
		}
		expressions = append(expressions, quoted)
	}
	if len(expressions) == 0 {
		return "", fmt.Errorf("mysql table read requires at least one selected field")
	}
	return strings.Join(expressions, ", "), nil
}

func mysqlHintsWithoutTransform(hints map[string]interface{}) map[string]interface{} {
	if len(hints) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(hints))
	for key, value := range hints {
		if key == plugin.TableReadHintGeometryTargetSRID || key == plugin.TableReadHintGeometryTransformPolicy {
			continue
		}
		cloned[key] = value
	}
	return cloned
}

func mysqlSpatialReadExpression(quoted string, column mysqlColumnInfo, hints map[string]interface{}, encoding format.GeometryEncoding) (string, error) {
	valueExpression := quoted
	targetSRID := mysqlGeometryTargetSRID(hints)
	sourceSRID := 0
	if column.SRSID.Valid {
		sourceSRID = int(column.SRSID.Int64)
	}
	policy := strings.ToLower(strings.TrimSpace(mysqlHintString(hints, plugin.TableReadHintGeometryTransformPolicy)))
	if targetSRID > 0 && targetSRID != sourceSRID {
		if sourceSRID <= 0 {
			if policy == "required" {
				return "", fmt.Errorf("mysql geometry column %q requires a known source SRID for required spatial transform", column.Name)
			}
		} else if policy == "" || policy == "required" {
			valueExpression = fmt.Sprintf("ST_Transform(%s, %d)", quoted, targetSRID)
		}
	}
	switch encoding {
	case format.GeometryEncodingEWKB:
		return "ST_AsWKB(" + valueExpression + ", 'axis-order=long-lat')", nil
	case format.GeometryEncodingGeoJSON:
		return "ST_AsGeoJSON(" + valueExpression + ", 15, 0)", nil
	default:
		return "", fmt.Errorf("unsupported mysql geometry read encoding %q", encoding)
	}
}

func mysqlGeometryEncodingHint(hints map[string]interface{}) format.GeometryEncoding {
	switch strings.ToLower(strings.TrimSpace(mysqlHintString(hints, plugin.TableReadHintGeometryEncoding))) {
	case string(format.GeometryEncodingEWKB):
		return format.GeometryEncodingEWKB
	case string(format.GeometryEncodingGeoJSON):
		return format.GeometryEncodingGeoJSON
	default:
		return ""
	}
}

func mysqlGeometryTargetSRID(hints map[string]interface{}) int {
	if hints == nil {
		return 0
	}
	var srid int
	_, _ = fmt.Sscan(mysqlHintString(hints, plugin.TableReadHintGeometryTargetSRID), &srid)
	return srid
}

func mysqlHintString(hints map[string]interface{}, key string) string {
	if hints == nil || hints[key] == nil {
		return ""
	}
	return fmt.Sprint(hints[key])
}

func mysqlSpatialInfoFromColumns(columns []mysqlColumnInfo, fields []datatype.FieldInfo, hints map[string]interface{}, encoding format.GeometryEncoding) *datatype.SpatialInfo {
	selected := make(map[string]bool, len(fields))
	for _, field := range fields {
		selected[field.Name] = true
	}
	rows := make([]mysqlSpatialColumnRow, 0)
	for _, column := range columns {
		if selected[column.Name] && datatype.IsSpatialFieldType(mysqlCommonFieldType(column)) {
			rows = append(rows, mysqlSpatialColumnRow{Name: column.Name, DataType: column.DataType, SRSID: column.SRSID, Nullable: column.Nullable})
		}
	}
	info := buildMySQLSpatialInfo(rows, nil, nil)
	if info == nil || encoding == "" {
		return info
	}
	targetSRID := mysqlGeometryTargetSRID(hints)
	if targetSRID <= 0 {
		return info
	}
	policy := strings.ToLower(strings.TrimSpace(mysqlHintString(hints, plugin.TableReadHintGeometryTransformPolicy)))
	if policy != "" && policy != "required" {
		return info
	}
	geometryField := strings.TrimSpace(mysqlHintString(hints, plugin.TableReadHintGeometryField))
	for i := range info.GeometryColumns {
		if geometryField != "" && !strings.EqualFold(geometryField, info.GeometryColumns[i].Name) {
			continue
		}
		if info.GeometryColumns[i].SRID != nil && *info.GeometryColumns[i].SRID > 0 {
			info.GeometryColumns[i].SRID = &targetSRID
			info.GeometryColumns[i].CRSRef = datatype.EPSGCRSRef(targetSRID)
		}
	}
	return info
}

type mysqlTableReadSession struct {
	db               *sql.DB
	tx               *sql.Tx
	rows             *sql.Rows
	fields           []datatype.FieldInfo
	spatialInfo      *datatype.SpatialInfo
	geometryEncoding format.GeometryEncoding
	offset           int64
	exhausted        bool
	closed           bool
}

func (s *mysqlTableReadSession) ReadBatch(_ context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("mysql table read session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	if s.exhausted {
		batch := &plugin.BatchData{Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset}
		if s.spatialInfo != nil {
			batch.Spatial = s.spatialInfo.Clone()
		}
		return batch, nil
	}
	columns, err := s.rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get mysql read columns: %w", err)
	}
	result := make([]map[string]interface{}, 0, limit)
	for len(result) < limit {
		if !s.rows.Next() {
			s.exhausted = true
			break
		}
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := s.rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan mysql table row: %w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for i, name := range columns {
			value, err := mysqlReadValue(name, values[i], s.fields, s.spatialInfo, s.geometryEncoding)
			if err != nil {
				return nil, err
			}
			row[name] = value
		}
		result = append(result, row)
	}
	if err := s.rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql table rows: %w", err)
	}
	batch := &plugin.BatchData{Rows: result, Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset}
	if s.spatialInfo != nil {
		batch.Spatial = s.spatialInfo.Clone()
	}
	s.offset += int64(len(result))
	return batch, nil
}

func mysqlReadValue(column string, value interface{}, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo, encoding format.GeometryEncoding) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	spatialColumn := false
	for _, field := range fields {
		if strings.EqualFold(field.Name, column) && datatype.IsSpatialFieldType(field.Type) {
			spatialColumn = true
			break
		}
	}
	if !spatialColumn || encoding == "" {
		if bytes, ok := value.([]byte); ok {
			return string(bytes), nil
		}
		return value, nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil, fmt.Errorf("mysql geometry column %q returned %T, want []byte", column, value)
	}
	srid := 0
	if spatialInfo != nil {
		for _, geometry := range spatialInfo.GeometryColumns {
			if strings.EqualFold(geometry.Name, column) && geometry.SRID != nil {
				srid = *geometry.SRID
				break
			}
		}
	}
	switch encoding {
	case format.GeometryEncodingEWKB:
		geometry, err := wkb.Unmarshal(bytes)
		if err != nil {
			return nil, fmt.Errorf("decode mysql WKB column %q: %w", column, err)
		}
		if srid > 0 {
			geometry, err = geom.SetSRID(geometry, srid)
			if err != nil {
				return nil, fmt.Errorf("set mysql geometry column %q SRID: %w", column, err)
			}
		}
		encoded, err := ewkb.Marshal(geometry, ewkb.NDR)
		if err != nil {
			return nil, fmt.Errorf("encode mysql geometry column %q as EWKB: %w", column, err)
		}
		return encoded, nil
	case format.GeometryEncodingGeoJSON:
		var geometry map[string]interface{}
		if err := json.Unmarshal(bytes, &geometry); err != nil {
			return nil, fmt.Errorf("decode mysql GeoJSON column %q: %w", column, err)
		}
		return geometry, nil
	default:
		return nil, fmt.Errorf("unsupported mysql geometry read encoding %q", encoding)
	}
}

func (s *mysqlTableReadSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	var closeErr error
	if s.rows != nil {
		closeErr = s.rows.Close()
	}
	if s.tx != nil {
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && closeErr == nil {
			closeErr = err
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}
