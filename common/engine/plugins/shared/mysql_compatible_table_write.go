package shared

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/mysql"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
)

const mysqlCompatibleDefaultInsertChunkSize = 1000
const mysqlCompatibleMaxBindParams = 65535

// MySQLCompatibleTableWriter implements the non-spatial table write contract
// shared by engines that expose a verified MySQL-compatible SQL surface.
// Engine-specific plugins remain responsible for declaring capabilities and
// keeping unsupported extensions, such as spatial writes, outside this path.
type MySQLCompatibleTableWriter struct {
	EngineType      string
	EngineName      string
	DefaultPort     int
	WriterConnector string
}

func (w MySQLCompatibleTableWriter) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteOptions) error {
	if err := w.validate(); err != nil {
		return err
	}
	if HasSpatialTableWrite(opts.Fields, opts.SpatialInfo) {
		return fmt.Errorf("%s table write does not support spatial fields", w.engineType())
	}
	database, table, err := w.tablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := w.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build %s dsn: %w", w.engineType(), err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open %s connection: %w", w.engineType(), err)
	}
	defer db.Close()

	return w.createTableIfNotExists(ctx, db, database, table, opts.Fields)
}

// DeleteResource removes exactly the table addressed by path. It never drops
// a database and exists to support an explicit replace policy.
func (w MySQLCompatibleTableWriter) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	if err := w.validate(); err != nil {
		return err
	}
	database, table, err := w.tablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := w.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build %s dsn: %w", w.engineType(), err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open %s connection: %w", w.engineType(), err)
	}
	defer db.Close()

	statement := "DROP TABLE IF EXISTS " + mysqlCompatibleDialect().QualifiedTable(database, table)
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("drop %s table %s.%s: %w", w.engineType(), database, table, err)
	}
	return nil
}

func (w MySQLCompatibleTableWriter) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if err := w.validate(); err != nil {
		return nil, err
	}
	markerProvider := w.engineType() + ".table_write_session"
	if err := resume.RejectUnsupported(opts.ResumeMarker, markerProvider); err != nil {
		return nil, err
	}
	if !w.supportsInsertMethod(opts.Method) {
		return nil, fmt.Errorf("%s table write session only supports insert method", w.engineType())
	}
	if HasSpatialTableWrite(opts.Fields, opts.SpatialInfo) {
		return nil, fmt.Errorf("%s table write session does not support spatial fields", w.engineType())
	}

	database, table, err := w.tablePathParts(path)
	if err != nil {
		return nil, err
	}
	columns := mysqlCompatibleFieldColumns(opts.Fields)
	if len(columns) == 0 {
		return nil, fmt.Errorf("%s table write session requires fields", w.engineType())
	}
	if len(columns) > mysqlCompatibleMaxBindParams {
		return nil, fmt.Errorf("%s table write session has %d columns, exceeding max bind parameters %d", w.engineType(), len(columns), mysqlCompatibleMaxBindParams)
	}

	dsn, err := w.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build %s dsn: %w", w.engineType(), err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s connection: %w", w.engineType(), err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("begin %s table write session: %w", w.engineType(), err)
	}

	return &mysqlCompatibleTableWriteSession{
		db:              db,
		tx:              tx,
		engineType:      w.engineType(),
		writerConnector: w.writerConnector(),
		database:        database,
		table:           table,
		columns:         columns,
		chunkSize:       mysqlCompatibleInsertChunkSize(len(columns), mysqlCompatibleDefaultInsertChunkSize),
	}, nil
}

// HasSpatialTableWrite reports whether a table write request carries spatial
// fields or spatial metadata and therefore needs an engine-specific provider.
func HasSpatialTableWrite(fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) bool {
	for _, field := range fields {
		if datatype.IsSpatialFieldType(field.Type) {
			return true
		}
	}
	return spatialInfo != nil && len(spatialInfo.GeometryColumns) > 0
}

func (w MySQLCompatibleTableWriter) validate() error {
	if strings.TrimSpace(w.EngineType) == "" {
		return fmt.Errorf("mysql-compatible table writer requires engine type")
	}
	if w.DefaultPort <= 0 {
		return fmt.Errorf("%s table writer requires a positive default port", w.engineType())
	}
	return nil
}

