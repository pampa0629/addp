package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	oraclemapping "github.com/addp/common/format/mappers/oracle"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
)

func (p *OraclePlugin) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	return p.openTableReadSession(ctx, connInfo, path, opts, 0, 0)
}

func (p *OraclePlugin) readBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	session, err := p.openTableReadSession(ctx, connInfo, path, plugin.TableReadSessionOptions{
		Query: opts.Query,
		Args:  opts.Args,
		Hints: opts.Hints,
	}, opts.Limit, opts.Offset)
	if err != nil {
		return nil, err
	}
	defer session.Close(context.Background())
	return session.ReadBatch(ctx, opts.Limit)
}

func (p *OraclePlugin) openTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableReadSessionOptions, limit int, offset int64) (plugin.TableReadSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, "oracle.table_read_session"); err != nil {
		return nil, err
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build Oracle table read DSN: %w", err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Oracle table read connection: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	query, fields, err := p.oracleReadQuery(ctx, db, path, opts, limit, offset)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("begin Oracle table read transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, fmt.Errorf("set Oracle table read transaction to read only: %w", err)
	}
	rows, err := tx.QueryContext(ctx, query, opts.Args...)
	if err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, fmt.Errorf("query Oracle table read session: %w", err)
	}
	if fields == nil {
		fields, err = oracleFieldsFromColumnTypes(rows)
		if err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			_ = db.Close()
			return nil, err
		}
	}
	return &oracleTableReadSession{db: db, tx: tx, rows: rows, fields: fields, offset: offset}, nil
}

func (p *OraclePlugin) oracleReadQuery(ctx context.Context, db *sql.DB, path plugin.CatalogPath, opts plugin.TableReadSessionOptions, limit int, offset int64) (string, []datatype.FieldInfo, error) {
	query := strings.TrimSpace(opts.Query)
	if query != "" {
		if limit > 0 {
			query = commonquery.ForEngine(p.Type()).PaginateQuerySQL(query, limit, int(offset))
		}
		return query, nil, nil
	}
	segments := plugin.CatalogPathWithoutRoot(path).Segments
	if len(segments) < 2 {
		return "", nil, fmt.Errorf("Oracle table read requires a schema/table path or query")
	}
	schema := segments[len(segments)-2].Name
	table := segments[len(segments)-1].Name
	if p.isSystemSchema(schema) {
		return "", nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported, fmt.Errorf("Oracle system schema %q is not exposed", schema))
	}
	fields, err := p.listColumnsWithSQL(ctx, db, schema, table)
	if err != nil {
		return "", nil, err
	}
	fields, err = oracleSelectedFields(fields, opts.Hints)
	if err != nil {
		return "", nil, err
	}
	for _, field := range fields {
		if field.Type == datatype.FieldTypeUnknown {
			return "", nil, fmt.Errorf("Oracle table %s.%s contains unsupported column %q (%s)", schema, table, field.Name, field.NativeType)
		}
	}
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("Oracle table read requires at least one selected field")
	}
	identifiers := make([]string, 0, len(fields))
	dialect := commonquery.ForEngine(p.Type())
	for _, field := range fields {
		identifiers = append(identifiers, dialect.QuoteIdentifier(field.Name))
	}
	return dialect.SelectTableSQL(strings.Join(identifiers, ", "), schema, table, "", "", limit, int(offset)), fields, nil
}

