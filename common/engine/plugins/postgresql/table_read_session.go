package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/postgresql"
	"github.com/addp/common/resume"
	"github.com/addp/common/sqldialect"
)

func (p *PostgreSQLPlugin) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, "postgresql.table_read_session"); err != nil {
		return nil, err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgresql connection: %w", err)
	}
	query, fields, spatialInfo, err := postgresReadSessionQuery(ctx, db, path, opts)
	if err != nil {
		db.Close()
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin postgresql table read session: %w", err)
	}

	cursorName := "addp_transfer_read_cursor"
	declareSQL := fmt.Sprintf("DECLARE %s NO SCROLL CURSOR FOR %s", cursorName, query)
	if _, err := tx.ExecContext(ctx, declareSQL); err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("declare postgresql read cursor: %w", err)
	}

	return &postgresTableReadSession{
		db:          db,
		tx:          tx,
		cursorName:  cursorName,
		fields:      fields,
		spatialInfo: spatialInfo,
	}, nil
}

func postgresReadSessionQuery(ctx context.Context, db *sql.DB, path plugin.CatalogPath, opts plugin.TableReadSessionOptions) (string, []datatype.FieldInfo, *datatype.SpatialInfo, error) {
	query := strings.TrimSpace(opts.Query)
	if query != "" {
		return query, nil, nil, nil
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return "", nil, nil, err
	}
	columns, err := postgresTableColumns(ctx, db, schema, table)
	if err != nil {
		return "", nil, nil, err
	}
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, postgresFieldInfoFromColumn(column))
	}
	selectedFields, err := postgresSelectedFields(fields, opts.Metadata)
	if err != nil {
		return "", nil, nil, err
	}
	if len(selectedFields) > 0 {
		fields = selectedFields
	}
	selectExpr := postgresSelectExprForFields(fields)
	if shouldReadPostgresSpatialAsGeoJSON(opts.Metadata) {
		if expr, err := postgresGeoJSONSelectExpr(columns, opts.Metadata, fields); err != nil {
			return "", nil, nil, err
		} else if expr != "" {
			selectExpr = expr
		}
	}
	return sqldialect.ForEngine("postgresql").SelectTableSQL(selectExpr, schema, table, "", "", 0, 0), fields, postgresSpatialInfoFromFields(fields), nil
}

func postgresSelectedFields(fields []datatype.FieldInfo, metadata map[string]interface{}) ([]datatype.FieldInfo, error) {
	if len(fields) == 0 || metadata == nil {
		return nil, nil
	}
	selection := postgresFieldSelection(metadata)
	if selection == nil || len(selection.Include) == 0 {
		return nil, nil
	}
	byName := make(map[string]datatype.FieldInfo, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
	}
	selected := make([]datatype.FieldInfo, 0, len(selection.Include))
	seen := map[string]bool{}
	for _, name := range selection.Include {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		field, ok := byName[name]
		if !ok {
			if selection.EffectiveMissingFieldPolicy() == format.MissingFieldIgnore {
				continue
			}
			return nil, fmt.Errorf("field selection references missing field %q", name)
		}
		selected = append(selected, field)
	}
	return selected, nil
}

func postgresFieldSelection(metadata map[string]interface{}) *format.FieldSelectionOptions {
	if metadata == nil {
		return nil
	}
	value := metadata[format.FieldSelectionOptionKey]
	switch selection := value.(type) {
	case *format.FieldSelectionOptions:
		return selection
	case format.FieldSelectionOptions:
		return &selection
	default:
		return nil
	}
}

func postgresSelectExprForFields(fields []datatype.FieldInfo) string {
	if len(fields) == 0 {
		return "*"
	}
	dialect := sqldialect.ForEngine("postgresql")
	exprs := make([]string, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		exprs = append(exprs, dialect.QuoteIdentifier(name))
	}
	if len(exprs) == 0 {
		return "*"
	}
	return strings.Join(exprs, ", ")
}

func shouldReadPostgresSpatialAsGeoJSON(metadata map[string]interface{}) bool {
	if metadata == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(metadataString(metadata, "spatial.target_encoding")), "geojson")
}