func (w MySQLCompatibleTableWriter) engineType() string {
	return strings.ToLower(strings.TrimSpace(w.EngineType))
}

func (w MySQLCompatibleTableWriter) engineName() string {
	if name := strings.TrimSpace(w.EngineName); name != "" {
		return name
	}
	return w.EngineType
}

func (w MySQLCompatibleTableWriter) writerConnector() string {
	if connector := strings.ToLower(strings.TrimSpace(w.WriterConnector)); connector != "" {
		return connector
	}
	return w.engineType() + "_insert"
}

func (w MySQLCompatibleTableWriter) supportsInsertMethod(method string) bool {
	normalized := strings.ToLower(strings.TrimSpace(method))
	return normalized == "" || normalized == "insert" || normalized == "copy" || normalized == w.writerConnector()
}

func (w MySQLCompatibleTableWriter) serverDSN(connInfo plugin.ConnectionInfo) (string, error) {
	parts := plugin.ParseDriverConnInfo(connInfo, w.DefaultPort, "")
	if err := parts.Require(w.engineName(), "host", "user"); err != nil {
		return "", err
	}
	return plugin.MySQLStyleDSN(parts.User, parts.Password, parts.Host, parts.Port, "", map[string]string{
		"parseTime": "true",
		"timeout":   "10s",
		"charset":   "utf8mb4",
		"collation": "utf8mb4_unicode_ci",
	}), nil
}

func (w MySQLCompatibleTableWriter) tablePathParts(path plugin.EngineCatalogPath) (string, string, error) {
	if len(path.Segments) < 2 {
		return "", "", fmt.Errorf("%s table path requires database/table catalog path", w.engineType())
	}
	database := strings.TrimSpace(path.Segments[len(path.Segments)-2].Name)
	table := strings.TrimSpace(path.Segments[len(path.Segments)-1].Name)
	if database == "" || table == "" {
		return "", "", fmt.Errorf("%s table path requires non-empty database and table", w.engineType())
	}
	return database, table, nil
}

func (w MySQLCompatibleTableWriter) createTableIfNotExists(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo) error {
	return w.createTableIfNotExistsWithPrimaryKeys(ctx, db, database, table, fields, nil)
}

func (w MySQLCompatibleTableWriter) createTableIfNotExistsWithPrimaryKeys(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo, explicitPrimaryKeys []string) error {
	writeFields := mysqlCompatibleWriteFields(fields)
	if len(writeFields) == 0 {
		return fmt.Errorf("%s table write prepare requires at least one named field", w.engineType())
	}
	dialect := mysqlCompatibleDialect()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+dialect.QuoteIdentifier(database)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return fmt.Errorf("create %s database %s: %w", w.engineType(), database, err)
	}

	definitions := make([]string, 0, len(writeFields)+1)
	primaryKeys := make([]string, 0)
	for _, field := range writeFields {
		definition, err := w.columnDefinition(field)
		if err != nil {
			return err
		}
		definitions = append(definitions, definition)
		if len(explicitPrimaryKeys) == 0 && field.PrimaryKey {
			primaryKeys = append(primaryKeys, dialect.QuoteIdentifier(field.Name))
		}
	}
	if len(explicitPrimaryKeys) > 0 {
		for _, key := range explicitPrimaryKeys {
			primaryKeys = append(primaryKeys, dialect.QuoteIdentifier(key))
		}
	}
	if len(primaryKeys) > 0 {
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primaryKeys, ", ")+")")
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci", dialect.QualifiedTable(database, table), strings.Join(definitions, ", "))
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create %s table %s.%s: %w", w.engineType(), database, table, err)
	}
	return w.evolveTableSchema(ctx, db, database, table, writeFields)
}

func (w MySQLCompatibleTableWriter) evolveTableSchema(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo) error {
	columns, err := w.tableColumns(ctx, db, database, table)
	if err != nil {
		return err
	}
	statements, err := w.schemaEvolutionStatements(database, table, fields, columns)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("evolve %s table %s.%s schema: %w", w.engineType(), database, table, err)
		}
	}
	return nil
}

type mysqlCompatibleColumnInfo struct {
	Name              string
	DataType          string
	NativeType        string
	NumericPrecision  sql.NullInt64
	NumericScale      sql.NullInt64
	TemporalPrecision sql.NullInt64
	Nullable          bool
	PrimaryKey        bool
	Comment           string
}