func (p *OraclePlugin) listColumnsWithSQL(ctx context.Context, db *sql.DB, schema, table string) ([]datatype.FieldInfo, error) {
	var rows []oracleColumnRow
	query := `
		SELECT c.column_name AS name,
		       c.data_type,
		       c.data_type_owner,
		       c.data_length,
		       c.char_length,
		       c.data_precision AS numeric_precision,
		       c.data_scale AS numeric_scale,
		       CASE c.nullable WHEN 'Y' THEN 1 ELSE 0 END AS nullable_flag,
		       CASE WHEN pk.column_name IS NOT NULL THEN 1 ELSE 0 END AS primary_key_flag,
		       cc.comments AS "comment",
		       c.column_id AS ordinal_position,
		       c.data_default AS default_expression,
		       c.virtual_column
		  FROM all_tab_cols c
		  LEFT JOIN (
		        SELECT cols.owner, cols.table_name, cols.column_name
		          FROM all_constraints cons
		          JOIN all_cons_columns cols
		            ON cols.owner = cons.owner
		           AND cols.constraint_name = cons.constraint_name
		           AND cols.table_name = cons.table_name
		         WHERE cons.constraint_type = 'P'
		       ) pk
		    ON pk.owner = c.owner
		   AND pk.table_name = c.table_name
		   AND pk.column_name = c.column_name
		  LEFT JOIN all_col_comments cc
		    ON cc.owner = c.owner
		   AND cc.table_name = c.table_name
		   AND cc.column_name = c.column_name
		 WHERE c.owner = :1
		   AND c.table_name = :2
		   AND c.hidden_column = 'NO'
		 ORDER BY c.column_id
	`
	if err := db.QueryRowContext(ctx, "SELECT 1 FROM DUAL").Scan(new(int)); err != nil {
		return nil, fmt.Errorf("validate Oracle table read connection: %w", err)
	}
	rowsDB, err := db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, fmt.Errorf("list Oracle table columns: %w", err)
	}
	defer rowsDB.Close()
	for rowsDB.Next() {
		var row oracleColumnRow
		if err := rowsDB.Scan(
			&row.Name, &row.DataType, &row.DataTypeOwner, &row.DataLength, &row.CharLength,
			&row.NumericPrecision, &row.NumericScale, &row.NullableFlag, &row.PrimaryKeyFlag,
			&row.Comment, &row.OrdinalPosition, &row.DefaultExpression, &row.VirtualColumn,
		); err != nil {
			return nil, fmt.Errorf("scan Oracle table column: %w", err)
		}
		rows = append(rows, row)
	}
	if err := rowsDB.Err(); err != nil {
		return nil, fmt.Errorf("iterate Oracle table columns: %w", err)
	}
	mapper := &oraclemapping.TypeMapper{}
	fields := make([]datatype.FieldInfo, 0, len(rows))
	for _, row := range rows {
		nativeType := oracleColumnNativeType(row)
		field := datatype.FieldInfo{Name: row.Name, Type: mapper.ToCommon(nativeType), NativeType: nativeType, Nullable: row.NullableFlag == 1, PrimaryKey: row.PrimaryKeyFlag == 1, Comment: row.Comment.String, OrdinalPosition: row.OrdinalPosition, DefaultExpression: strings.TrimSpace(row.DefaultExpression.String), Generated: strings.EqualFold(row.VirtualColumn, "YES")}
		if row.CharLength.Valid {
			field.Size = int(row.CharLength.Int64)
		}
		if row.NumericPrecision.Valid {
			field.Precision = int(row.NumericPrecision.Int64)
		}
		if row.NumericScale.Valid {
			field.Scale = int(row.NumericScale.Int64)
		}
		fields = append(fields, field)
	}
	return plugin.NormalizeFieldInfos(fields), nil
}

func oracleSelectedFields(fields []datatype.FieldInfo, hints map[string]interface{}) ([]datatype.FieldInfo, error) {
	if hints == nil {
		return fields, nil
	}
	selection, ok := hints[format.FieldSelectionOptionKey].(format.FieldSelectionOptions)
	if !ok {
		selectionPtr, ptrOK := hints[format.FieldSelectionOptionKey].(*format.FieldSelectionOptions)
		if !ptrOK || selectionPtr == nil {
			return fields, nil
		}
		selection = *selectionPtr
	}
	selected, err := format.ApplyFieldSelectionToTableInfo(&datatype.TableInfo{Fields: fields}, &selection)
	if err != nil {
		return nil, err
	}
	return selected.Fields, nil
}

type oracleTableReadSession struct {
	db        *sql.DB
	tx        *sql.Tx
	rows      *sql.Rows
	fields    []datatype.FieldInfo
	offset    int64
	columns   []string
	exhausted bool
	closed    bool
}

