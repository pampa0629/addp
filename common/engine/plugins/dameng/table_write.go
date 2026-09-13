package dameng

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
)

const damengTableWriteSessionMarkerProvider = "dameng.table_write_session"

func (p *Plugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteOptions) error {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	if err := validateWriteFields(opts.Fields, opts.SpatialInfo); err != nil {
		return err
	}
	if err := p.requireCurrentSchema(ctx, connInfo, schema); err != nil {
		return err
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	exists, err := tableExists(ctx, db, table)
	if err != nil {
		return err
	}
	if !exists {
		statement, err := createTableSQL(schema, table, opts.Fields)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create DM8 table: %w", err)
		}
		return nil
	}
	actual, err := p.listColumns(ctx, connInfo, schema, table)
	if err != nil {
		return err
	}
	actualByName := make(map[string]datatype.FieldInfo, len(actual))
	for _, field := range actual {
		actualByName[strings.ToUpper(field.Name)] = field
	}
	for _, expected := range opts.Fields {
		if _, ok := actualByName[strings.ToUpper(expected.Name)]; ok {
			continue
		}
		if !expected.Nullable {
			return fmt.Errorf("DM8 existing table is missing NOT NULL field %q", expected.Name)
		}
		sqlType, err := dmSQLType(expected)
		if err != nil {
			return err
		}
		statement := "ALTER TABLE " + commonquery.ForDialect(commonquery.DialectDameng).QualifiedTable(schema, table) + " ADD " + commonquery.ForDialect(commonquery.DialectDameng).QuoteIdentifier(expected.Name) + " " + sqlType
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("add DM8 table field %q: %w", expected.Name, err)
		}
	}
	return nil
}

func (p *Plugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	if err := p.requireCurrentSchema(ctx, connInfo, schema); err != nil {
		return err
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	exists, err := tableExists(ctx, db, table)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE "+commonquery.ForDialect(p.SQLDialect()).QualifiedTable(schema, table)); err != nil {
		return fmt.Errorf("drop DM8 table: %w", err)
	}
	return nil
}

func (p *Plugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, damengTableWriteSessionMarkerProvider); err != nil {
		return nil, err
	}
	if err := validateWriteFields(opts.Fields, opts.SpatialInfo); err != nil {
		return nil, err
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	columns := fieldNames(opts.Fields)
	statement := insertSQL(schema, table, columns)
	db, err := p.openDB(connInfo)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin DM8 table write session: %w", err)
	}
	if opts.Replace {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+commonquery.ForDialect(p.SQLDialect()).QualifiedTable(schema, table)); err != nil {
			tx.Rollback()
			db.Close()
			return nil, fmt.Errorf("replace DM8 target table: %w", err)
		}
	}
	stmt, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("prepare DM8 table insert: %w", err)
	}
	return &tableWriteSession{db: db, tx: tx, stmt: stmt, schema: schema, table: table, fields: append([]datatype.FieldInfo(nil), opts.Fields...)}, nil
}

type tableWriteSession struct {
	db             *sql.DB
	tx             *sql.Tx
	stmt           *sql.Stmt
	schema         string
	table          string
	fields         []datatype.FieldInfo
	rowsWritten    int64
	batchesWritten int64
	marker         *resume.Marker
	closed         bool
}

func (s *tableWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) error {
	if s.closed {
		return fmt.Errorf("DM8 table write session is closed")
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	for rowIndex, row := range batch.Rows {
		args := make([]interface{}, 0, len(s.fields))
		for _, field := range s.fields {
			value, ok := row[field.Name]
			if !ok {
				return fmt.Errorf("DM8 insert row %d is missing field %q", rowIndex, field.Name)
			}
			if value == nil && !field.Nullable {
				return fmt.Errorf("DM8 insert row %d field %q is NOT NULL", rowIndex, field.Name)
			}
			args = append(args, value)
		}
		if _, err := s.stmt.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("insert DM8 row %d: %w", rowIndex, err)
		}
	}
	s.rowsWritten += int64(len(batch.Rows))
	s.batchesWritten++
	return nil
}

