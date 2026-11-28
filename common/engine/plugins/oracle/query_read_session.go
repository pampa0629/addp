package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/addp/common/engine/plugin"
	goora "github.com/sijms/go-ora/v2"
)

var _ plugin.GuardedQueryReadSessionProvider = (*OraclePlugin)(nil)

func (p *OraclePlugin) OpenGuardedQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery, authorize plugin.QueryReadAuthorizer) (plugin.GuardedQueryReadSession, error) {
	if authorize == nil {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	expected, err := prepared.ReadSet(ctx)
	if err != nil {
		return nil, err
	}
	binding, err := plugin.ConsumeSQLPreparedReadSession(prepared, p)
	if err != nil {
		return nil, err
	}
	request := binding.Request
	if expected == nil || !request.Options.ReadOnly || request.Options.Limit != 0 || request.Options.Offset != 0 {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	dsn, err := p.BuildDSN(binding.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	var tx *sql.Tx
	var rows *sql.Rows
	opened := false
	defer func() {
		if !opened {
			cancel()
			if rows != nil {
				_ = rows.Close()
			}
			if tx != nil {
				_ = tx.Rollback()
			}
			_ = db.Close()
		}
	}()
	tx, err = plugin.BeginControlledReadOnlySQLTransaction(sessionCtx, db, p.SQLDialect(), p.ControlledReadOnlySQLBoundary(), sql.LevelDefault)
	if err != nil {
		return nil, err
	}
	for _, path := range expected.Paths {
		if err := oracleVerifyReadTable(sessionCtx, tx, path); err != nil {
			return nil, err
		}
	}
	query, err := RewriteSpatialSelectAll(sessionCtx, tx, request.Query)
	if err != nil {
		return nil, err
	}
	var expanded goora.Clob
	_, err = tx.ExecContext(sessionCtx, "BEGIN SYS.DBMS_UTILITY.EXPAND_SQL_TEXT(:1,:2); END;",
		goora.Clob{String: strings.TrimSuffix(strings.TrimSpace(query), ";"), Valid: true}, sql.Out{Dest: &expanded})
	if err != nil {
		return nil, fmt.Errorf("%w: Oracle native source expansion: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	// Expanded tables must be qualified by the native resolver. Exact equality
	// excludes synonym, view and remote redirection from the admitted path.
	actual, err := inspectOracleReadSQL(expanded.String, "", request.EngineID, true)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected.Paths, actual.Paths) {
		return nil, oracleReadUnresolved("Oracle resolved sources differ from the prepared SQL")
	}
	if err := authorize(sessionCtx, actual.Clone()); err != nil {
		return nil, err
	}
	if err := binding.ValidateExecution(sessionCtx, tx); err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(sessionCtx, strings.TrimSuffix(strings.TrimSpace(query), ";"), request.Options.Args...)
	if err != nil {
		return nil, err
	}
	fields, err := oracleFieldsFromColumnTypes(rows)
	if err != nil {
		return nil, err
	}
	opened = true
	return &guardedOracleQuerySession{
		oracleTableReadSession: &oracleTableReadSession{db: db, tx: tx, rows: rows, fields: fields},
		binding:                binding, readSet: actual, cancel: cancel,
	}, nil
}

func oracleVerifyReadTable(ctx context.Context, tx *sql.Tx, path plugin.EngineCatalogPath) error {
	parts := plugin.EngineCatalogPathWithoutRoot(path).Segments
	var count int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM all_tables t
		WHERE t.owner = :1 AND t.table_name = :2 AND t.temporary = 'N' AND t.secondary = 'N'
		AND NOT EXISTS (SELECT 1 FROM all_external_tables e WHERE e.owner = t.owner AND e.table_name = t.table_name)
		AND NOT EXISTS (SELECT 1 FROM all_mviews m WHERE m.owner = t.owner AND m.mview_name = t.table_name)
		AND NOT EXISTS (SELECT 1 FROM all_tab_cols c WHERE c.owner = t.owner AND c.table_name = t.table_name
		                AND c.virtual_column = 'YES' AND c.hidden_column = 'NO')
	`, parts[0].Name, parts[1].Name).Scan(&count)
	if err != nil {
		return fmt.Errorf("%w: verify Oracle local table: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	if count != 1 {
		return oracleReadUnresolved("Oracle source is not a proven local ordinary table")
	}
	return nil
}

type guardedOracleQuerySession struct {
	*oracleTableReadSession
	binding *plugin.SQLPreparedReadSession
	readSet *plugin.QueryReadSet
	cancel  context.CancelFunc
}

func (s *guardedOracleQuerySession) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return s.readSet.Clone(), nil
}

func (s *guardedOracleQuerySession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	batch, err := s.oracleTableReadSession.ReadBatch(ctx, limit)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return batch, s.binding.FinalizeBatch(batch)
}

func (s *guardedOracleQuerySession) Close(ctx context.Context) error {
	defer s.cancel()
	return s.oracleTableReadSession.Close(ctx)
}

var oracleSelectAllPattern = regexp.MustCompile(`(?is)^\s*SELECT\s+\*\s+FROM\s+((?:"[^"]+"|[A-Za-z0-9_$#]+)\s*\.\s*(?:"[^"]+"|[A-Za-z0-9_$#]+))(.*)$`)

// RewriteSpatialSelectAll expands the one unambiguous table-sample form used
// by the query workbench. Oracle's UDT decoder can block on a bare SDO_GEOMETRY;
// projecting that column to GeoJSON keeps the result tabular and inspectable.
func RewriteSpatialSelectAll(ctx context.Context, tx *sql.Tx, query string) (string, error) {
	matches := oracleSelectAllPattern.FindStringSubmatch(query)
	if len(matches) != 3 {
		return query, nil
	}
	suffix := matches[2]
	trimmedSuffix := strings.TrimSpace(suffix)
	if trimmedSuffix != "" && !strings.HasPrefix(strings.ToUpper(trimmedSuffix), "FETCH") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "WHERE") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "ORDER") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "GROUP") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "OFFSET") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "FOR") {
		return query, nil
	}
	parts := strings.Split(matches[1], ".")
	if len(parts) != 2 {
		return query, nil
	}
	identifier := func(part string) string {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, `"`) {
			return strings.Trim(part, `"`)
		}
		return strings.ToUpper(part)
	}
	owner, table := identifier(parts[0]), identifier(parts[1])
	rows, err := tx.QueryContext(ctx, `
		SELECT column_name, data_type
		  FROM all_tab_columns
		 WHERE owner = :1 AND table_name = :2
		 ORDER BY column_id`, owner, table)
	if err != nil {
		return query, err
	}
	defer rows.Close()
	type column struct{ name, dataType string }
	columns := make([]column, 0)
	hasSpatial := false
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.dataType); err != nil {
			return query, err
		}
		columns = append(columns, c)
		if strings.EqualFold(strings.TrimSpace(c.dataType), "SDO_GEOMETRY") {
			hasSpatial = true
		}
	}
	if err := rows.Err(); err != nil {
		return query, err
	}
	if !hasSpatial || len(columns) == 0 {
		return query, nil
	}
	projection := make([]string, 0, len(columns))
	for _, c := range columns {
		quoted := `"` + strings.ReplaceAll(c.name, `"`, `""`) + `"`
		if strings.EqualFold(strings.TrimSpace(c.dataType), "SDO_GEOMETRY") {
			projection = append(projection, `MDSYS.SDO_UTIL.TO_GEOJSON(`+quoted+`) AS `+quoted)
		} else {
			projection = append(projection, quoted)
		}
	}
	return "SELECT " + strings.Join(projection, ", ") + " FROM " + matches[1] + suffix, nil
}
