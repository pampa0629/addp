package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonquery "github.com/addp/common/query"
	"github.com/lib/pq"
)

type postgresBoundedWatermarkSession struct {
	db           *sql.DB
	tx           *sql.Tx
	cursorName   string
	fields       []datatype.FieldInfo
	spatialInfo  *datatype.SpatialInfo
	upper        *plugin.WatermarkCursor
	cursorFields []string
	offset       int64
	closed       bool
}

func (p *PostgreSQLPlugin) OpenBoundedWatermarkRead(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BoundedWatermarkReadOptions) (plugin.BoundedWatermarkReadSession, error) {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	cursorFields, err := plugin.NormalizeWatermarkFields(opts.WatermarkField, opts.TieBreakers)
	if err != nil {
		return nil, err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("open postgresql watermark connection: %w", err)
	}
	fail := func(err error) (plugin.BoundedWatermarkReadSession, error) {
		_ = db.Close()
		return nil, err
	}
	columns, err := postgresTableColumns(ctx, db, schema, table)
	if err != nil {
		return fail(err)
	}
	columnByName := make(map[string]postgresColumnInfo, len(columns))
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		columnByName[column.Name] = column
		fields = append(fields, postgresFieldInfoFromColumn(column))
	}
	for _, name := range cursorFields {
		if _, ok := columnByName[name]; !ok {
			return fail(fmt.Errorf("postgresql watermark cursor field %q does not exist", name))
		}
	}
	if err := validatePostgresUniqueTieBreakers(ctx, db, schema, table, opts.TieBreakers); err != nil {
		return fail(err)
	}
	if opts.Start != nil && len(opts.Start.Values) != len(cursorFields) {
		return fail(fmt.Errorf("postgresql watermark start cursor has %d values, want %d", len(opts.Start.Values), len(cursorFields)))
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return fail(fmt.Errorf("begin postgresql watermark snapshot: %w", err))
	}
	failTx := func(err error) (plugin.BoundedWatermarkReadSession, error) {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, err
	}
	dialect := commonquery.ForEngine("postgresql")
	qualified := dialect.QualifiedTable(schema, table)
	quotedCursor := quotePostgresFields(cursorFields)
	nullPredicate := make([]string, 0, len(quotedCursor))
	for _, name := range quotedCursor {
		nullPredicate = append(nullPredicate, name+" IS NULL")
	}
	var nullCount int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified+" WHERE "+strings.Join(nullPredicate, " OR ")).Scan(&nullCount); err != nil {
		return failTx(fmt.Errorf("check postgresql watermark null values: %w", err))
	}
	if nullCount > 0 {
		return failTx(fmt.Errorf("postgresql watermark cursor fields contain %d rows with NULL values", nullCount))
	}

	upperQuery := "SELECT " + strings.Join(quotedCursor, ", ") + " FROM " + qualified + " ORDER BY " + orderPostgresFields(quotedCursor, "DESC") + " LIMIT 1"
	upperValues := make([]interface{}, len(cursorFields))
	upperPointers := make([]interface{}, len(cursorFields))
	for i := range upperValues {
		upperPointers[i] = &upperValues[i]
	}
	upper := (*plugin.WatermarkCursor)(nil)
	if err := tx.QueryRowContext(ctx, upperQuery).Scan(upperPointers...); err != nil {
		if err != sql.ErrNoRows {
			return failTx(fmt.Errorf("freeze postgresql watermark upper bound: %w", err))
		}
	} else {
		upper = &plugin.WatermarkCursor{Values: stringifyPostgresCursor(upperValues)}
	}

	selectFields := postgresSelectExprForFields(fields)
	query := "SELECT " + selectFields + " FROM " + qualified
	args := make([]interface{}, 0, len(cursorFields)*2)
	predicates := make([]string, 0, 2)
	if upper == nil {
		predicates = append(predicates, "FALSE")
	} else {
		if opts.Start != nil {
			predicates = append(predicates, postgresTuplePredicate(cursorFields, columnByName, len(args)+1, ">"))
			for _, value := range opts.Start.Values {
				args = append(args, value)
			}
		}
		predicates = append(predicates, postgresTuplePredicate(cursorFields, columnByName, len(args)+1, "<="))
		for _, value := range upper.Values {
			args = append(args, value)
		}
	}
	query += " WHERE " + strings.Join(predicates, " AND ") + " ORDER BY " + strings.Join(quotedCursor, ", ")
	cursorName := "addp_transfer_watermark_cursor"
	if _, err := tx.ExecContext(ctx, "DECLARE "+cursorName+" NO SCROLL CURSOR FOR "+query, args...); err != nil {
		return failTx(fmt.Errorf("declare postgresql watermark cursor: %w", err))
	}
	return &postgresBoundedWatermarkSession{
		db: db, tx: tx, cursorName: cursorName, fields: fields,
		spatialInfo: postgresSpatialInfoFromFields(fields), upper: upper, cursorFields: cursorFields,
	}, nil
}