func (w MySQLCompatibleTableWriter) tableColumns(ctx context.Context, db *sql.DB, database, table string) ([]mysqlCompatibleColumnInfo, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type, column_type, numeric_precision, numeric_scale, datetime_precision,
		       (is_nullable = 'YES') AS is_nullable, (column_key = 'PRI') AS primary_key, COALESCE(column_comment, '')
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query %s table columns: %w", w.engineType(), err)
	}
	defer rows.Close()

	columns := make([]mysqlCompatibleColumnInfo, 0)
	for rows.Next() {
		var column mysqlCompatibleColumnInfo
		if err := rows.Scan(
			&column.Name, &column.DataType, &column.NativeType, &column.NumericPrecision, &column.NumericScale,
			&column.TemporalPrecision, &column.Nullable, &column.PrimaryKey, &column.Comment,
		); err != nil {
			return nil, fmt.Errorf("scan %s table column: %w", w.engineType(), err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s table columns: %w", w.engineType(), err)
	}
	return columns, nil
}

func (w MySQLCompatibleTableWriter) schemaEvolutionStatements(database, table string, fields []datatype.FieldInfo, existingColumns []mysqlCompatibleColumnInfo) ([]string, error) {
	existingByName := make(map[string]mysqlCompatibleColumnInfo, len(existingColumns))
	for _, column := range existingColumns {
		existingByName[column.Name] = column
	}

	statements := make([]string, 0)
	for _, field := range mysqlCompatibleWriteFields(fields) {
		expectedType, err := w.sqlTypeForField(field)
		if err != nil {
			return nil, err
		}
		column, exists := existingByName[field.Name]
		if exists {
			if !mysqlCompatibleColumnMatchesField(column, field) {
				return nil, fmt.Errorf("%s target column %q has type %q, expected %q", w.engineType(), field.Name, mysqlCompatibleColumnNativeType(column), expectedType)
			}
			continue
		}
		if field.PrimaryKey {
			return nil, fmt.Errorf("%s schema evolution cannot add primary key column %q to existing table", w.engineType(), field.Name)
		}
		if !field.Nullable && strings.TrimSpace(field.DefaultExpression) == "" {
			return nil, fmt.Errorf("%s schema evolution cannot add non-null column %q without default expression", w.engineType(), field.Name)
		}
		definition, err := w.columnDefinition(field)
		if err != nil {
			return nil, err
		}
		statements = append(statements, "ALTER TABLE "+mysqlCompatibleDialect().QualifiedTable(database, table)+" ADD COLUMN "+definition)
	}
	return statements, nil
}

func mysqlCompatibleWriteFields(fields []datatype.FieldInfo) []datatype.FieldInfo {
	result := make([]datatype.FieldInfo, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		field.Name = name
		result = append(result, field)
	}
	return result
}

func mysqlCompatibleFieldColumns(fields []datatype.FieldInfo) []string {
	writeFields := mysqlCompatibleWriteFields(fields)
	columns := make([]string, len(writeFields))
	for index := range writeFields {
		columns[index] = writeFields[index].Name
	}
	return columns
}

func (w MySQLCompatibleTableWriter) columnDefinition(field datatype.FieldInfo) (string, error) {
	sqlType, err := w.sqlTypeForField(field)
	if err != nil {
		return "", err
	}
	definition := mysqlCompatibleDialect().QuoteIdentifier(field.Name) + " " + sqlType
	if expression := strings.TrimSpace(field.DefaultExpression); expression != "" {
		definition += " DEFAULT " + expression
	}
	if !field.Nullable {
		definition += " NOT NULL"
	}
	return definition, nil
}

func (w MySQLCompatibleTableWriter) sqlTypeForField(field datatype.FieldInfo) (string, error) {
	if datatype.IsSpatialFieldType(field.Type) {
		return "", fmt.Errorf("%s table write does not support spatial field %q", w.engineType(), field.Name)
	}
	fieldType := datatype.ParseFieldType(string(field.Type))
	switch fieldType {
	case datatype.FieldTypeTime:
		return "TIME(6)", nil
	case datatype.FieldTypeTimestamp:
		return "DATETIME(6)", nil
	case datatype.FieldTypeDecimal:
		if err := w.validateDecimalField(field); err != nil {
			return "", err
		}
	}
	mapper := format.GetTypeMapper("mysql")
	if mapper == nil {
		return "TEXT", nil
	}
	nativeType, size, precision := mapper.FromCommon(fieldType)
	if fieldType == datatype.FieldTypeDecimal {
		size = field.Precision
		precision = field.Scale
	}
	return mysqlCompatibleNativeTypeWithSize(nativeType, size, precision), nil
}

func (w MySQLCompatibleTableWriter) validateDecimalField(field datatype.FieldInfo) error {
	if field.Precision <= 0 {
		return fmt.Errorf("%s decimal field %q requires explicit precision and scale", w.engineType(), field.Name)
	}
	if field.Precision > 65 {
		return fmt.Errorf("%s decimal field %q precision %d exceeds maximum 65", w.engineType(), field.Name, field.Precision)
	}
	if field.Scale < 0 || field.Scale > 30 {
		return fmt.Errorf("%s decimal field %q scale %d must be between 0 and 30", w.engineType(), field.Name, field.Scale)
	}
	if field.Scale > field.Precision {
		return fmt.Errorf("%s decimal field %q scale %d exceeds precision %d", w.engineType(), field.Name, field.Scale, field.Precision)
	}
	return nil
}

func mysqlCompatibleColumnMatchesField(column mysqlCompatibleColumnInfo, field datatype.FieldInfo) bool {
	expected := datatype.ParseFieldType(string(field.Type))
	existing := mysqlCompatibleCommonFieldType(column)
	if expected == datatype.FieldTypeUnknown {
		return existing == datatype.FieldTypeString || existing == datatype.FieldTypeUnknown
	}
	if expected == datatype.FieldTypeDecimal && existing == datatype.FieldTypeDecimal {
		return column.NumericPrecision.Valid && column.NumericScale.Valid &&
			int(column.NumericPrecision.Int64) == field.Precision && int(column.NumericScale.Int64) == field.Scale
	}
	if (expected == datatype.FieldTypeTime || expected == datatype.FieldTypeTimestamp) && existing == expected {
		return column.TemporalPrecision.Valid && column.TemporalPrecision.Int64 == 6
	}
	if expected == datatype.FieldTypeUUID {
		return strings.EqualFold(strings.TrimSpace(column.NativeType), "varchar(36)")
	}
	return expected == existing
}

func mysqlCompatibleColumnNativeType(column mysqlCompatibleColumnInfo) string {
	if nativeType := strings.TrimSpace(column.NativeType); nativeType != "" {
		return nativeType
	}
	return strings.TrimSpace(column.DataType)
}

func mysqlCompatibleCommonFieldType(column mysqlCompatibleColumnInfo) datatype.FieldType {
	if strings.EqualFold(strings.TrimSpace(column.NativeType), "tinyint(1)") {
		return datatype.FieldTypeBool
	}
	if mapper := format.GetTypeMapper("mysql"); mapper != nil {
		if fieldType := mapper.ToCommon(mysqlCompatibleColumnNativeType(column)); fieldType != "" && fieldType != datatype.FieldTypeUnknown {
			return fieldType
		}
		if fieldType := mapper.ToCommon(column.DataType); fieldType != "" && fieldType != datatype.FieldTypeUnknown {
			return fieldType
		}
	}
	return datatype.FieldTypeUnknown
}

func mysqlCompatibleNativeTypeWithSize(nativeType string, size, precision int) string {
	nativeType = strings.ToUpper(strings.TrimSpace(nativeType))
	if nativeType == "" {
		return "TEXT"
	}
	switch nativeType {
	case "VARCHAR", "CHAR", "VARBINARY", "BINARY":
		if size > 0 {
			return fmt.Sprintf("%s(%d)", nativeType, size)
		}
	case "DECIMAL", "NUMERIC":
		if size > 0 && precision > 0 {
			return fmt.Sprintf("%s(%d,%d)", nativeType, size, precision)
		}
		if size > 0 {
			return fmt.Sprintf("%s(%d)", nativeType, size)
		}
	case "TINYINT":
		if size > 0 {
			return fmt.Sprintf("%s(%d)", nativeType, size)
		}
	}
	return nativeType
}

func mysqlCompatibleInsertChunkSize(columnCount, requested int) int {
	if requested <= 0 {
		requested = mysqlCompatibleDefaultInsertChunkSize
	}
	if columnCount <= 0 {
		return requested
	}
	maxRowsByParams := mysqlCompatibleMaxBindParams / columnCount
	if requested > maxRowsByParams {
		return maxRowsByParams
	}
	return requested
}

func mysqlCompatibleInsertSQL(database, table string, columns []string, rows []map[string]interface{}) (string, []interface{}) {
	dialect := mysqlCompatibleDialect()
	quotedColumns := make([]string, len(columns))
	for index, column := range columns {
		quotedColumns[index] = dialect.QuoteIdentifier(column)
	}

	valueGroup := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ") + ")"
	valueGroups := make([]string, len(rows))
	args := make([]interface{}, 0, len(rows)*len(columns))
	for rowIndex, row := range rows {
		valueGroups[rowIndex] = valueGroup
		for _, column := range columns {
			args = append(args, row[column])
		}
	}
	statement := "INSERT INTO " + dialect.QualifiedTable(database, table) + " (" + strings.Join(quotedColumns, ", ") + ") VALUES " + strings.Join(valueGroups, ", ")
	return statement, args
}