func postgresGeoJSONSelectExpr(columns []postgresColumnInfo, metadata map[string]interface{}, fields []datatype.FieldInfo) (string, error) {
	geometryField := strings.TrimSpace(metadataString(metadata, "geometry_field"))
	selected := map[string]bool{}
	if len(fields) > 0 {
		for _, field := range fields {
			selected[field.Name] = true
		}
	}
	dialect := sqldialect.ForEngine("postgresql")
	exprs := make([]string, 0, len(columns))
	for _, column := range columns {
		if len(selected) > 0 && !selected[column.Name] {
			continue
		}
		quoted := dialect.QuoteIdentifier(column.Name)
		if column.IsSpatial() && (geometryField == "" || column.Name == geometryField) {
			exprs = append(exprs, "ST_AsGeoJSON("+quoted+")::json AS "+quoted)
			continue
		}
		exprs = append(exprs, quoted)
	}
	if len(exprs) == 0 {
		return "", nil
	}
	return strings.Join(exprs, ", "), nil
}

type postgresColumnInfo struct {
	Name       string
	DataType   string
	UDTName    string
	NativeType string
}

func (c postgresColumnInfo) IsSpatial() bool {
	switch strings.ToLower(strings.TrimSpace(c.UDTName)) {
	case "geometry", "geography":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(c.DataType)) {
	case "geometry", "geography", "user-defined":
		return strings.EqualFold(strings.TrimSpace(c.UDTName), "geometry") || strings.EqualFold(strings.TrimSpace(c.UDTName), "geography")
	default:
		return false
	}
}

func postgresTableColumns(ctx context.Context, db *sql.DB, schema, table string) ([]postgresColumnInfo, error) {
	if db == nil {
		return nil, fmt.Errorf("postgresql table columns requires db")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT
			c.column_name,
			c.data_type,
			c.udt_name,
			format_type(a.atttypid, a.atttypmod) AS native_type
		FROM information_schema.columns c
		JOIN pg_catalog.pg_namespace n
			ON n.nspname = c.table_schema
		JOIN pg_catalog.pg_class cls
			ON cls.relnamespace = n.oid
			AND cls.relname = c.table_name
		JOIN pg_catalog.pg_attribute a
			ON a.attrelid = cls.oid
			AND a.attname = c.column_name
			AND a.attnum > 0
			AND NOT a.attisdropped
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("query postgresql table columns %s.%s: %w", schema, table, err)
	}
	defer rows.Close()

	columns := make([]postgresColumnInfo, 0)
	for rows.Next() {
		var column postgresColumnInfo
		if err := rows.Scan(&column.Name, &column.DataType, &column.UDTName, &column.NativeType); err != nil {
			return nil, fmt.Errorf("scan postgresql table column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgresql table columns: %w", err)
	}
	return columns, nil
}

func postgresFieldInfoFromColumn(column postgresColumnInfo) datatype.FieldInfo {
	nativeType := postgresColumnNativeType(column)
	return datatype.FieldInfo{
		Name:       column.Name,
		Type:       postgresCommonFieldType(column, nativeType),
		NativeType: nativeType,
	}
}

func postgresColumnNativeType(column postgresColumnInfo) string {
	nativeType := strings.TrimSpace(column.NativeType)
	if nativeType != "" && nativeType != "-" {
		return nativeType
	}
	dataType := strings.TrimSpace(column.DataType)
	udtName := strings.TrimSpace(column.UDTName)
	if column.IsSpatial() && udtName != "" {
		return strings.ToLower(udtName)
	}
	if strings.EqualFold(dataType, "USER-DEFINED") && udtName != "" {
		return udtName
	}
	return dataType
}

func postgresCommonFieldType(column postgresColumnInfo, nativeType string) datatype.FieldType {
	if column.IsSpatial() {
		return datatype.FieldTypeGeometry
	}
	if mapper := format.GetTypeMapper("postgresql"); mapper != nil {
		if fieldType := mapper.ToCommon(nativeType); fieldType != "" && fieldType != datatype.FieldTypeUnknown {
			return fieldType
		}
	}
	fallbackType := stripPostgresTypeModifiers(strings.TrimSpace(column.DataType))
	if mapper := format.GetTypeMapper("postgresql"); mapper != nil {
		if fieldType := mapper.ToCommon(fallbackType); fieldType != "" && fieldType != datatype.FieldTypeUnknown {
			return fieldType
		}
	}
	return datatype.FieldTypeUnknown
}

var postgresTypeModifierPattern = regexp.MustCompile(`\s*\(.*\)$`)

func stripPostgresTypeModifiers(value string) string {
	return postgresTypeModifierPattern.ReplaceAllString(strings.TrimSpace(value), "")
}

func postgresSpatialInfoFromFields(fields []datatype.FieldInfo) *datatype.SpatialInfo {
	if len(fields) == 0 {
		return nil
	}
	columns := make([]datatype.GeometryColumnInfo, 0)
	for _, field := range fields {
		if !datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		geometryType, srid, dimension := parsePostgresSpatialType(field.NativeType)
		column := datatype.GeometryColumnInfo{
			Name:         field.Name,
			GeometryType: geometryType,
		}
		if srid > 0 {
			column.SRID = &srid
		}
		if dimension > 0 {
			column.Dimension = &dimension
		}
		columns = append(columns, column)
	}
	if len(columns) == 0 {
		return nil
	}
	return &datatype.SpatialInfo{
		GeometryColumns:       columns,
		PrimaryGeometryColumn: columns[0].Name,
	}
}

func parsePostgresSpatialType(nativeType string) (string, int, int) {
	value := strings.TrimSpace(nativeType)
	open := strings.Index(value, "(")
	close := strings.LastIndex(value, ")")
	if open < 0 || close <= open {
		return "", 0, 0
	}
	parts := strings.Split(value[open+1:close], ",")
	geometryType, dimension := normalizePostgresGeometryType(parts[0])
	srid := 0
	if len(parts) > 1 {
		_, _ = fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &srid)
	}
	return geometryType, srid, dimension
}