func validatePostgresUniqueTieBreakers(ctx context.Context, db *sql.DB, schema, table string, ties []string) error {
	if len(ties) == 0 {
		return fmt.Errorf("postgresql watermark requires tie_breaker fields")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT array_agg(a.attname ORDER BY key_column.ordinality)
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key_column(attnum, ordinality) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key_column.attnum
		WHERE n.nspname = $1 AND t.relname = $2 AND i.indisunique AND i.indpred IS NULL
		GROUP BY i.indexrelid
	`, schema, table)
	if err != nil {
		return fmt.Errorf("query postgresql watermark unique keys: %w", err)
	}
	defer rows.Close()
	want := strings.Join(ties, "\x00")
	for rows.Next() {
		var values []string
		if err := rows.Scan(pq.Array(&values)); err != nil {
			return fmt.Errorf("scan postgresql unique keys: %w", err)
		}
		if strings.Join(values, "\x00") == want {
			return nil
		}
	}
	return fmt.Errorf("postgresql watermark tie_breaker %v must match a non-partial unique or primary key", ties)
}

func quotePostgresFields(fields []string) []string {
	dialect := commonquery.ForEngine("postgresql")
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		result = append(result, dialect.QuoteIdentifier(field))
	}
	return result
}

func orderPostgresFields(fields []string, direction string) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, field+" "+direction)
	}
	return strings.Join(parts, ", ")
}

func postgresTuplePredicate(fields []string, columns map[string]postgresColumnInfo, start int, operator string) string {
	quoted := quotePostgresFields(fields)
	params := make([]string, 0, len(fields))
	for i, name := range fields {
		params = append(params, fmt.Sprintf("CAST($%d AS %s)", start+i, postgresColumnNativeType(columns[name])))
	}
	return "(" + strings.Join(quoted, ", ") + ") " + operator + " (" + strings.Join(params, ", ") + ")"
}

func stringifyPostgresCursor(values []interface{}) []string {
	result := make([]string, len(values))
	for i, value := range values {
		switch typed := value.(type) {
		case time.Time:
			result[i] = typed.UTC().Format(time.RFC3339Nano)
		case []byte:
			result[i] = string(typed)
		default:
			result[i] = fmt.Sprint(typed)
		}
	}
	return result
}

func (s *postgresBoundedWatermarkSession) UpperBound() *plugin.WatermarkCursor {
	if s.upper == nil {
		return nil
	}
	return &plugin.WatermarkCursor{Values: append([]string(nil), s.upper.Values...)}
}

func (s *postgresBoundedWatermarkSession) TableInfo() (*datatype.TableInfo, *datatype.SpatialInfo) {
	return &datatype.TableInfo{Fields: append([]datatype.FieldInfo(nil), s.fields...)}, s.spatialInfo.Clone()
}

func (s *postgresBoundedWatermarkSession) PositionForRow(row map[string]interface{}) (*plugin.WatermarkCursor, error) {
	values := make([]interface{}, 0, len(s.cursorFields))
	for _, field := range s.cursorFields {
		value, ok := row[field]
		if !ok || value == nil {
			return nil, fmt.Errorf("postgresql watermark row is missing cursor field %q", field)
		}
		values = append(values, value)
	}
	return &plugin.WatermarkCursor{Values: stringifyPostgresCursor(values)}, nil
}

func (s *postgresBoundedWatermarkSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("postgresql watermark session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.tx.QueryContext(ctx, fmt.Sprintf("FETCH FORWARD %d FROM %s", limit, s.cursorName))
	if err != nil {
		return nil, fmt.Errorf("fetch postgresql watermark cursor: %w", err)
	}
	defer rows.Close()
	batch, err := scanPostgresRowsToBatch(rows, s.fields, s.spatialInfo, format.GeometryEncodingEWKB, s.offset)
	if err != nil {
		return nil, err
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

func (s *postgresBoundedWatermarkSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	var result error
	if _, err := s.tx.ExecContext(ctx, "CLOSE "+s.cursorName); err != nil {
		result = err
	}
	if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && result == nil {
		result = err
	}
	if err := s.db.Close(); err != nil && result == nil {
		result = err
	}
	return result
}