func mysqlCompatibleDialect() commonquery.Dialect {
	return commonquery.ForDialect(commonquery.DialectMySQL)
}

type mysqlCompatibleTableWriteSession struct {
	db              *sql.DB
	tx              *sql.Tx
	engineType      string
	writerConnector string
	database        string
	table           string
	columns         []string
	chunkSize       int
	batchesWritten  int64
	rowsWritten     int64
	commitMarker    *resume.Marker
	closed          bool
}

func (s *mysqlCompatibleTableWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) error {
	if s.closed {
		return fmt.Errorf("%s table write session is closed", s.engineType)
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	chunkSize := s.chunkSize
	if chunkSize <= 0 {
		chunkSize = mysqlCompatibleInsertChunkSize(len(s.columns), mysqlCompatibleDefaultInsertChunkSize)
	}
	if chunkSize <= 0 {
		return fmt.Errorf("%s table write session has too many columns for insert bind parameters", s.engineType)
	}

	for start := 0; start < len(batch.Rows); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + chunkSize
		if end > len(batch.Rows) {
			end = len(batch.Rows)
		}
		statement, args := mysqlCompatibleInsertSQL(s.database, s.table, s.columns, batch.Rows[start:end])
		if _, err := s.tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("execute %s table write session rows %d-%d: %w", s.engineType, start, end, err)
		}
	}
	s.batchesWritten++
	s.rowsWritten += int64(len(batch.Rows))
	return nil
}

