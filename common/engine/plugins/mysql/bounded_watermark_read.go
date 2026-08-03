package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

type mysqlBoundedWatermarkSession struct {
	db           *sql.DB
	tx           *sql.Tx
	rows         *sql.Rows
	fields       []datatype.FieldInfo
	upper        *plugin.WatermarkCursor
	cursorFields []string
	offset       int64
	closed       bool
}

func (p *MySQLPlugin) OpenBoundedWatermarkRead(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.CatalogPath,
	opts plugin.BoundedWatermarkReadOptions,
) (plugin.BoundedWatermarkReadSession, error) {
	database, table, err := mysqlTablePathParts(path)
	if err != nil {
		return nil, err
	}
	requestedCursorFields, err := plugin.NormalizeWatermarkFields(opts.WatermarkField, opts.TieBreakers)
	if err != nil {
		return nil, err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build mysql watermark dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql watermark connection: %w", err)
	}
	fail := func(err error) (plugin.BoundedWatermarkReadSession, error) {
		_ = db.Close()
		return nil, err
	}
	if err := validateMySQLWatermarkSourceTable(ctx, db, database, table); err != nil {
		return fail(err)
	}
	columns, err := mysqlTableColumns(ctx, db, database, table)
	if err != nil {
		return fail(err)
	}
	fields, columnsByName, err := mysqlWatermarkFields(columns)
	if err != nil {
		return fail(err)
	}
	cursorFields, cursorColumns, err := resolveMySQLWatermarkCursorFields(requestedCursorFields, columnsByName)
	if err != nil {
		return fail(err)
	}
	if err := validateMySQLWatermarkCursor(ctx, db, database, table, opts.TieBreakers, columnsByName); err != nil {
		return fail(err)
	}
	if opts.Start != nil && len(opts.Start.Values) != len(cursorFields) {
		return fail(fmt.Errorf("mysql watermark start cursor has %d values, want %d", len(opts.Start.Values), len(cursorFields)))
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return fail(fmt.Errorf("begin mysql watermark consistent snapshot: %w", err))
	}
	failTx := func(err error) (plugin.BoundedWatermarkReadSession, error) {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, err
	}
	dialect := mysqlDialect()
	qualified := dialect.QualifiedTable(database, table)
	quotedCursor := quoteMySQLFields(cursorFields)
	upperQuery := "SELECT " + strings.Join(quotedCursor, ", ") + " FROM " + qualified +
		" ORDER BY " + orderMySQLFields(quotedCursor, "DESC") + " LIMIT 1"
	upperValues := make([]interface{}, len(cursorFields))
	upperPointers := make([]interface{}, len(cursorFields))
	for index := range upperValues {
		upperPointers[index] = &upperValues[index]
	}
	var upper *plugin.WatermarkCursor
	if err := tx.QueryRowContext(ctx, upperQuery).Scan(upperPointers...); err != nil {
		if err != sql.ErrNoRows {
			return failTx(fmt.Errorf("freeze mysql watermark upper bound: %w", err))
		}
	} else {
		values, err := stringifyMySQLCursor(upperValues, cursorColumns)
		if err != nil {
			return failTx(fmt.Errorf("freeze mysql watermark upper bound: %w", err))
		}
		upper = &plugin.WatermarkCursor{Values: values}
	}

	selectFields := quoteMySQLFields(mysqlFieldColumns(fields))
	query := "SELECT " + strings.Join(selectFields, ", ") + " FROM " + qualified
	args := make([]interface{}, 0, len(cursorFields)*2)
	predicates := make([]string, 0, 2)
	if upper == nil {
		predicates = append(predicates, "FALSE")
	} else {
		if opts.Start != nil {
			predicates = append(predicates, mysqlTuplePredicate(quotedCursor, ">"))
			args = appendMySQLCursorArgs(args, opts.Start.Values)
		}
		predicates = append(predicates, mysqlTuplePredicate(quotedCursor, "<="))
		args = appendMySQLCursorArgs(args, upper.Values)
	}
	query += " WHERE " + strings.Join(predicates, " AND ") + " ORDER BY " + strings.Join(quotedCursor, ", ")
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return failTx(fmt.Errorf("open mysql watermark row stream: %w", err))
	}
	return &mysqlBoundedWatermarkSession{
		db: db, tx: tx, rows: rows, fields: fields, upper: upper, cursorFields: cursorFields,
	}, nil
}

