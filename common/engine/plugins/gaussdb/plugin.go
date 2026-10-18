package gaussdb

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	_ "github.com/HuaweiCloudDeveloper/gaussdb-go/stdlib"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonquery "github.com/addp/common/query"
	"gorm.io/gorm"
)

// Plugin owns GaussDB identity and only the admitted read-only Provider surface.
// The protocol adapter is composed explicitly; no write Provider is promoted.
type Plugin struct{}

var certifiedVersion = regexp.MustCompile(`^gaussdb \(GaussDB Kernel 507\.0\.T1 build [0-9a-f]+\)(?:$|[ ,])`)

// Reserved schemas observed in the admitted 507.0.T1 kernel. Explicit names
// avoid hiding user schemas merely because they share a vendor prefix.
var systemSchemas = []string{
	"blockchain", "cstore", "db4ai", "dbe_alert", "dbe_application_info",
	"dbe_compression", "dbe_describe", "dbe_file", "dbe_heat_map", "dbe_ilm",
	"dbe_ilm_admin", "dbe_lob", "dbe_match", "dbe_obfuscation_toolkit", "dbe_output",
	"dbe_perf", "dbe_pldebugger", "dbe_pldeveloper", "dbe_profiler", "dbe_random",
	"dbe_raw", "dbe_scheduler", "dbe_session", "dbe_sql", "dbe_sql_util", "dbe_stats",
	"dbe_task", "dbe_utility", "dbe_xml", "dbe_xmldom", "dbe_xmlgen", "dbe_xmlparser",
	"pkg_service", "pkg_util", "prvt_ilm", "resource_manager", "snapshot", "sqladvisor", "sys",
}

var (
	_ plugin.ConnectionProbeProvider    = (*Plugin)(nil)
	_ plugin.EnginePlugin               = (*Plugin)(nil)
	_ plugin.ConnectionPoolPlugin       = (*Plugin)(nil)
	_ plugin.EngineCatalogFactsProvider = (*Plugin)(nil)
	_ plugin.TableReadSessionProvider   = (*Plugin)(nil)
	_ plugin.QueryReadSessionProvider   = (*Plugin)(nil)
	_ plugin.SQLQueryRuntimeProvider    = (*Plugin)(nil)
)