func normalizePostgresGeometryType(value string) (string, int) {
	normalized := strings.TrimPrefix(strings.TrimSpace(value), "ST_")
	dimension := 0
	lower := strings.ToLower(normalized)
	if strings.HasSuffix(lower, "z") {
		dimension = 3
		normalized = strings.TrimSuffix(normalized, normalized[len(normalized)-1:])
		lower = strings.ToLower(normalized)
	}
	switch lower {
	case "point":
		return "Point", dimension
	case "linestring":
		return "LineString", dimension
	case "polygon":
		return "Polygon", dimension
	case "multipoint":
		return "MultiPoint", dimension
	case "multilinestring":
		return "MultiLineString", dimension
	case "multipolygon":
		return "MultiPolygon", dimension
	default:
		return "", 0
	}
}

func metadataString(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value := values[key]
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

type postgresTableReadSession struct {
	db          *sql.DB
	tx          *sql.Tx
	cursorName  string
	fields      []datatype.FieldInfo
	spatialInfo *datatype.SpatialInfo
	closed      bool
	offset      int64
}

func (s *postgresTableReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("postgresql table read session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	query := fmt.Sprintf("FETCH FORWARD %d FROM %s", limit, s.cursorName)
	rows, err := s.tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("fetch postgresql read cursor: %w", err)
	}
	defer rows.Close()

	batch, err := scanPostgresRowsToBatch(rows, s.fields, s.spatialInfo, s.offset)
	if err != nil {
		return nil, err
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

func (s *postgresTableReadSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true

	var closeErr error
	if s.tx != nil {
		if _, err := s.tx.ExecContext(ctx, "CLOSE "+s.cursorName); err != nil {
			closeErr = fmt.Errorf("close postgresql read cursor: %w", err)
		}
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && closeErr == nil {
			closeErr = fmt.Errorf("rollback postgresql table read session: %w", err)
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && closeErr == nil {
			closeErr = fmt.Errorf("close postgresql table read session connection: %w", err)
		}
	}
	return closeErr
}

func scanPostgresRowsToBatch(rows *sql.Rows, tableFields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo, offset int64) (*plugin.BatchData, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get postgresql cursor columns: %w", err)
	}
	resultRows := make([]map[string]interface{}, 0)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan postgresql cursor row: %w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			value := values[i]
			if bytes, ok := value.([]byte); ok {
				row[column] = string(bytes)
				continue
			}
			row[column] = value
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgresql cursor rows: %w", err)
	}

	return &plugin.BatchData{
		Rows:    resultRows,
		Fields:  postgresReadBatchFields(columns, tableFields),
		Spatial: spatialInfo.Clone(),
		Offset:  offset,
	}, nil
}

func postgresReadBatchFields(columns []string, tableFields []datatype.FieldInfo) []datatype.FieldInfo {
	fields := make([]datatype.FieldInfo, 0, len(columns))
	if len(tableFields) == 0 {
		for _, column := range columns {
			fields = append(fields, datatype.FieldInfo{Name: column})
		}
		return fields
	}
	byName := make(map[string]datatype.FieldInfo, len(tableFields))
	for _, field := range tableFields {
		if field.Name == "" {
			continue
		}
		byName[strings.ToLower(field.Name)] = field
	}
	for _, column := range columns {
		if field, ok := byName[strings.ToLower(column)]; ok {
			field.Name = column
			fields = append(fields, field)
			continue
		}
		fields = append(fields, datatype.FieldInfo{Name: column})
	}
	return fields
}
