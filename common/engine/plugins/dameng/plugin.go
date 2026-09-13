package dameng

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

const (
	driverName                = "dm"
	damengDecimalMaxPrecision = 38
	damengDecimalMaxScale     = 38
)

type Plugin struct{}

var (
	_ plugin.BatchReadableProvider                = (*Plugin)(nil)
	_ plugin.BoundedWatermarkReadProvider         = (*Plugin)(nil)
	_ plugin.ConnectionIdentityProvider           = (*Plugin)(nil)
	_ plugin.ConnectionSpecProvider               = (*Plugin)(nil)
	_ plugin.ControlledReadOnlySQLProvider        = (*Plugin)(nil)
	_ plugin.DSNProvider                          = (*Plugin)(nil)
	_ plugin.EngineCatalogFactsProvider           = (*Plugin)(nil)
	_ plugin.EngineCatalogModelProvider           = (*Plugin)(nil)
	_ plugin.EngineCatalogProvider                = (*Plugin)(nil)
	_ plugin.ParameterizedSQLQueryRuntimeProvider = (*Plugin)(nil)
	_ plugin.QueryReadSessionProvider             = (*Plugin)(nil)
	_ plugin.ResourceDeleteProvider               = (*Plugin)(nil)
	_ plugin.TableReadSessionProvider             = (*Plugin)(nil)
	_ plugin.TableUpsertProvider                  = (*Plugin)(nil)
	_ plugin.TableWritePreparer                   = (*Plugin)(nil)
	_ plugin.TableWriteSessionProvider            = (*Plugin)(nil)
)

func init() {
	plugin.Register(&Plugin{})
}

func (p *Plugin) Type() string         { return "dameng" }
func (p *Plugin) DisplayName() string  { return "达梦 DM8" }
func (p *Plugin) EngineOrigin() string { return "general" }

func (p *Plugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "localhost", Placeholder: "localhost"},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Identity: true, Default: 5236, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Default: "ADDP_BUSINESS", Placeholder: "ADDP_BUSINESS"},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.password", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true, PlaceholderKey: "storageEngine.passwordPlaceholder"},
	)
}

func (p *Plugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *Plugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *Plugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *Plugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }

func (p *Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	if err := plugin.ValidateRequiredFields(connInfo, p.RequiredFields()); err != nil {
		return err
	}
	for _, field := range []string{"user", "password"} {
		if strings.ContainsAny(plugin.GetString(connInfo, field), "@:/?#") {
			return fmt.Errorf("DM8 connection field %q contains a reserved DSN character", field)
		}
	}
	return nil
}

func (p *Plugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	if err := p.ValidateConnectionInfo(connInfo); err != nil {
		return "", err
	}
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = p.DefaultPort()
	}
	return "dm://" + plugin.GetString(connInfo, "user") + ":" + plugin.GetString(connInfo, "password") + "@" + host + ":" + strconv.Itoa(port), nil
}

func (p *Plugin) Capabilities() plugin.EngineCapabilities {
	caps := plugin.NewTabularCapabilities(p.Type(), plugin.EngineCatalogTermSchema, plugin.TabularCapabilityOptions{
		Constraints:          true,
		TableReadSession:     true,
		QueryReadSession:     true,
		TableWriteSession:    true,
		TableWritePrepare:    true,
		BoundedWatermarkRead: true,
		TableUpsert:          true,
		Delete:               true,
		SupportsCancel:       true,
		SupportsParameters:   true,
		IdentifierQuote:      `"`,
	})
	plugin.ApplyExplicitDecimalTableWriteLimits(&caps, damengDecimalMaxPrecision, damengDecimalMaxScale)
	return caps
}

func (p *Plugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
}

func (p *Plugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *Plugin) SQLDialect() string                 { return commonquery.DialectDameng }
func (p *Plugin) QueryLanguages() []string           { return []string{"sql"} }
func (p *Plugin) SupportsParameterizedQueries() bool { return true }
func (p *Plugin) ControlledReadOnlySQLBoundary() plugin.ControlledReadOnlySQLBoundary {
	return plugin.ControlledReadOnlySQLBoundaryDatabaseTransaction
}

func (p *Plugin) GenerateSampleQuery(_ context.Context, _ plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return plugin.SampleSQLForDialectCatalogPath(p.SQLDialect(), opts.Path, 10), "sql"
}

func (p *Plugin) PrepareQuery(_ context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	return plugin.PrepareSQLRuntimeQuery(p, connInfo, req, nil, nil)
}

func (p *Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	db, err := p.openDB(connInfo)
	if err != nil {
		return err
	}
	defer db.Close()
	var build string
	if err := db.QueryRowContext(ctx, "SELECT BUILD_VERSION FROM V$INSTANCE").Scan(&build); err != nil {
		return fmt.Errorf("query DM8 build version: %w", err)
	}
	if build != "03134284604-20260707-335949-20228" {
		return fmt.Errorf("unexpected DM8 build version %q", build)
	}
	return nil
}

