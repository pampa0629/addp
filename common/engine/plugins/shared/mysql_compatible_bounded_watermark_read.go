package shared

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

// MySQLCompatibleWatermarkColumn contains the column facts required to validate
// and serialize a stable watermark cursor.
type MySQLCompatibleWatermarkColumn struct {
	Name       string
	NativeType string
	Type       datatype.FieldType
	Nullable   bool
}

// MySQLCompatibleWatermarkTable is the engine adapter result consumed by the
// shared bounded watermark reader. Projection may contain engine-specific read
// expressions, such as MySQL spatial conversion to EWKB.
type MySQLCompatibleWatermarkTable struct {
	Fields      []datatype.FieldInfo
	SpatialInfo *datatype.SpatialInfo
	Columns     []MySQLCompatibleWatermarkColumn
	Projection  string
}

// MySQLCompatibleBoundedWatermarkReader owns the consistent-snapshot and
// compound-cursor algorithm shared by verified MySQL-compatible engines.
type MySQLCompatibleBoundedWatermarkReader struct {
	EngineType    string
	BuildDSN      func(plugin.ConnectionInfo) (string, error)
	DescribeTable func(context.Context, *sql.DB, string, string) (*MySQLCompatibleWatermarkTable, error)
	DecodeValue   func(string, interface{}, []datatype.FieldInfo, *datatype.SpatialInfo) (interface{}, error)
}

type mysqlCompatibleBoundedWatermarkSession struct {
	engineType   string
	db           *sql.DB
	tx           *sql.Tx
	rows         *sql.Rows
	fields       []datatype.FieldInfo
	spatialInfo  *datatype.SpatialInfo
	upper        *plugin.WatermarkCursor
	cursorFields []string
	cursorTypes  []MySQLCompatibleWatermarkColumn
	decodeValue  func(string, interface{}, []datatype.FieldInfo, *datatype.SpatialInfo) (interface{}, error)
	offset       int64
	exhausted    bool
	closed       bool
}