func (s *tableWriteSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.stmt.Close(); err != nil {
		_ = s.abort()
		return fmt.Errorf("close DM8 insert statement: %w", err)
	}
	if err := s.tx.Commit(); err != nil {
		_ = s.db.Close()
		return fmt.Errorf("commit DM8 table write session: %w", err)
	}
	s.marker = &resume.Marker{Version: resume.MarkerVersionV1, Provider: damengTableWriteSessionMarkerProvider, PositionUnit: "session_commit", CommitPosition: map[string]interface{}{"rows_committed": s.rowsWritten, "batches_committed": s.batchesWritten}, Fingerprint: map[string]interface{}{"target": strings.Trim(s.schema+"/"+s.table, "/"), "columns": fieldNames(s.fields), "method": "dm_insert"}}
	return s.db.Close()
}

func (s *tableWriteSession) CommitMarker() *resume.Marker {
	return s.marker.Clone()
}

func (s *tableWriteSession) Abort(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.abort()
}

func (s *tableWriteSession) abort() error {
	var result error
	if s.stmt != nil {
		result = s.stmt.Close()
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

func (p *Plugin) PrepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions) error {
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: opts.Fields, SpatialInfo: opts.SpatialInfo}); err != nil {
		return err
	}
	return p.validateUniqueFields(ctx, connInfo, path, opts.Keys)
}

func (p *Plugin) UpsertBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.TableUpsertOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if err := validateWriteFields(opts.Fields, opts.SpatialInfo); err != nil {
		return err
	}
	if err := p.validateUniqueFields(ctx, connInfo, path, opts.Keys); err != nil {
		return err
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	columns := fieldNames(batch.Fields)
	if len(columns) == 0 {
		columns = sortedRowColumns(batch.Rows)
	}
	fields, err := selectFields(columns, opts.Fields)
	if err != nil {
		return err
	}
	statement, err := mergeSQL(schema, table, fields, opts.Keys)
	if err != nil {
		return err
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin DM8 upsert transaction: %w", err)
	}
	defer tx.Rollback()
	for rowIndex, row := range batch.Rows {
		args := make([]interface{}, 0, len(columns))
		for _, column := range columns {
			value, ok := row[column]
			if !ok {
				return fmt.Errorf("DM8 upsert row %d is missing field %q", rowIndex, column)
			}
			args = append(args, value)
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("execute DM8 MERGE row %d: %w", rowIndex, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit DM8 upsert: %w", err)
	}
	return nil
}

func (p *Plugin) validateUniqueFields(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, keys []string) error {
	if len(keys) == 0 {
		return fmt.Errorf("DM8 upsert requires keys")
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	constraints, err := p.listConstraints(ctx, connInfo, schema, table)
	if err != nil {
		return err
	}
	for _, constraint := range constraints {
		if equalFieldSet(constraint.Fields, keys) {
			return nil
		}
	}
	return fmt.Errorf("DM8 upsert keys %v must match a primary or unique constraint", keys)
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE = 'TABLE' AND OBJECT_NAME = ?", strings.ToUpper(table)).Scan(&count); err != nil {
		return false, fmt.Errorf("query DM8 table existence: %w", err)
	}
	return count == 1, nil
}

func validateWriteFields(fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) error {
	if len(fields) == 0 {
		return fmt.Errorf("DM8 table write requires fields")
	}
	if spatialInfo != nil && spatialInfo.IsSpatial() {
		return fmt.Errorf("DM8 provider does not support spatial fields")
	}
	seen := map[string]bool{}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || seen[strings.ToUpper(name)] {
			return fmt.Errorf("DM8 table write fields must be non-empty and unique")
		}
		seen[strings.ToUpper(name)] = true
		if datatype.IsSpatialFieldType(field.Type) {
			return fmt.Errorf("DM8 provider does not support spatial fields")
		}
		if _, err := dmSQLType(field); err != nil {
			return err
		}
	}
	return nil
}

func createTableSQL(schema, table string, fields []datatype.FieldInfo) (string, error) {
	dialect := commonquery.ForDialect(commonquery.DialectDameng)
	definitions := make([]string, 0, len(fields)+1)
	primaryKeys := make([]string, 0)
	for _, field := range fields {
		sqlType, err := dmSQLType(field)
		if err != nil {
			return "", err
		}
		definition := dialect.QuoteIdentifier(field.Name) + " " + sqlType
		if !field.Nullable {
			definition += " NOT NULL"
		}
		definitions = append(definitions, definition)
		if field.PrimaryKey {
			primaryKeys = append(primaryKeys, dialect.QuoteIdentifier(field.Name))
		}
	}
	if len(primaryKeys) > 0 {
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primaryKeys, ", ")+")")
	}
	return "CREATE TABLE " + dialect.QualifiedTable(schema, table) + " (" + strings.Join(definitions, ", ") + ")", nil
}