func validateMySQLWatermarkSourceTable(ctx context.Context, db *sql.DB, database, table string) error {
	var tableType string
	var engine sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT table_type, engine
		FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ?
	`, database, table).Scan(&tableType, &engine)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("mysql watermark source %s.%s does not exist", database, table)
		}
		return fmt.Errorf("read mysql watermark source table %s.%s: %w", database, table, err)
	}
	if !strings.EqualFold(strings.TrimSpace(tableType), "BASE TABLE") {
		return fmt.Errorf("mysql watermark source %s.%s must be a base table, got %q", database, table, tableType)
	}
	if !engine.Valid || !strings.EqualFold(strings.TrimSpace(engine.String), "InnoDB") {
		return fmt.Errorf("mysql watermark source %s.%s must use InnoDB, got %q", database, table, engine.String)
	}
	return nil
}

func mysqlWatermarkFields(columns []mysqlColumnInfo) ([]datatype.FieldInfo, map[string]mysqlColumnInfo, error) {
	if len(columns) == 0 {
		return nil, nil, fmt.Errorf("mysql watermark source has no columns")
	}
	fields := make([]datatype.FieldInfo, 0, len(columns))
	columnsByName := make(map[string]mysqlColumnInfo, len(columns))
	for index, column := range columns {
		field := mysqlFieldInfoFromColumn(column)
		field.OrdinalPosition = index + 1
		if datatype.IsSpatialFieldType(field.Type) {
			return nil, nil, fmt.Errorf("mysql bounded watermark source does not support spatial column %q", column.Name)
		}
		fields = append(fields, field)
		columnsByName[strings.ToLower(column.Name)] = column
	}
	return fields, columnsByName, nil
}

func mysqlFieldInfoFromColumn(column mysqlColumnInfo) datatype.FieldInfo {
	field := datatype.FieldInfo{
		Name:       column.Name,
		Type:       mysqlCommonFieldType(column),
		NativeType: mysqlColumnNativeType(column),
		Nullable:   column.Nullable,
	}
	if column.NumericPrecision.Valid {
		field.Precision = int(column.NumericPrecision.Int64)
	}
	if column.NumericScale.Valid {
		field.Scale = int(column.NumericScale.Int64)
	}
	return field
}

func resolveMySQLWatermarkCursorFields(
	requested []string,
	columnsByName map[string]mysqlColumnInfo,
) ([]string, []mysqlColumnInfo, error) {
	fields := make([]string, 0, len(requested))
	columns := make([]mysqlColumnInfo, 0, len(requested))
	seen := map[string]bool{}
	for _, name := range requested {
		column, ok := columnsByName[strings.ToLower(name)]
		if !ok {
			return nil, nil, fmt.Errorf("mysql watermark cursor field %q does not exist", name)
		}
		key := strings.ToLower(column.Name)
		if seen[key] {
			return nil, nil, fmt.Errorf("mysql watermark cursor fields must be unique ignoring case")
		}
		if column.Nullable {
			return nil, nil, fmt.Errorf("mysql watermark cursor field %q must be NOT NULL", column.Name)
		}
		fieldType := mysqlCommonFieldType(column)
		if !mysqlWatermarkCursorTypeSupported(fieldType) {
			return nil, nil, fmt.Errorf("mysql watermark cursor field %q has unsupported type %q", column.Name, mysqlColumnNativeType(column))
		}
		seen[key] = true
		fields = append(fields, column.Name)
		columns = append(columns, column)
	}
	return fields, columns, nil
}

func mysqlWatermarkCursorTypeSupported(fieldType datatype.FieldType) bool {
	switch datatype.ParseFieldType(string(fieldType)) {
	case datatype.FieldTypeBool,
		datatype.FieldTypeInt,
		datatype.FieldTypeBigInt,
		datatype.FieldTypeFloat,
		datatype.FieldTypeDouble,
		datatype.FieldTypeDecimal,
		datatype.FieldTypeString,
		datatype.FieldTypeDate,
		datatype.FieldTypeTime,
		datatype.FieldTypeTimestamp,
		datatype.FieldTypeUUID:
		return true
	default:
		return false
	}
}

func validateMySQLWatermarkCursor(
	ctx context.Context,
	db *sql.DB,
	database, table string,
	tieBreakers []string,
	columnsByName map[string]mysqlColumnInfo,
) error {
	if len(tieBreakers) == 0 {
		return fmt.Errorf("mysql watermark requires tie_breaker fields")
	}
	want := make([]string, 0, len(tieBreakers))
	for _, name := range tieBreakers {
		column, ok := columnsByName[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return fmt.Errorf("mysql watermark tie_breaker field %q does not exist", name)
		}
		want = append(want, column.Name)
	}
	indexes, err := mysqlUniqueIndexes(ctx, db, database, table)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		if equalMySQLIdentifierLists(index, want) {
			return nil
		}
	}
	return fmt.Errorf("mysql watermark tie_breaker %v must exactly match a non-prefix unique or primary key", want)
}

func equalMySQLIdentifierLists(left, right []string) bool {
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

func quoteMySQLFields(fields []string) []string {
	quoted := make([]string, 0, len(fields))
	for _, field := range fields {
		quoted = append(quoted, mysqlDialect().QuoteIdentifier(field))
	}
	return quoted
}

func orderMySQLFields(fields []string, direction string) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, field+" "+direction)
	}
	return strings.Join(parts, ", ")
}

func mysqlTuplePredicate(quotedFields []string, operator string) string {
	params := make([]string, len(quotedFields))
	for index := range params {
		params[index] = "?"
	}
	return "(" + strings.Join(quotedFields, ", ") + ") " + operator + " (" + strings.Join(params, ", ") + ")"
}

func appendMySQLCursorArgs(args []interface{}, values []string) []interface{} {
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func stringifyMySQLCursor(values []interface{}, columns []mysqlColumnInfo) ([]string, error) {
	if len(values) != len(columns) {
		return nil, fmt.Errorf("mysql watermark cursor has %d values, want %d", len(values), len(columns))
	}
	result := make([]string, len(values))
	for index, value := range values {
		if value == nil {
			return nil, fmt.Errorf("mysql watermark cursor field %q is NULL", columns[index].Name)
		}
		switch typed := value.(type) {
		case time.Time:
			if mysqlCommonFieldType(columns[index]) == datatype.FieldTypeDate {
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

func (s *mysqlBoundedWatermarkSession) UpperBound() *plugin.WatermarkCursor {
	if s.upper == nil {
		return nil
	}
	return &plugin.WatermarkCursor{Values: append([]string(nil), s.upper.Values...)}
}

func (s *mysqlBoundedWatermarkSession) TableInfo() (*datatype.TableInfo, *datatype.SpatialInfo) {
	return &datatype.TableInfo{Fields: append([]datatype.FieldInfo(nil), s.fields...)}, nil
}

func (s *mysqlBoundedWatermarkSession) PositionForRow(row map[string]interface{}) (*plugin.WatermarkCursor, error) {
	values := make([]interface{}, 0, len(s.cursorFields))
	columns := make([]mysqlColumnInfo, 0, len(s.cursorFields))
	fieldsByName := make(map[string]datatype.FieldInfo, len(s.fields))
	for _, field := range s.fields {
		fieldsByName[strings.ToLower(field.Name)] = field
	}
	for _, name := range s.cursorFields {
		value, ok := mysqlRowValue(row, name)
		if !ok || value == nil {
			return nil, fmt.Errorf("mysql watermark row is missing cursor field %q", name)
		}
		field := fieldsByName[strings.ToLower(name)]
		columns = append(columns, mysqlColumnInfo{Name: field.Name, NativeType: field.NativeType})
		values = append(values, value)
	}
	canonical, err := stringifyMySQLCursor(values, columns)
	if err != nil {
		return nil, err
	}
	return &plugin.WatermarkCursor{Values: canonical}, nil
}

func mysqlRowValue(row map[string]interface{}, name string) (interface{}, bool) {
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

func (s *mysqlBoundedWatermarkSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("mysql watermark session is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 1000
	}
	columns, err := s.rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get mysql watermark row columns: %w", err)
	}
	resultRows := make([]map[string]interface{}, 0, limit)
	for len(resultRows) < limit && s.rows.Next() {
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := s.rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan mysql watermark row: %w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			if bytes, ok := values[index].([]byte); ok {
				row[column] = string(bytes)
			} else {
				row[column] = values[index]
			}
		}
		resultRows = append(resultRows, row)
	}
	if err := s.rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql watermark rows: %w", err)
	}
	batch := &plugin.BatchData{
		Rows: resultRows, Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset,
	}
	s.offset += int64(len(resultRows))
	return batch, nil
}

func (s *mysqlBoundedWatermarkSession) Close(context.Context) error {
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