func (r MySQLCompatibleBoundedWatermarkReader) Open(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BoundedWatermarkReadOptions) (plugin.BoundedWatermarkReadSession, error) {
	engineType, err := r.validate()
	if err != nil {
		return nil, err
	}
	database, table, err := mysqlCompatibleWatermarkTablePathParts(engineType, path)
	if err != nil {
		return nil, err
	}
	requestedCursorFields, err := plugin.NormalizeWatermarkFields(opts.WatermarkField, opts.TieBreakers)
	if err != nil {
		return nil, err
	}
	dsn, err := r.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build %s watermark dsn: %w", engineType, err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s watermark connection: %w", engineType, err)
	}
	fail := func(err error) (plugin.BoundedWatermarkReadSession, error) {
		_ = db.Close()
		return nil, err
	}
	if err := validateMySQLCompatibleWatermarkSourceTable(ctx, db, engineType, database, table); err != nil {
		return fail(err)
	}
	tableInfo, err := r.DescribeTable(ctx, db, database, table)
	if err != nil {
		return fail(err)
	}
	if err := validateMySQLCompatibleWatermarkTable(engineType, tableInfo); err != nil {
		return fail(err)
	}
	cursorFields, cursorTypes, err := resolveMySQLCompatibleWatermarkCursorFields(engineType, requestedCursorFields, tableInfo.Columns)
	if err != nil {
		return fail(err)
	}
	if err := validateMySQLCompatibleWatermarkTieBreakers(ctx, db, engineType, database, table, cursorFields[1:]); err != nil {
		return fail(err)
	}
	if opts.Start != nil && len(opts.Start.Values) != len(cursorFields) {
		return fail(fmt.Errorf("%s watermark start cursor has %d values, want %d", engineType, len(opts.Start.Values), len(cursorFields)))
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return fail(fmt.Errorf("begin %s watermark consistent snapshot: %w", engineType, err))
	}
	failTx := func(err error) (plugin.BoundedWatermarkReadSession, error) {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, err
	}
	dialect := commonquery.ForDialect("mysql")
	qualified := dialect.QualifiedTable(database, table)
	quotedCursor := quoteMySQLCompatibleWatermarkFields(cursorFields)
	upperQuery := "SELECT " + strings.Join(quotedCursor, ", ") + " FROM " + qualified + " ORDER BY " + orderMySQLCompatibleWatermarkFields(quotedCursor, "DESC") + " LIMIT 1"
	upperValues := make([]interface{}, len(cursorFields))
	upperPointers := make([]interface{}, len(cursorFields))
	for index := range upperValues {
		upperPointers[index] = &upperValues[index]
	}
	var upper *plugin.WatermarkCursor
	if err := tx.QueryRowContext(ctx, upperQuery).Scan(upperPointers...); err != nil {
		if err != sql.ErrNoRows {
			return failTx(fmt.Errorf("freeze %s watermark upper bound: %w", engineType, err))
		}
	} else {
		values, err := stringifyMySQLCompatibleCursor(engineType, upperValues, cursorTypes)
		if err != nil {
			return failTx(fmt.Errorf("freeze %s watermark upper bound: %w", engineType, err))
		}
		upper = &plugin.WatermarkCursor{Values: values}
	}

	query := "SELECT " + tableInfo.Projection + " FROM " + qualified
	args := make([]interface{}, 0, len(cursorFields)*2)
	predicates := make([]string, 0, 2)
	if upper == nil {
		predicates = append(predicates, "FALSE")
	} else {
		if opts.Start != nil {
			predicates = append(predicates, mysqlCompatibleWatermarkTuplePredicate(quotedCursor, ">"))
			args = appendMySQLCompatibleWatermarkCursorArgs(args, opts.Start.Values)
		}
		predicates = append(predicates, mysqlCompatibleWatermarkTuplePredicate(quotedCursor, "<="))
		args = appendMySQLCompatibleWatermarkCursorArgs(args, upper.Values)
	}
	query += " WHERE " + strings.Join(predicates, " AND ") + " ORDER BY " + strings.Join(quotedCursor, ", ")
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return failTx(fmt.Errorf("open %s watermark row stream: %w", engineType, err))
	}
	decodeValue := r.DecodeValue
	if decodeValue == nil {
		decodeValue = decodeMySQLCompatibleScalarValue
	}
	return &mysqlCompatibleBoundedWatermarkSession{
		engineType: engineType, db: db, tx: tx, rows: rows,
		fields: append([]datatype.FieldInfo(nil), tableInfo.Fields...), spatialInfo: tableInfo.SpatialInfo.Clone(),
		upper: upper, cursorFields: cursorFields, cursorTypes: cursorTypes, decodeValue: decodeValue,
	}, nil
}

func (r MySQLCompatibleBoundedWatermarkReader) validate() (string, error) {
	engineType := strings.ToLower(strings.TrimSpace(r.EngineType))
	if engineType == "" {
		return "", fmt.Errorf("mysql-compatible bounded watermark reader requires engine type")
	}
	if r.BuildDSN == nil {
		return "", fmt.Errorf("%s bounded watermark reader requires BuildDSN", engineType)
	}
	if r.DescribeTable == nil {
		return "", fmt.Errorf("%s bounded watermark reader requires DescribeTable", engineType)
	}
	return engineType, nil
}

func validateMySQLCompatibleWatermarkTable(engineType string, table *MySQLCompatibleWatermarkTable) error {
	if table == nil || len(table.Fields) == 0 || len(table.Columns) == 0 {
		return fmt.Errorf("%s watermark source has no columns", engineType)
	}
	if len(table.Fields) != len(table.Columns) {
		return fmt.Errorf("%s watermark table description has %d fields and %d columns", engineType, len(table.Fields), len(table.Columns))
	}
	if strings.TrimSpace(table.Projection) == "" {
		return fmt.Errorf("%s watermark table description requires a projection", engineType)
	}
	return nil
}

