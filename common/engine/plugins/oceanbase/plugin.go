package oceanbase

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/mysql"
	commonquery "github.com/addp/common/query"
	_ "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var oceanBaseCatalogFactsDialect = shared.MySQLCompatibleCatalogFactsDialect{
	SystemSchemas: map[string]bool{
		"information_schema": true,
		"mysql":              true,
		"oceanbase":          true,
	},
	IncludeComment: true,
	MapFieldType:   oceanBaseCatalogFieldType,
}

func oceanBaseCatalogFieldType(nativeType string) datatype.FieldType {
	normalized := strings.TrimSpace(nativeType)
	if strings.EqualFold(normalized, "tinyint(1)") {
		return datatype.FieldTypeBool
	}
	mapper := format.GetTypeMapper("mysql")
	if mapper == nil {
		return datatype.FieldTypeUnknown
	}
	return mapper.ToCommon(normalized)
}

// Plugin exposes OceanBase MySQL mode as its own ADDP engine type while
// reusing only the compatible wire protocol, SQL dialect, and catalog facts.
type Plugin struct{}

var (
	_ plugin.BatchReadableProvider                = (*Plugin)(nil)
	_ plugin.ConnectionPoolPlugin                 = (*Plugin)(nil)
	_ plugin.ControlledReadOnlySQLProvider        = (*Plugin)(nil)
	_ plugin.EngineCatalogFactsProvider           = (*Plugin)(nil)
	_ plugin.EngineCatalogModelProvider           = (*Plugin)(nil)
	_ plugin.EngineCatalogProvider                = (*Plugin)(nil)
	_ plugin.ParameterizedSQLQueryRuntimeProvider = (*Plugin)(nil)
	_ plugin.ResourceDeleteProvider               = (*Plugin)(nil)
	_ plugin.TableUpsertProvider                  = (*Plugin)(nil)
	_ plugin.TableWritePreparer                   = (*Plugin)(nil)
	_ plugin.TableWriteSessionProvider            = (*Plugin)(nil)
)

func init() {
	plugin.Register(&Plugin{})
}

func (p *Plugin) Type() string         { return "oceanbase" }
func (p *Plugin) DisplayName() string  { return "OceanBase" }
func (p *Plugin) EngineOrigin() string { return "general" }

func (p *Plugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "localhost", Placeholder: "localhost"},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Identity: true, Default: 2881, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "database", LabelKey: "storageEngine.database", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "business", PlaceholderKey: "storageEngine.databasePlaceholder"},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "root@test", Placeholder: "root@test"},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.password", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
	)
}

func (p *Plugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *Plugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *Plugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *Plugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }

func (p *Plugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), plugin.EngineCatalogTermDatabase, plugin.TabularCapabilityOptions{
		Constraints:        true,
		Delete:             true,
		TableUpsert:        true,
		TableWritePrepare:  true,
		TableWriteSession:  true,
		SupportsExplain:    true,
		SupportsParameters: true,
		IdentifierQuote:    "`",
	})
}

func (p *Plugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.EngineCatalogTermDatabase)
}

func (p *Plugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *Plugin) tabularCatalogCallbacks() plugin.TabularCatalogCallbacks {
	return plugin.TabularCatalogCallbacks{
		NamespaceTerm:         plugin.EngineCatalogTermDatabase,
		ListNamespaces:        p.listNamespaces,
		ListTables:            p.listTables,
		ListColumns:           p.listColumns,
		RowCount:              p.getTableRowCount,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *Plugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *Plugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *Plugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return plugin.DescribeTabularCatalogFacts(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *Plugin) QueryLanguages() []string { return []string{"sql"} }

func (p *Plugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return p.queryProvenance().GenerateSampleQuery(ctx, connInfo, opts.Path, p.SQLDialect(), 10), "sql"
}

func (p *Plugin) PrepareQuery(_ context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	provenance := p.queryProvenance()
	return plugin.PrepareSQLRuntimeQuery(p, connInfo, req, provenance.ResolveReadSet, provenance.ResolveOutputLineage)
}

func (p *Plugin) queryProvenance() shared.MySQLCompatibleQueryProvenance {
	return shared.MySQLCompatibleQueryProvenance{
		EngineName:        p.DisplayName(),
		DefaultPort:       p.DefaultPort(),
		CatalogModel:      p.EngineCatalogModel(),
		BuildDSN:          p.BuildDSN,
		IsSystemNamespace: p.isSystemSchema,
		DescribeFacts:     p.DescribeEngineCatalogFacts,
	}
}

func (p *Plugin) SQLDialect() string                  { return commonquery.DialectMySQL }
func (p *Plugin) SupportsParameterizedQueries() bool  { return true }
func (p *Plugin) SupportsControlledReadOnlySQL() bool { return true }
func (p *Plugin) GORMDialect() string                 { return "mysql" }

func (p *Plugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *Plugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return plugin.ReadSQLBatch(ctx, p, connInfo, path, opts)
}

func (p *Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *Plugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.BuildMySQLCompatibleDSN(connInfo, p.DefaultPort(), p.DisplayName(), map[string]string{
		"charset":           "utf8mb4",
		"interpolateParams": "true",
		"parseTime":         "true",
		"timeout":           "10s",
	})
}

func (p *Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}
	return plugin.TestSQLConnection(ctx, "mysql", dsn, "SELECT VERSION()")
}

func (p *Plugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}
	return plugin.OpenGORMPool(gormmysql.Open(dsn), poolConfig)
}

func (p *Plugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	return oceanBaseCatalogFactsDialect.ListNamespaces(ctx, db, root, plugin.EngineCatalogTermDatabase)
}

func (p *Plugin) listTables(ctx context.Context, db *gorm.DB, database string) ([]datatype.TableInfo, error) {
	return oceanBaseCatalogFactsDialect.ListTables(ctx, db, database)
}

func (p *Plugin) listColumns(ctx context.Context, db *gorm.DB, database, table string) ([]datatype.FieldInfo, error) {
	return oceanBaseCatalogFactsDialect.ListColumns(ctx, db, database, table)
}

func (p *Plugin) getTableRowCount(ctx context.Context, db *gorm.DB, database, table string) (int64, error) {
	return oceanBaseCatalogFactsDialect.RowCount(ctx, db, database, table)
}

func (p *Plugin) isSystemSchema(schemaName string) bool {
	return oceanBaseCatalogFactsDialect.IsSystemSchema(schemaName)
}