func init()                            { plugin.Register(&Plugin{}) }
func (p *Plugin) Type() string         { return "gaussdb" }
func (p *Plugin) DisplayName() string  { return "GaussDB" }
func (p *Plugin) EngineOrigin() string { return "general" }
func (p *Plugin) protocol() *postgresql.PostgreSQLPlugin {
	return postgresql.NewProtocolCompatiblePlugin(postgresql.ProtocolIdentity{
		EngineType: p.Type(), DisplayName: p.DisplayName(), DriverName: "gaussdb", DefaultPort: p.DefaultPort(), DefaultSSLMode: "disable",
		CatalogFacts:            p.Capabilities().Storage.Facts,
		AdditionalSystemSchemas: append([]string(nil), systemSchemas...),
	})
}
func (p *Plugin) ConnectionSpec() plugin.ConnectionSpec {
	spec := (&postgresql.PostgreSQLPlugin{}).ConnectionSpec()
	for i := range spec.Fields {
		switch spec.Fields[i].Key {
		case "password":
			spec.Fields[i].Required = true
			spec.Fields[i].LabelKey = "storageEngine.password"
		case "sslmode":
			spec.Fields[i].Default = "disable"
		}
	}
	return spec
}
func (p *Plugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *Plugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *Plugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *Plugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }
func (p *Plugin) ValidateConnectionInfo(info plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(info, p.RequiredFields())
}
func (p *Plugin) BuildDSN(info plugin.ConnectionInfo) (string, error) {
	return p.protocol().BuildDSN(info)
}
func (p *Plugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), plugin.EngineCatalogTermSchema, plugin.TabularCapabilityOptions{
		TableReadSession: true, QueryReadSession: true, SupportsParameters: true, SupportsCancel: true, IdentifierQuote: `"`,
	})
}
func (p *Plugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
}
func (p *Plugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (p *Plugin) TestConnection(ctx context.Context, info plugin.ConnectionInfo) error {
	_, err := p.ProbeConnection(ctx, info)
	return err
}

func (p *Plugin) ProbeConnection(ctx context.Context, info plugin.ConnectionInfo) (plugin.ConnectionProbeResult, error) {
	if err := p.ValidateConnectionInfo(info); err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	dsn, err := p.BuildDSN(info)
	if err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	db, err := sql.Open("gaussdb", dsn)
	if err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	defer db.Close()
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var version string
	if err := db.QueryRowContext(probe, "SELECT version()").Scan(&version); err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	if !certifiedVersion.MatchString(version) {
		return plugin.ConnectionProbeResult{}, fmt.Errorf("%w: GaussDB Kernel 507.0.T1 required", commonapi.ErrBadRequest)
	}
	return plugin.ConnectionProbeResult{ServerVersion: version}, nil
}
func (p *Plugin) CreateConnectionPool(info plugin.ConnectionInfo, config *plugin.PoolConfig) (*gorm.DB, error) {
	return p.protocol().CreateConnectionPool(info, config)
}
func (p *Plugin) GORMDialect() string                { return "postgres" }
func (p *Plugin) SQLDialect() string                 { return commonquery.DialectPostgreSQL }
func (p *Plugin) QueryLanguages() []string           { return []string{"sql"} }
func (p *Plugin) SupportsParameterizedQueries() bool { return true }
func (p *Plugin) ControlledReadOnlySQLBoundary() plugin.ControlledReadOnlySQLBoundary {
	return plugin.ControlledReadOnlySQLBoundaryDatabaseTransaction
}
func (p *Plugin) GenerateSampleQuery(ctx context.Context, info plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return p.protocol().GenerateSampleQuery(ctx, info, opts)
}
func (p *Plugin) PrepareQuery(ctx context.Context, info plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	if !req.Options.ReadOnly {
		return nil, commonapi.ErrForbidden
	}
	return p.protocol().PrepareQuery(ctx, info, req)
}
func (p *Plugin) ExecuteSQL(ctx context.Context, info plugin.ConnectionInfo, statement string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	if !opts.ReadOnly {
		return nil, commonapi.ErrForbidden
	}
	return p.protocol().ExecuteSQL(ctx, info, statement, opts)
}
func (p *Plugin) OpenQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	return p.protocol().OpenQueryReadSession(ctx, prepared)
}
func (p *Plugin) ListChildren(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return p.protocol().ListChildren(ctx, info, path, opts)
}
func (p *Plugin) ResolvePath(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return p.protocol().ResolvePath(ctx, info, path)
}
func (p *Plugin) DescribeEngineCatalogFacts(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	// Spatial enrichment remains outside the certified non-spatial surface.
	opts.IncludeSpatialFacts = false
	facts, err := p.protocol().DescribeEngineCatalogFacts(ctx, info, path, opts)
	if err != nil || facts == nil || facts.Table == nil {
		return facts, err
	}
	// Basic primary-key facts are independent of optional constraint enrichment.
	// Field order must not replace native composite-key order.
	dsn, err := p.BuildDSN(info)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("gaussdb", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT a.attname
      FROM (
        SELECT pk.conrelid,pk.conkey,generate_subscripts(pk.conkey,1) AS ord
        FROM pg_catalog.pg_constraint pk
        JOIN pg_catalog.pg_class t ON t.oid=pk.conrelid
        JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace
        WHERE n.nspname=$1 AND t.relname=$2 AND pk.contype='p'
      ) k
      JOIN pg_catalog.pg_attribute a ON a.attrelid=k.conrelid AND a.attnum=k.conkey[k.ord]
      ORDER BY k.ord`, facts.Table.Native["namespace"], facts.Table.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	facts.Table.PrimaryKey = keys
	return facts, nil
}
func (p *Plugin) ReadBatch(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return p.protocol().ReadBatch(ctx, info, path, opts)
}
func (p *Plugin) OpenTableReadSession(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	return p.protocol().OpenTableReadSession(ctx, info, path, opts)
}