// DescribeNonSpatialMySQLCompatibleWatermarkTable reads the common
// information_schema surface used by non-spatial MySQL-compatible engines.
func DescribeNonSpatialMySQLCompatibleWatermarkTable(ctx context.Context, db *sql.DB, engineType, database, table string, mapFieldType func(string) datatype.FieldType) (*MySQLCompatibleWatermarkTable, error) {
	if mapFieldType == nil {
		return nil, fmt.Errorf("%s watermark table description requires a field type mapper", engineType)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, column_type, numeric_precision, numeric_scale,
		       (is_nullable = 'YES') AS is_nullable, (column_key = 'PRI') AS primary_key
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query %s watermark table columns: %w", engineType, err)
	}
	defer rows.Close()
	dialect := commonquery.ForDialect("mysql")
	result := &MySQLCompatibleWatermarkTable{}
	projection := make([]string, 0)
	for rows.Next() {
		var name, nativeType string
		var precision, scale sql.NullInt64
		var nullable, primaryKey bool
		if err := rows.Scan(&name, &nativeType, &precision, &scale, &nullable, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan %s watermark table column: %w", engineType, err)
		}
		fieldType := mapFieldType(nativeType)
		if datatype.IsSpatialFieldType(fieldType) {
			return nil, fmt.Errorf("%s bounded watermark read does not support spatial field %q", engineType, name)
		}
		field := datatype.FieldInfo{Name: name, Type: fieldType, NativeType: nativeType, Nullable: nullable, PrimaryKey: primaryKey, OrdinalPosition: len(result.Fields) + 1}
		if precision.Valid {
			field.Precision = int(precision.Int64)
		}
		if scale.Valid {
			field.Scale = int(scale.Int64)
		}
		result.Fields = append(result.Fields, field)
		result.Columns = append(result.Columns, MySQLCompatibleWatermarkColumn{Name: name, NativeType: nativeType, Type: fieldType, Nullable: nullable})
		projection = append(projection, dialect.QuoteIdentifier(name))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s watermark table columns: %w", engineType, err)
	}
	result.Projection = strings.Join(projection, ", ")
	return result, nil
}