func (s *oracleTableReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("Oracle table read session is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 1000
	}
	if s.exhausted {
		return &plugin.BatchData{Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset}, nil
	}
	if s.columns == nil {
		var err error
		s.columns, err = s.rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("get Oracle table read columns: %w", err)
		}
	}
	rows := make([]map[string]interface{}, 0, limit)
	for len(rows) < limit && s.rows.Next() {
		values, destinations := oracleScanValues(s.fields, s.columns)
		if err := s.rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan Oracle table row: %w", err)
		}
		row := make(map[string]interface{}, len(s.columns))
		for i, column := range s.columns {
			row[column] = values[i]()
		}
		rows = append(rows, row)
	}
	if err := s.rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Oracle table rows: %w", err)
	}
	if len(rows) < limit {
		s.exhausted = true
	}
	batch := &plugin.BatchData{Rows: rows, Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset}
	s.offset += int64(len(rows))
	return batch, nil
}

func (s *oracleTableReadSession) Close(_ context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.rows.Close(); err != nil {
		_ = s.tx.Rollback()
		_ = s.db.Close()
		return err
	}
	if err := s.tx.Commit(); err != nil {
		_ = s.db.Close()
		return fmt.Errorf("commit Oracle table read session: %w", err)
	}
	return s.db.Close()
}

func oracleFieldsFromColumnTypes(rows *sql.Rows) ([]datatype.FieldInfo, error) {
	columns, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("get Oracle result column types: %w", err)
	}
	mapper := &oraclemapping.TypeMapper{}
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		native := strings.ToUpper(strings.TrimSpace(column.DatabaseTypeName()))
		field := datatype.FieldInfo{Name: column.Name(), Type: mapper.ToCommon(native), NativeType: native, Nullable: true}
		if precision, scale, ok := column.DecimalSize(); ok {
			field.Precision = int(precision)
			field.Scale = int(scale)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func oracleScanValues(fields []datatype.FieldInfo, columns []string) ([]func() interface{}, []interface{}) {
	values := make([]func() interface{}, len(columns))
	destinations := make([]interface{}, len(columns))
	for index, column := range columns {
		field := oracleFieldForColumn(fields, column)
		switch field.Type {
		case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
			destination := new(sql.NullInt64)
			destinations[index] = destination
			values[index] = func() interface{} {
				if !destination.Valid {
					return nil
				}
				return destination.Int64
			}
		case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
			destination := new(sql.NullFloat64)
			destinations[index] = destination
			values[index] = func() interface{} {
				if !destination.Valid {
					return nil
				}
				return destination.Float64
			}
		case datatype.FieldTypeDecimal:
			destination := new(sql.RawBytes)
			destinations[index] = destination
			values[index] = func() interface{} {
				if *destination == nil {
					return nil
				}
				return string(*destination)
			}
		case datatype.FieldTypeBool:
			destination := new(sql.NullBool)
			destinations[index] = destination
			values[index] = func() interface{} {
				if !destination.Valid {
					return nil
				}
				return destination.Bool
			}
		case datatype.FieldTypeString, datatype.FieldTypeJSON:
			destination := new(sql.NullString)
			destinations[index] = destination
			values[index] = func() interface{} {
				if !destination.Valid {
					return nil
				}
				return destination.String
			}
		case datatype.FieldTypeBytes:
			destination := new([]byte)
			destinations[index] = destination
			values[index] = func() interface{} {
				if *destination == nil {
					return nil
				}
				return append([]byte(nil), (*destination)...)
			}
		case datatype.FieldTypeTimestamp, datatype.FieldTypeDate:
			destination := new(sql.NullTime)
			destinations[index] = destination
			values[index] = func() interface{} {
				if !destination.Valid {
					return nil
				}
				return destination.Time
			}
		default:
			destination := new(interface{})
			destinations[index] = destination
			values[index] = func() interface{} {
				return *destination
			}
		}
	}
	return values, destinations
}

func oracleFieldForColumn(fields []datatype.FieldInfo, column string) datatype.FieldInfo {
	for _, field := range fields {
		if strings.EqualFold(field.Name, column) {
			return field
		}
	}
	return datatype.FieldInfo{Type: datatype.FieldTypeUnknown}
}
