package highgo

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/datatype"
	_ "github.com/addp/common/engine/internal/highgopq"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
	"gorm.io/gorm"
)

// Plugin owns HighGo identity and its certified non-spatial Provider surface.
// Reusing PostgreSQL does not expose its extensions, CDC or arbitrary write SQL.
type Plugin struct{}

// Native HighGo reports the same product/version in Chinese or English.
// Anchor the vendor and version boundaries to reject PostgreSQL and 4.50.
var certifiedVersion = regexp.MustCompile(`^(?:瀚高数据库管理系统V|HighGo Database Management System )4\.5(?:\.[0-9]+)*(?:$|[ ,，])`)

var (
	_ plugin.EnginePlugin                = (*Plugin)(nil)
	_ plugin.ConnectionPoolPlugin        = (*Plugin)(nil)
	_ plugin.EngineCatalogFactsProvider  = (*Plugin)(nil)
	_ plugin.TableReadSessionProvider    = (*Plugin)(nil)
	_ plugin.QueryReadSessionProvider    = (*Plugin)(nil)
	_ plugin.SQLQueryRuntimeProvider     = (*Plugin)(nil)
	_ plugin.BatchWritableProvider       = (*Plugin)(nil)
	_ plugin.TableWritePreparer          = (*Plugin)(nil)
	_ plugin.TableWriteSessionProvider   = (*Plugin)(nil)
	_ plugin.TableWriteDefinitionChecker = (*Plugin)(nil)
)

func init()                            { plugin.Register(&Plugin{}) }
func (p *Plugin) Type() string         { return "highgo" }
func (p *Plugin) DisplayName() string  { return "HighGo V4.5" }
func (p *Plugin) EngineOrigin() string { return "general" }
func (p *Plugin) protocol() *postgresql.PostgreSQLPlugin {
	return postgresql.NewProtocolCompatiblePlugin(postgresql.ProtocolIdentity{
		EngineType: p.Type(), DisplayName: p.DisplayName(), DriverName: "highgo", DefaultPort: p.DefaultPort(), DefaultSSLMode: "require",
		AdditionalSystemSchemas: []string{"dbms_alert", "dbms_assert", "dbms_output", "dbms_pipe", "dbms_random", "dbms_utility", "mysql", "oracle", "plunit", "plvchr", "plvdate", "plvlex", "plvstr", "plvsubst", "utl_file", "wmsys", "tiger", "tiger_data", "topology"},
		AdditionalSystemTables:  []string{"hg_t_audit_log", "pg_buffercache", "pg_stat_statements"},
	})
}
func (p *Plugin) ConnectionSpec() plugin.ConnectionSpec {
	spec := (&postgresql.PostgreSQLPlugin{}).ConnectionSpec()
	for i := range spec.Fields {
		switch spec.Fields[i].Key {
		case "port":
			spec.Fields[i].Default = p.DefaultPort()
		case "password":
			spec.Fields[i].Required = true
			spec.Fields[i].LabelKey = "storageEngine.password"
		case "sslmode":
			spec.Fields[i].Default = "require"
		}
	}
	return spec
}
func (p *Plugin) DefaultPort() int                   { return 5866 }
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
		TableReadSession: true, QueryReadSession: true, SupportsParameters: true, IdentifierQuote: `"`,
		BatchWrite: true, TableWritePrepare: true, TableWriteSession: true, WriterConnector: "postgres_copy",
	})
}
func (p *Plugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
}
func (p *Plugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (p *Plugin) TestConnection(ctx context.Context, info plugin.ConnectionInfo) error {
	if err := p.ValidateConnectionInfo(info); err != nil {
		return err
	}
	dsn, err := p.BuildDSN(info)
	if err != nil {
		return err
	}
	db, err := sql.Open("highgo", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var version string
	if err := db.QueryRowContext(probe, "SELECT version()").Scan(&version); err != nil {
		return err
	}
	if !certifiedVersion.MatchString(version) {
		return fmt.Errorf("%w: HighGo V4.5 version required", commonapi.ErrBadRequest)
	}
	return nil
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
	// Facts options request optional enrichment. Meta requests all enrichments;
	// return ordinary fields without invoking capabilities not declared by HighGo.
	opts.IncludeSpatialFacts = false
	opts.IncludeIndexes = false
	opts.IncludeConstraints = false
	opts.IncludePartitioning = false
	return p.protocol().DescribeEngineCatalogFacts(ctx, info, path, opts)
}
func (p *Plugin) ReadBatch(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return p.protocol().ReadBatch(ctx, info, path, opts)
}
func (p *Plugin) OpenTableReadSession(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	return p.protocol().OpenTableReadSession(ctx, info, path, opts)
}

func (p *Plugin) CheckTableWriteDefinition(definition plugin.TableWriteDefinition) plugin.TableWriteDefinitionReport {
	return p.protocol().CheckNonSpatialTableWriteDefinition(definition)
}

func (p *Plugin) validateWrite(fields []datatype.FieldInfo, spatial *datatype.SpatialInfo) error {
	return p.CheckTableWriteDefinition(plugin.TableWriteDefinitionFromOptions(plugin.TableWriteOptions{
		Fields: fields, SpatialInfo: spatial,
	})).UnsupportedError()
}

func (p *Plugin) PrepareTableWrite(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteOptions) error {
	if err := p.validateWrite(opts.Fields, opts.SpatialInfo); err != nil {
		return err
	}
	return p.protocol().PrepareTableWrite(ctx, info, path, opts)
}

func (p *Plugin) WriteBatch(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.BatchWriteOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if err := p.validateWrite(batch.Fields, batch.Spatial); err != nil {
		return err
	}
	return p.protocol().WriteBatch(ctx, info, path, batch, opts)
}

func (p *Plugin) OpenTableWriteSession(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if opts.Replace {
		return nil, commonapi.ErrForbidden
	}
	if err := p.validateWrite(opts.Fields, opts.SpatialInfo); err != nil {
		return nil, err
	}
	session, err := p.protocol().OpenTableWriteSession(ctx, info, path, opts)
	if err != nil {
		return nil, err
	}
	return &tableWriteSession{TableWriteSession: session}, nil
}

// Only the owning engine identity changes; transaction/marker semantics remain
// those of the single shared PostgreSQL COPY implementation.
type tableWriteSession struct {
	plugin.TableWriteSession
}

func (s *tableWriteSession) CommitMarker() *resume.Marker {
	marker := s.TableWriteSession.(plugin.CommitMarkerProvider).CommitMarker()
	if marker != nil {
		marker.Provider = "highgo.table_write_session"
	}
	return marker
}