func validateMySQLCompatibleWatermarkSourceTable(ctx context.Context, db *sql.DB, engineType, database, table string) error {
	var tableType string
	var engine sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT table_type, engine FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ?
	`, database, table).Scan(&tableType, &engine)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s watermark source %s.%s does not exist", engineType, database, table)
		}
		return fmt.Errorf("read %s watermark source table %s.%s: %w", engineType, database, table, err)
	}
	if !strings.EqualFold(strings.TrimSpace(tableType), "BASE TABLE") {
		return fmt.Errorf("%s watermark source %s.%s must be a base table, got %q", engineType, database, table, tableType)
	}
	if !engine.Valid || !strings.EqualFold(strings.TrimSpace(engine.String), "InnoDB") {
		return fmt.Errorf("%s watermark source %s.%s must use InnoDB, got %q", engineType, database, table, engine.String)
	}
	return nil
}

func resolveMySQLCompatibleWatermarkCursorFields(engineType string, requested []string, columns []MySQLCompatibleWatermarkColumn) ([]string, []MySQLCompatibleWatermarkColumn, error) {
	columnsByName := make(map[string]MySQLCompatibleWatermarkColumn, len(columns))
	for _, column := range columns {
		columnsByName[strings.ToLower(column.Name)] = column
	}
	fields := make([]string, 0, len(requested))
	resolved := make([]MySQLCompatibleWatermarkColumn, 0, len(requested))
	seen := map[string]bool{}
	for _, name := range requested {
		column, ok := columnsByName[strings.ToLower(name)]
		if !ok {
			return nil, nil, fmt.Errorf("%s watermark cursor field %q does not exist", engineType, name)
		}
		key := strings.ToLower(column.Name)
		if seen[key] {
			return nil, nil, fmt.Errorf("%s watermark cursor fields must be unique ignoring case", engineType)
		}
		if column.Nullable {
			return nil, nil, fmt.Errorf("%s watermark cursor field %q must be NOT NULL", engineType, column.Name)
		}
		if !mySQLCompatibleWatermarkCursorTypeSupported(column.Type) {
			return nil, nil, fmt.Errorf("%s watermark cursor field %q has unsupported type %q", engineType, column.Name, column.NativeType)
		}
		seen[key] = true
		fields = append(fields, column.Name)
		resolved = append(resolved, column)
	}
	return fields, resolved, nil
}

func mySQLCompatibleWatermarkCursorTypeSupported(fieldType datatype.FieldType) bool {
	switch datatype.ParseFieldType(string(fieldType)) {
	case datatype.FieldTypeBool, datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeFloat,
		datatype.FieldTypeDouble, datatype.FieldTypeDecimal, datatype.FieldTypeString, datatype.FieldTypeDate,
		datatype.FieldTypeTime, datatype.FieldTypeTimestamp, datatype.FieldTypeUUID:
		return true
	default:
		return false
	}
}

func validateMySQLCompatibleWatermarkTieBreakers(ctx context.Context, db *sql.DB, engineType, database, table string, tieBreakers []string) error {
	if len(tieBreakers) == 0 {
		return fmt.Errorf("%s watermark requires tie_breaker fields", engineType)
	}
	indexes, err := mysqlCompatibleUniqueIndexes(ctx, db, engineType, "watermark", database, table)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		if equalMySQLCompatibleIdentifiers(index, tieBreakers) {
			return nil
		}
	}
	return fmt.Errorf("%s watermark tie_breaker %v must exactly match a non-prefix unique or primary key", engineType, tieBreakers)
}

func mysqlCompatibleUniqueIndexes(ctx context.Context, db *sql.DB, engineType, operation, database, table string) ([][]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT index_name, seq_in_index, column_name, sub_part
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = ? AND non_unique = 0
		ORDER BY index_name, seq_in_index
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query %s %s unique constraints: %w", engineType, operation, err)
	}
	defer rows.Close()
	indexOrder := make([]string, 0)
	indexColumns := map[string][]string{}
	for rows.Next() {
		var indexName string
		var sequence int
		var column sql.NullString
		var prefixLength sql.NullInt64
		if err := rows.Scan(&indexName, &sequence, &column, &prefixLength); err != nil {
			return nil, fmt.Errorf("scan %s %s unique constraint: %w", engineType, operation, err)
		}
		if _, exists := indexColumns[indexName]; !exists {
			indexOrder = append(indexOrder, indexName)
		}
		name := ""
		if column.Valid && !prefixLength.Valid {
			name = column.String
		}
		indexColumns[indexName] = append(indexColumns[indexName], name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s %s unique constraints: %w", engineType, operation, err)
	}
	result := make([][]string, 0, len(indexOrder))
	for _, indexName := range indexOrder {
		result = append(result, indexColumns[indexName])
	}
	return result, nil
}

func equalMySQLCompatibleIdentifiers(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] == "" || !strings.EqualFold(left[index], right[index]) {
			return false
		}
	}
	return true
}

func stringifyMySQLCompatibleCursor(engineType string, values []interface{}, columns []MySQLCompatibleWatermarkColumn) ([]string, error) {
	if len(values) != len(columns) {
		return nil, fmt.Errorf("%s watermark cursor has %d values, want %d", engineType, len(values), len(columns))
	}
	result := make([]string, len(values))
	for index, value := range values {
		if value == nil {
			return nil, fmt.Errorf("%s watermark cursor field %q is NULL", engineType, columns[index].Name)
		}
		switch typed := value.(type) {
		case time.Time:
			if columns[index].Type == datatype.FieldTypeDate {
				result[index] = typed.Format("2006-01-02")
			} else {
				result[index] = typed.Format("2006-01-02 15:04:05.999999")
			}
		case []byte:
			result[index] = string(typed)
		default:
			result[index] = fmt.Sprint(typed)
		}
	}
	return result, nil
}

func (s *mysqlCompatibleBoundedWatermarkSession) UpperBound() *plugin.WatermarkCursor {
	if s.upper == nil {
		return nil
	}
	return &plugin.WatermarkCursor{Values: append([]string(nil), s.upper.Values...)}
}

func (s *mysqlCompatibleBoundedWatermarkSession) TableInfo() (*datatype.TableInfo, *datatype.SpatialInfo) {
	return &datatype.TableInfo{Fields: append([]datatype.FieldInfo(nil), s.fields...)}, s.spatialInfo.Clone()
}

func (s *mysqlCompatibleBoundedWatermarkSession) PositionForRow(row map[string]interface{}) (*plugin.WatermarkCursor, error) {
	values := make([]interface{}, 0, len(s.cursorFields))
	for _, name := range s.cursorFields {
		value, ok := mysqlCompatibleWatermarkRowValue(row, name)
		if !ok || value == nil {
			return nil, fmt.Errorf("%s watermark row is missing cursor field %q", s.engineType, name)
		}
		values = append(values, value)
	}
	canonical, err := stringifyMySQLCompatibleCursor(s.engineType, values, s.cursorTypes)
	if err != nil {
		return nil, err
	}
	return &plugin.WatermarkCursor{Values: canonical}, nil
}

func mysqlCompatibleWatermarkRowValue(row map[string]interface{}, name string) (interface{}, bool) {
	if value, ok := row[name]; ok {
		return value, true
	}
	for candidate, value := range row {
		if strings.EqualFold(candidate, name) {
			return value, true
		}
	}
	return nil, false
}

func (s *mysqlCompatibleBoundedWatermarkSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("%s watermark session is closed", s.engineType)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 1000
	}
	if s.exhausted {
		return &plugin.BatchData{Fields: append([]datatype.FieldInfo(nil), s.fields...), Spatial: s.spatialInfo.Clone(), Offset: s.offset}, nil
	}
	columns, err := s.rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get %s watermark row columns: %w", s.engineType, err)
	}
	resultRows := make([]map[string]interface{}, 0, limit)
	for len(resultRows) < limit {
		if !s.rows.Next() {
			s.exhausted = true
			break
		}
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := s.rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan %s watermark row: %w", s.engineType, err)
		}
		row := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			value, err := s.decodeValue(column, values[index], s.fields, s.spatialInfo)
			if err != nil {
				return nil, err
			}
			row[column] = value
		}
		resultRows = append(resultRows, row)
	}
	if err := s.rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s watermark rows: %w", s.engineType, err)
	}
	batch := &plugin.BatchData{Rows: resultRows, Fields: append([]datatype.FieldInfo(nil), s.fields...), Spatial: s.spatialInfo.Clone(), Offset: s.offset}
	s.offset += int64(len(resultRows))
	return batch, nil
}

func (s *mysqlCompatibleBoundedWatermarkSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	var result error
	if s.rows != nil {
		if err := s.rows.Close(); err != nil {
			result = err
		}
	}
	if s.tx != nil {
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && result == nil {
			result = err
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}

func mysqlCompatibleWatermarkTablePathParts(engineType string, path plugin.EngineCatalogPath) (string, string, error) {
	if len(path.Segments) < 2 {
		return "", "", fmt.Errorf("%s table path requires database/table catalog path", engineType)
	}
	database := strings.TrimSpace(path.Segments[len(path.Segments)-2].Name)
	table := strings.TrimSpace(path.Segments[len(path.Segments)-1].Name)
	if database == "" || table == "" {
		return "", "", fmt.Errorf("%s table path requires non-empty database and table", engineType)
	}
	return database, table, nil
}

func quoteMySQLCompatibleWatermarkFields(fields []string) []string {
	dialect := commonquery.ForDialect("mysql")
	quoted := make([]string, 0, len(fields))
	for _, field := range fields {
		quoted = append(quoted, dialect.QuoteIdentifier(field))
	}
	return quoted
}

func orderMySQLCompatibleWatermarkFields(fields []string, direction string) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, field+" "+direction)
	}
	return strings.Join(parts, ", ")
}

func mysqlCompatibleWatermarkTuplePredicate(quotedFields []string, operator string) string {
	params := make([]string, len(quotedFields))
	for index := range params {
		params[index] = "?"
	}
	return "(" + strings.Join(quotedFields, ", ") + ") " + operator + " (" + strings.Join(params, ", ") + ")"
}

func appendMySQLCompatibleWatermarkCursorArgs(args []interface{}, values []string) []interface{} {
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func decodeMySQLCompatibleScalarValue(_ string, value interface{}, _ []datatype.FieldInfo, _ *datatype.SpatialInfo) (interface{}, error) {
	if bytes, ok := value.([]byte); ok {
		return string(bytes), nil
	}
	return value, nil
}