func (p *Plugin) openDB(connInfo plugin.ConnectionInfo) (*sql.DB, error) {
	if !driverLoaded() {
		return nil, fmt.Errorf("dameng official driver is unavailable: run this data-plane operation in the pinned Linux ARM64 Docker boundary with build tag dameng_official")
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build DM8 DSN: %w", err)
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open DM8 connection: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	return db, nil
}

func driverLoaded() bool {
	for _, name := range sql.Drivers() {
		if name == driverName {
			return true
		}
	}
	return false
}

func (p *Plugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, statement string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	bound, args, err := plugin.BindSQLRuntimeParameters(p.SQLDialect(), statement, opts)
	if err != nil {
		return nil, err
	}
	if opts.ReadOnly {
		if err := commonquery.RequireReadOnly(bound); err != nil {
			return nil, fmt.Errorf("read-only SQL validation failed: %w", err)
		}
	}
	if opts.Limit > 0 {
		bound = commonquery.ForDialect(p.SQLDialect()).PaginateQuerySQL(bound, opts.Limit, 0)
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if opts.ReadOnly {
		tx, err := plugin.BeginControlledReadOnlySQLTransaction(ctx, db, p.SQLDialect(), p.ControlledReadOnlySQLBoundary(), sql.LevelDefault)
		if err != nil {
			return nil, fmt.Errorf("begin DM8 read-only transaction: %w", err)
		}
		defer tx.Rollback()
		rows, err := tx.QueryContext(ctx, bound, args...)
		if err != nil {
			return nil, err
		}
		result, err := scanRows(rows)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit DM8 read-only transaction: %w", err)
		}
		return result, nil
	}
	rows, err := db.QueryContext(ctx, bound, args...)
	if err != nil {
		return nil, err
	}
	return scanRows(rows)
}

func scanRows(rows *sql.Rows) (*plugin.QueryResult, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read DM8 query columns: %w", err)
	}
	result := &plugin.QueryResult{Columns: columns, Rows: make([]map[string]interface{}, 0)}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan DM8 query row: %w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			value := values[index]
			if bytes, ok := value.([]byte); ok {
				value = string(bytes)
			}
			row[column] = value
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate DM8 query rows: %w", err)
	}
	return result, nil
}

func (p *Plugin) OpenQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	connInfo, req, err := plugin.ConsumeSQLPreparedQuery(prepared, p)
	if err != nil {
		return nil, err
	}
	if err := commonquery.RequireReadOnly(req.Query); err != nil {
		return nil, fmt.Errorf("read-only SQL validation failed: %w", err)
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return nil, err
	}
	tx, err := plugin.BeginControlledReadOnlySQLTransaction(ctx, db, p.SQLDialect(), p.ControlledReadOnlySQLBoundary(), sql.LevelDefault)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin DM8 query read session: %w", err)
	}
	rows, err := tx.QueryContext(ctx, req.Query, req.Options.Args...)
	if err != nil {
		_ = tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("open DM8 query read session: %w", err)
	}
	return plugin.NewSQLTransactionRowsTableReadSession(db, tx, rows, nil)
}

func (p *Plugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	if len(opts.Args) > 0 && !p.SupportsParameterizedQueries() {
		return nil, fmt.Errorf("engine %s does not support parameterized SQL batch reads", p.Type())
	}
	statement := strings.TrimSpace(opts.Query)
	if statement == "" {
		schema, table, err := tablePathParts(path)
		if err != nil {
			return nil, err
		}
		limit := opts.Limit
		if limit <= 0 {
			limit = 1000
		}
		statement = commonquery.ForDialect(p.SQLDialect()).SelectTableSQL("*", schema, table, "", "", limit, int(opts.Offset))
	} else {
		if err := commonquery.RequireReadOnly(statement); err != nil {
			return nil, fmt.Errorf("read-only SQL validation failed: %w", err)
		}
		if opts.Limit > 0 {
			statement = commonquery.ForDialect(p.SQLDialect()).PaginateQuerySQL(statement, opts.Limit, int(opts.Offset))
		}
	}
	result, err := p.ExecuteSQL(ctx, connInfo, statement, plugin.QueryOptions{ReadOnly: true, Args: opts.Args})
	if err != nil {
		return nil, err
	}
	return plugin.QueryResultToBatchData(result, opts.Offset), nil
}

func (p *Plugin) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	fields, err := p.listColumns(ctx, connInfo, schema, table)
	if err != nil {
		return nil, err
	}
	statement := strings.TrimSpace(opts.Query)
	if statement == "" {
		statement = "SELECT * FROM " + commonquery.ForDialect(p.SQLDialect()).QualifiedTable(schema, table)
	}
	if err := commonquery.RequireReadOnly(statement); err != nil {
		return nil, fmt.Errorf("read-only SQL validation failed: %w", err)
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return nil, err
	}
	tx, err := plugin.BeginControlledReadOnlySQLTransaction(ctx, db, p.SQLDialect(), p.ControlledReadOnlySQLBoundary(), sql.LevelDefault)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin DM8 table read session: %w", err)
	}
	rows, err := tx.QueryContext(ctx, statement, opts.Args...)
	if err != nil {
		_ = tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("open DM8 table read session: %w", err)
	}
	return plugin.NewSQLTransactionRowsTableReadSession(db, tx, rows, fields)
}

func tablePathParts(path plugin.EngineCatalogPath) (string, string, error) {
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) != 2 || segments[0].Term != plugin.EngineCatalogTermSchema || segments[1].Term != plugin.EngineCatalogTermTable {
		return "", "", fmt.Errorf("DM8 provider requires schema/table catalog path")
	}
	schema := strings.TrimSpace(segments[0].Name)
	table := strings.TrimSpace(segments[1].Name)
	if schema == "" || table == "" {
		return "", "", fmt.Errorf("DM8 provider requires non-empty schema and table")
	}
	return schema, table, nil
}

func equalFieldSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for index := range leftCopy {
		if !strings.EqualFold(leftCopy[index], rightCopy[index]) {
			return false
		}
	}
	return true
}
