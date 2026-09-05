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
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
)

func (p *PostgreSQLPlugin) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
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
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin postgresql table read session: %w", err)
	}

	cursorName := "addp_transfer_read_cursor"
	declareSQL := fmt.Sprintf("DECLARE %s NO SCROLL CURSOR FOR %s", cursorName, query)
	if _, err := tx.ExecContext(ctx, declareSQL, opts.Args...); err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("declare postgresql read cursor: %w", err)
	}

	return &postgresTableReadSession{
		db:               db,
		tx:               tx,
		cursorName:       cursorName,
		fields:           fields,
		spatialInfo:      spatialInfo,
		geometryEncoding: postgresGeometryEncodingHint(opts.Hints),
	}, nil
}

func postgresReadSessionQuery(ctx context.Context, db *sql.DB, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (string, []datatype.FieldInfo, *datatype.SpatialInfo, error) {
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
	selectedFields, err := postgresSelectedFields(fields, opts.Hints)
	if err != nil {
		return "", nil, nil, err
	}
	if len(selectedFields) > 0 {
		fields = selectedFields
	}
	selectExpr := postgresSelectExprForFields(fields)
	switch postgresGeometryEncodingHint(opts.Hints) {
	case format.GeometryEncodingGeoJSON:
		if expr, err := postgresGeoJSONSelectExpr(columns, opts.Hints, fields); err != nil {
			return "", nil, nil, err
		} else if expr != "" {
			selectExpr = expr
		}
	case format.GeometryEncodingEWKB:
		if expr, err := postgresEWKBSelectExpr(columns, opts.Hints, fields); err != nil {
			return "", nil, nil, err
		} else if expr != "" {
			selectExpr = expr
		}
	}
	return commonquery.ForDialect("postgresql").SelectTableSQL(selectExpr, schema, table, "", "", 0, 0), fields, postgresSpatialInfoFromFieldsWithHints(fields, opts.Hints), nil
}

func postgresSelectedFields(fields []datatype.FieldInfo, hints map[string]interface{}) ([]datatype.FieldInfo, error) {
	if len(fields) == 0 || hints == nil {
		return nil, nil
	}
	selection := postgresFieldSelection(hints)
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

func postgresFieldSelection(hints map[string]interface{}) *format.FieldSelectionOptions {
	if hints == nil {
		return nil
	}
	value := hints[format.FieldSelectionOptionKey]
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
	dialect := commonquery.ForDialect("postgresql")
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

func postgresGeometryEncodingHint(hints map[string]interface{}) format.GeometryEncoding {
	if hints == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(hintString(hints, plugin.TableReadHintGeometryEncoding))) {
	case string(format.GeometryEncodingGeoJSON):
		return format.GeometryEncodingGeoJSON
	case string(format.GeometryEncodingEWKB):
		return format.GeometryEncodingEWKB
	default:
		return ""
	}
}

func postgresGeoJSONSelectExpr(columns []postgresColumnInfo, hints map[string]interface{}, fields []datatype.FieldInfo) (string, error) {
	geometryField := strings.TrimSpace(hintString(hints, plugin.TableReadHintGeometryField))
	targetSRID := postgresGeoJSONTargetSRID(hints)
	transformPolicy := strings.ToLower(strings.TrimSpace(hintString(hints, plugin.TableReadHintGeometryTransformPolicy)))
	selected := map[string]bool{}
	if len(fields) > 0 {
		for _, field := range fields {
			selected[field.Name] = true
		}
	}
	dialect := commonquery.ForDialect("postgresql")
	exprs := make([]string, 0, len(columns))
	for _, column := range columns {
		if len(selected) > 0 && !selected[column.Name] {
			continue
		}
		quoted := dialect.QuoteIdentifier(column.Name)
		if column.IsSpatial() && (geometryField == "" || column.Name == geometryField) {
			expr, err := postgresGeoJSONSpatialExpr(quoted, column, targetSRID, transformPolicy)
			if err != nil {
				return "", err
			}
			exprs = append(exprs, expr+" AS "+quoted)
			continue
		}
		exprs = append(exprs, quoted)
	}
	if len(exprs) == 0 {
		return "", nil
	}
	return strings.Join(exprs, ", "), nil
}

func postgresEWKBSelectExpr(columns []postgresColumnInfo, hints map[string]interface{}, fields []datatype.FieldInfo) (string, error) {
	geometryField := strings.TrimSpace(hintString(hints, plugin.TableReadHintGeometryField))
	targetSRID := postgresGeometryTargetSRID(hints)
	transformPolicy := strings.ToLower(strings.TrimSpace(hintString(hints, plugin.TableReadHintGeometryTransformPolicy)))
	selected := map[string]bool{}
	if len(fields) > 0 {
		for _, field := range fields {
			selected[field.Name] = true
		}
	}
	dialect := commonquery.ForDialect("postgresql")
	exprs := make([]string, 0, len(columns))
	for _, column := range columns {
		if len(selected) > 0 && !selected[column.Name] {
			continue
		}
		quoted := dialect.QuoteIdentifier(column.Name)
		if column.IsSpatial() && (geometryField == "" || column.Name == geometryField) {
			expr, err := postgresEWKBSpatialExpr(quoted, column, targetSRID, transformPolicy)
			if err != nil {
				return "", err
			}
			exprs = append(exprs, expr+" AS "+quoted)
			continue
		}
		exprs = append(exprs, quoted)
	}
	if len(exprs) == 0 {
		return "", nil
	}
	return strings.Join(exprs, ", "), nil
}

func postgresEWKBSpatialExpr(quoted string, column postgresColumnInfo, targetSRID int, transformPolicy string) (string, error) {
	sourceSRID := postgresColumnSRID(column)
	if targetSRID <= 0 || sourceSRID == targetSRID {
		return "ST_AsEWKB(" + quoted + ")", nil
	}
	if sourceSRID <= 0 {
		if transformPolicy == "required" {
			return "", postgresSpatialSourceSRIDPolicyError(column.Name)
		}
		return "ST_AsEWKB(" + quoted + ")", nil
	}
	if transformPolicy != "" && transformPolicy != "required" {
		return "ST_AsEWKB(" + quoted + ")", nil
	}
	return "ST_AsEWKB(ST_Transform(" + quoted + ", " + fmt.Sprint(targetSRID) + "))", nil
}

func postgresGeoJSONSpatialExpr(quoted string, column postgresColumnInfo, targetSRID int, transformPolicy string) (string, error) {
	sourceSRID := postgresColumnSRID(column)
	if targetSRID <= 0 || sourceSRID == targetSRID {
		return "ST_AsGeoJSON(" + quoted + ")::json", nil
	}
	if sourceSRID <= 0 {
		if transformPolicy == "required" {
			return "", postgresSpatialSourceSRIDPolicyError(column.Name)
		}
		return "ST_AsGeoJSON(" + quoted + ")::json", nil
	}
	if transformPolicy != "" && transformPolicy != "required" {
		return "ST_AsGeoJSON(" + quoted + ")::json", nil
	}
	return "ST_AsGeoJSON(ST_Transform(" + quoted + ", " + fmt.Sprint(targetSRID) + "))::json", nil
}

func postgresGeoJSONTargetSRID(hints map[string]interface{}) int {
	return postgresGeometryTargetSRID(hints)
}

func postgresGeometryTargetSRID(hints map[string]interface{}) int {
	if hints == nil {
		return 0
	}
	switch value := hints[plugin.TableReadHintGeometryTargetSRID].(type) {
	case int:
		return value
	case int8:
		return int(value)
	case int16:
		return int(value)
	case int32:
		return int(value)
	case int64:
		return int(value)
	case uint:
		return int(value)
	case uint8:
		return int(value)
	case uint16:
		return int(value)
	case uint32:
		return int(value)
	case uint64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	case string:
		var srid int
		_, _ = fmt.Sscanf(strings.TrimSpace(value), "%d", &srid)
		return srid
	default:
		return 0
	}
}

func postgresColumnSRID(column postgresColumnInfo) int {
	_, srid, _ := parsePostgresSpatialType(column.NativeType)
	return srid
}

func postgresSpatialSourceSRIDPolicyError(columnName string) error {
	return fmt.Errorf("postgresql geometry column %q requires a known source SRID for required spatial transform", columnName)
}

type postgresColumnInfo struct {
	Name             string
	DataType         string
	UDTName          string
	NativeType       string
	NumericPrecision sql.NullInt64
	NumericScale     sql.NullInt64
	Nullable         bool
	PrimaryKey       bool
	Comment          string
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
			format_type(a.atttypid, a.atttypmod) AS native_type,
			c.numeric_precision,
			c.numeric_scale,
			(c.is_nullable = 'YES') AS nullable
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
		if err := rows.Scan(
			&column.Name,
			&column.DataType,
			&column.UDTName,
			&column.NativeType,
			&column.NumericPrecision,
			&column.NumericScale,
			&column.Nullable,
		); err != nil {
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
	field := datatype.FieldInfo{
		Name:       column.Name,
		Type:       postgresCommonFieldType(column, nativeType),
		NativeType: nativeType,
		Nullable:   column.Nullable,
		PrimaryKey: column.PrimaryKey,
		Comment:    column.Comment,
	}
	if field.Type == datatype.FieldTypeDecimal && column.NumericPrecision.Valid && column.NumericPrecision.Int64 > 0 {
		field.Precision = int(column.NumericPrecision.Int64)
		if column.NumericScale.Valid && column.NumericScale.Int64 >= 0 {
			field.Scale = int(column.NumericScale.Int64)
		}
	}
	return field
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

func postgresSpatialInfoFromFieldsWithHints(fields []datatype.FieldInfo, hints map[string]interface{}) *datatype.SpatialInfo {
	spatialInfo := postgresSpatialInfoFromFields(fields)
	if spatialInfo == nil {
		return nil
	}
	targetSRID := postgresGeometryTargetSRID(hints)
	if targetSRID <= 0 {
		return spatialInfo
	}
	encoding := postgresGeometryEncodingHint(hints)
	if encoding != format.GeometryEncodingGeoJSON && encoding != format.GeometryEncodingEWKB {
		return spatialInfo
	}
	transformPolicy := strings.ToLower(strings.TrimSpace(hintString(hints, plugin.TableReadHintGeometryTransformPolicy)))
	if transformPolicy != "" && transformPolicy != "required" {
		return spatialInfo
	}
	geometryField := strings.TrimSpace(hintString(hints, plugin.TableReadHintGeometryField))
	targetCRS := datatype.EPSGCRSRef(targetSRID)
	next := spatialInfo.Clone()
	updated := false
	for i := range next.GeometryColumns {
		if geometryField != "" && !strings.EqualFold(next.GeometryColumns[i].Name, geometryField) {
			continue
		}
		if next.GeometryColumns[i].SRID == nil || *next.GeometryColumns[i].SRID <= 0 {
			continue
		}
		next.GeometryColumns[i].SRID = &targetSRID
		next.GeometryColumns[i].CRSRef = targetCRS
		updated = true
	}
	if !updated {
		return spatialInfo
	}
	next.SRID = &targetSRID
	next.CRSRef = targetCRS
	return next
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
	if strings.HasSuffix(lower, "z") || strings.HasSuffix(lower, "zm") {
		dimension = 3
	}
	geometryType := datatype.ParseGeometryType(normalized)
	if geometryType == datatype.GeometryTypeUnknown {
		return "", 0
	}
	return string(geometryType), dimension
}

func hintString(values map[string]interface{}, key string) string {
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
	db               *sql.DB
	tx               *sql.Tx
	cursorName       string
	fields           []datatype.FieldInfo
	spatialInfo      *datatype.SpatialInfo
	geometryEncoding format.GeometryEncoding
	closed           bool
	offset           int64
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

	batch, err := scanPostgresRowsToBatch(rows, s.fields, s.spatialInfo, s.geometryEncoding, s.offset)
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

func scanPostgresRowsToBatch(rows *sql.Rows, tableFields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo, geometryEncoding format.GeometryEncoding, offset int64) (*plugin.BatchData, error) {
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
				if geometryEncoding == format.GeometryEncodingEWKB && isGeometryColumn(tableFields, column) {
					row[column] = bytes
				} else {
					row[column] = string(bytes)
				}
				continue
			}
			row[column] = value
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgresql cursor rows: %w", err)
	}

	resolvedFields := tableFields
	if len(resolvedFields) == 0 {
		resolvedFields = postgresFieldsFromColumnTypes(rows, columns)
	}
	return &plugin.BatchData{
		Rows:    resultRows,
		Fields:  postgresReadBatchFields(columns, resolvedFields),
		Spatial: spatialInfo.Clone(),
		Offset:  offset,
	}, nil
}

func postgresFieldsFromColumnTypes(rows *sql.Rows, columns []string) []datatype.FieldInfo {
	columnTypes, err := rows.ColumnTypes()
	if err != nil || len(columnTypes) != len(columns) {
		return postgresReadBatchFields(columns, nil)
	}
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for index, columnType := range columnTypes {
		nativeType := strings.ToLower(strings.TrimSpace(columnType.DatabaseTypeName()))
		nullable, _ := columnType.Nullable()
		fields = append(fields, postgresFieldInfoFromColumn(postgresColumnInfo{
			Name: columns[index], DataType: nativeType, UDTName: nativeType,
			NativeType: nativeType, Nullable: nullable,
		}))
	}
	return fields
}

func isGeometryColumn(fields []datatype.FieldInfo, column string) bool {
	for _, field := range fields {
		if strings.EqualFold(field.Name, column) && datatype.IsSpatialFieldType(field.Type) {
			return true
		}
	}
	return false
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
