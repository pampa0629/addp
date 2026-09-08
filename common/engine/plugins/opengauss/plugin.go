package opengauss

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
	"gorm.io/gorm"
)

// Plugin exposes openGauss as an independent ADDP engine type. PostgreSQL
// protocol behavior is reused only for the explicitly forwarded and certified
// providers below; PostgreSQL-only spatial and change-apply providers remain
// outside the openGauss capability surface.
type Plugin struct{}

var (
	_ plugin.BatchReadableProvider                = (*Plugin)(nil)
	_ plugin.BoundedWatermarkReadProvider         = (*Plugin)(nil)
	_ plugin.ConnectionPoolPlugin                 = (*Plugin)(nil)
	_ plugin.ControlledReadOnlySQLProvider        = (*Plugin)(nil)
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

func (p *Plugin) Type() string         { return "opengauss" }
func (p *Plugin) DisplayName() string  { return "openGauss" }
func (p *Plugin) EngineOrigin() string { return "general" }

func (p *Plugin) protocol() *postgresql.PostgreSQLPlugin {
	return postgresql.NewProtocolCompatiblePlugin(postgresql.ProtocolIdentity{
		EngineType:  p.Type(),
		DisplayName: p.DisplayName(),
	})
}

func (p *Plugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "localhost", Placeholder: "localhost"},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Identity: true, Default: 5432, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "database", LabelKey: "storageEngine.database", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "business", PlaceholderKey: "storageEngine.databasePlaceholder"},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Default: "gaussdb", Placeholder: "gaussdb"},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.password", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "sslmode", LabelKey: "storageEngine.sslMode", Input: plugin.ConnectionFieldSelect, Default: "disable", Options: []plugin.ConnectionFieldOption{
			{Value: "disable", LabelKey: "storageEngine.sslDisable"}, {Value: "require", LabelKey: "storageEngine.sslRequire"},
			{Value: "verify-ca", LabelKey: "storageEngine.sslVerifyCa"}, {Value: "verify-full", LabelKey: "storageEngine.sslVerifyFull"},
		}},
	)
}

func (p *Plugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *Plugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *Plugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *Plugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }

func (p *Plugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), plugin.EngineCatalogTermSchema, plugin.TabularCapabilityOptions{
		Constraints:          true,
		TableReadSession:     true,
		QueryReadSession:     true,
		TableWriteSession:    true,
		TableWritePrepare:    true,
		BoundedWatermarkRead: true,
		TableUpsert:          true,
		Delete:               true,
		SupportsExplain:      true,
		SupportsCancel:       true,
		SupportsParameters:   true,
		IdentifierQuote:      `"`,
		WriterConnector:      "postgres_copy",
	})
}

func (p *Plugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
}

func (p *Plugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *Plugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return p.protocol().ListChildren(ctx, connInfo, parent, opts)
}

func (p *Plugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return p.protocol().ResolvePath(ctx, connInfo, path)
}

func (p *Plugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return p.protocol().DescribeEngineCatalogFacts(ctx, connInfo, path, opts)
}

func (p *Plugin) QueryLanguages() []string { return []string{"sql"} }

func (p *Plugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return p.protocol().GenerateSampleQuery(ctx, connInfo, opts)
}

func (p *Plugin) PrepareQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	return p.protocol().PrepareQuery(ctx, connInfo, req)
}

func (p *Plugin) OpenQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	return p.protocol().OpenQueryReadSession(ctx, prepared)
}

func (p *Plugin) SQLDialect() string                  { return commonquery.DialectPostgreSQL }
func (p *Plugin) SupportsParameterizedQueries() bool  { return true }
func (p *Plugin) SupportsControlledReadOnlySQL() bool { return true }

func (p *Plugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *Plugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return p.protocol().ReadBatch(ctx, connInfo, path, opts)
}

func (p *Plugin) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	return p.protocol().OpenTableReadSession(ctx, connInfo, path, opts)
}

func (p *Plugin) OpenBoundedWatermarkRead(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BoundedWatermarkReadOptions) (plugin.BoundedWatermarkReadSession, error) {
	return p.protocol().OpenBoundedWatermarkRead(ctx, connInfo, path, opts)
}

func (p *Plugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteOptions) error {
	return p.protocol().PrepareTableWrite(ctx, connInfo, path, opts)
}

func (p *Plugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	session, err := p.protocol().OpenTableWriteSession(ctx, connInfo, path, opts)
	if err != nil {
		return nil, err
	}
	return &tableWriteSession{TableWriteSession: session}, nil
}

type tableWriteSession struct {
	plugin.TableWriteSession
}

func (s *tableWriteSession) CommitMarker() *resume.Marker {
	provider, ok := s.TableWriteSession.(plugin.CommitMarkerProvider)
	if !ok {
		return nil
	}
	marker := provider.CommitMarker()
	if marker != nil {
		marker.Provider = "opengauss.table_write_session"
	}
	return marker
}

func (p *Plugin) PrepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions) error {
	return p.protocol().PrepareTableUpsert(ctx, connInfo, path, opts)
}

func (p *Plugin) UpsertBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.TableUpsertOptions) error {
	return p.protocol().UpsertBatch(ctx, connInfo, path, batch, opts)
}

func (p *Plugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	return p.protocol().DeleteResource(ctx, connInfo, path)
}

func (p *Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *Plugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.BuildPostgreSQLDSN(connInfo, p.DefaultPort())
}

func (p *Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build openGauss connection string: %w", err)
	}
	return plugin.TestSQLConnection(ctx, "postgres", dsn, "SELECT version()")
}

func (p *Plugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	return p.protocol().CreateConnectionPool(connInfo, poolConfig)
}

func (p *Plugin) GORMDialect() string { return "postgres" }