func dmSQLType(field datatype.FieldInfo) (string, error) {
	switch field.Type {
	case datatype.FieldTypeString, datatype.FieldTypeUUID:
		size := field.Size
		if size <= 0 {
			size = 255
		}
		return fmt.Sprintf("VARCHAR(%d)", size), nil
	case datatype.FieldTypeBool:
		return "BOOLEAN", nil
	case datatype.FieldTypeBytes:
		return "BLOB", nil
	case datatype.FieldTypeInt:
		return "INTEGER", nil
	case datatype.FieldTypeBigInt:
		return "BIGINT", nil
	case datatype.FieldTypeFloat:
		return "REAL", nil
	case datatype.FieldTypeDouble:
		return "DOUBLE", nil
	case datatype.FieldTypeDecimal:
		if err := plugin.ValidateExplicitDecimalFieldDefinition("dameng", field, damengDecimalMaxPrecision, damengDecimalMaxScale); err != nil {
			return "", err
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", field.Precision, field.Scale), nil
	case datatype.FieldTypeDate:
		return "DATE", nil
	case datatype.FieldTypeTime:
		return "TIME", nil
	case datatype.FieldTypeTimestamp:
		return "TIMESTAMP(6)", nil
	case datatype.FieldTypeJSON:
		return "CLOB", nil
	default:
		return "", fmt.Errorf("DM8 field %q has unsupported type %q", field.Name, field.Type)
	}
}

func fieldNames(fields []datatype.FieldInfo) []string {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if name := strings.TrimSpace(field.Name); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func insertSQL(schema, table string, columns []string) string {
	dialect := commonquery.ForDialect(commonquery.DialectDameng)
	quoted := make([]string, 0, len(columns))
	values := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, dialect.QuoteIdentifier(column))
		values = append(values, "?")
	}
	return "INSERT INTO " + dialect.QualifiedTable(schema, table) + " (" + strings.Join(quoted, ", ") + ") VALUES (" + strings.Join(values, ", ") + ")"
}

func sortedRowColumns(rows []map[string]interface{}) []string {
	seen := map[string]bool{}
	for _, row := range rows {
		for column := range row {
			seen[column] = true
		}
	}
	result := make([]string, 0, len(seen))
	for column := range seen {
		result = append(result, column)
	}
	sort.Strings(result)
	return result
}

func selectFields(columns []string, fields []datatype.FieldInfo) ([]datatype.FieldInfo, error) {
	byName := make(map[string]datatype.FieldInfo, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
	}
	result := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		field, ok := byName[column]
		if !ok {
			return nil, fmt.Errorf("DM8 upsert batch column %q is absent from fields", column)
		}
		result = append(result, field)
	}
	return result, nil
}

func mergeSQL(schema, table string, fields []datatype.FieldInfo, keys []string) (string, error) {
	dialect := commonquery.ForDialect(commonquery.DialectDameng)
	selects := make([]string, 0, len(fields))
	columns := make([]string, 0, len(fields))
	insertValues := make([]string, 0, len(fields))
	keySet := map[string]bool{}
	for _, key := range keys {
		keySet[key] = true
	}
	for _, field := range fields {
		sqlType, err := dmSQLType(field)
		if err != nil {
			return "", err
		}
		quoted := dialect.QuoteIdentifier(field.Name)
		selects = append(selects, "CAST(? AS "+sqlType+") AS "+quoted)
		columns = append(columns, quoted)
		insertValues = append(insertValues, "source."+quoted)
	}
	on := make([]string, 0, len(keys))
	updates := make([]string, 0, len(fields))
	for _, key := range keys {
		quoted := dialect.QuoteIdentifier(key)
		on = append(on, "target."+quoted+" = source."+quoted)
	}
	for _, field := range fields {
		if keySet[field.Name] {
			continue
		}
		quoted := dialect.QuoteIdentifier(field.Name)
		updates = append(updates, "target."+quoted+" = source."+quoted)
	}
	statement := "MERGE INTO " + dialect.QualifiedTable(schema, table) + " target USING (SELECT " + strings.Join(selects, ", ") + " FROM DUAL) source ON (" + strings.Join(on, " AND ") + ") "
	if len(updates) > 0 {
		statement += "WHEN MATCHED THEN UPDATE SET " + strings.Join(updates, ", ") + " "
	}
	statement += "WHEN NOT MATCHED THEN INSERT (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(insertValues, ", ") + ")"
	return statement, nil
}
