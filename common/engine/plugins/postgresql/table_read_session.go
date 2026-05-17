package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
)

func (p *PostgreSQLPlugin) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgresql connection: %w", err)
	}
	query, err := postgresReadSessionQuery(ctx, db, path, opts)
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
		db:         db,
		tx:         tx,
		cursorName: cursorName,
	}, nil
}

func postgresReadSessionQuery(ctx context.Context, db *sql.DB, path plugin.CatalogPath, opts plugin.TableReadSessionOptions) (string, error) {
	query := strings.TrimSpace(opts.Query)
	if query != "" {
		return query, nil
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return "", err
	}
	selectExpr := "*"
	if shouldReadPostgresSpatialAsGeoJSON(opts.Metadata) {
		if expr, err := postgresGeoJSONSelectExpr(ctx, db, schema, table, opts.Metadata); err != nil {
			return "", err
		} else if expr != "" {
			selectExpr = expr
		}
	}
	return sqldialect.ForEngine("postgresql").SelectTableSQL(selectExpr, schema, table, "", "", 0, 0), nil
}

func shouldReadPostgresSpatialAsGeoJSON(metadata map[string]interface{}) bool {
	if metadata == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(metadataString(metadata, "spatial.target_encoding")), "geojson")
}

func postgresGeoJSONSelectExpr(ctx context.Context, db *sql.DB, schema, table string, metadata map[string]interface{}) (string, error) {
	columns, err := postgresTableColumns(ctx, db, schema, table)
	if err != nil {
		return "", err
	}
	geometryField := strings.TrimSpace(metadataString(metadata, "geometry_field"))
	dialect := sqldialect.ForEngine("postgresql")
	exprs := make([]string, 0, len(columns))
	for _, column := range columns {
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
	Name     string
	DataType string
	UDTName  string
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
		SELECT column_name, data_type, udt_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("query postgresql table columns %s.%s: %w", schema, table, err)
	}
	defer rows.Close()

	columns := make([]postgresColumnInfo, 0)
	for rows.Next() {
		var column postgresColumnInfo
		if err := rows.Scan(&column.Name, &column.DataType, &column.UDTName); err != nil {
			return nil, fmt.Errorf("scan postgresql table column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgresql table columns: %w", err)
	}
	return columns, nil
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
	db         *sql.DB
	tx         *sql.Tx
	cursorName string
	closed     bool
	offset     int64
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

	batch, err := scanPostgresRowsToBatch(rows, s.offset)
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

func scanPostgresRowsToBatch(rows *sql.Rows, offset int64) (*plugin.BatchData, error) {
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

	fields := make([]plugin.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, plugin.FieldInfo{Name: column})
	}
	return &plugin.BatchData{
		Rows:   resultRows,
		Fields: fields,
		Offset: offset,
	}, nil
}