func (s *mysqlCompatibleTableWriteSession) Close(_ context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.tx.Commit(); err != nil {
		_ = s.db.Close()
		return fmt.Errorf("commit %s table write session: %w", s.engineType, err)
	}
	s.commitMarker = s.buildCommitMarker()
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close %s table write session connection: %w", s.engineType, err)
	}
	return nil
}

func (s *mysqlCompatibleTableWriteSession) CommitMarker() *resume.Marker {
	if s == nil {
		return nil
	}
	return s.commitMarker.Clone()
}

func (s *mysqlCompatibleTableWriteSession) Abort(_ context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	var abortErr error
	if s.tx != nil {
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone {
			abortErr = fmt.Errorf("rollback %s table write session: %w", s.engineType, err)
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && abortErr == nil {
			abortErr = fmt.Errorf("close %s table write session connection: %w", s.engineType, err)
		}
	}
	return abortErr
}

func (s *mysqlCompatibleTableWriteSession) buildCommitMarker() *resume.Marker {
	return &resume.Marker{
		Version:      resume.MarkerVersionV1,
		Provider:     s.engineType + ".table_write_session",
		PositionUnit: "session_commit",
		CommitPosition: map[string]interface{}{
			"rows_committed":    s.rowsWritten,
			"batches_committed": s.batchesWritten,
		},
		Fingerprint: map[string]interface{}{
			"target":   strings.Trim(s.database+"/"+s.table, "/"),
			"database": s.database,
			"table":    s.table,
			"columns":  append([]string(nil), s.columns...),
			"method":   s.writerConnector,
		},
	}
}
