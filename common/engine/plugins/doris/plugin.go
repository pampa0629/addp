package doris

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	commonquery "github.com/addp/common/query"
	_ "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var dorisCatalogFactsDialect = shared.MySQLCompatibleCatalogFactsDialect{
	SystemSchemas: map[string]bool{
		"information_schema": true,
		"mysql":              true,
		"performance_schema": true,
		"sys":                true,
		"__internal_schema":  true,
	},
	MapFieldType: dorisCatalogFieldType,
}

func dorisCatalogFieldType(nativeType string) datatype.FieldType {
	return dorisCommonFieldType(dorisColumnInfo{NativeType: nativeType})
}

// DorisPlugin Apache Doris 数据库插件
// Doris 兼容 MySQL 协议，使用 MySQL 驱动
type DorisPlugin struct{}

const (
	dorisDecimalMaxPrecision = 38
	dorisDecimalMaxScale     = 38
)

func init() {
	plugin.Register(&DorisPlugin{})
}

func (p *DorisPlugin) Type() string {
	return "doris"
}

func (p *DorisPlugin) DisplayName() string {
	return "Apache Doris"
}

func (p *DorisPlugin) EngineOrigin() string {
	return "general"
}

func (p *DorisPlugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "localhost", Placeholder: "localhost"},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Identity: true, Default: 9030, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "http_port", LabelKey: "storageEngine.httpPort", Input: plugin.ConnectionFieldNumber, Default: 8030, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "database", LabelKey: "storageEngine.database", Input: plugin.ConnectionFieldText, Required: true, Identity: true, PlaceholderKey: "storageEngine.databasePlaceholder"},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Default: "root", Placeholder: "root"},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.passwordOptional", Input: plugin.ConnectionFieldPassword, Sensitive: true, HintKey: "storageEngine.hints.dorisPassword"},
	)
}

func (p *DorisPlugin) DefaultPort() int {
	return p.ConnectionSpec().DefaultPortValue()
}

func (p *DorisPlugin) RequiredFields() []string {
	return p.ConnectionSpec().RequiredFields()
}

func (p *DorisPlugin) SensitiveFields() []string {
	return p.ConnectionSpec().SensitiveFields()
}

func (p *DorisPlugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
}

func (p *DorisPlugin) Capabilities() plugin.EngineCapabilities {
	caps := plugin.NewTabularCapabilities(p.Type(), "database", plugin.TabularCapabilityOptions{
		Constraints:        true,
		Write:              true,
		BulkWrite:          true,
		BatchWrite:         true,
		TableReadSession:   true,
		TableWriteSession:  true,
		TableWritePrepare:  true,
		Delete:             true,
		SupportsExplain:    true,
		SupportsParameters: true,
		IdentifierQuote:    "`",
		WriterConnector:    "doris_insert",
	})
	plugin.ApplyExplicitDecimalTableWriteLimits(&caps, dorisDecimalMaxPrecision, dorisDecimalMaxScale)
	return caps
}

func (p *DorisPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel("database")
}

func (p *DorisPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *DorisPlugin) tabularCatalogCallbacks() plugin.TabularCatalogCallbacks {
	return plugin.TabularCatalogCallbacks{
		NamespaceTerm:         "database",
		ListNamespaces:        p.listNamespaces,
		ListTables:            p.listTables,
		GetTable:              dorisCatalogFactsDialect.GetTable,
		ListColumns:           p.listColumns,
		RowCount:              p.getTableRowCount,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *DorisPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *DorisPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *DorisPlugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return plugin.DescribeTabularCatalogFacts(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *DorisPlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *DorisPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return p.queryProvenance().GenerateSampleQuery(ctx, connInfo, opts.Path, p.SQLDialect(), 10), "sql"
}

func (p *DorisPlugin) PrepareQuery(_ context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	provenance := p.queryProvenance()
	return plugin.PrepareSQLRuntimeQuery(p, connInfo, req, provenance.ResolveReadSet, provenance.ResolveOutputLineage)
}

func (p *DorisPlugin) queryProvenance() shared.MySQLCompatibleQueryProvenance {
	return shared.MySQLCompatibleQueryProvenance{
		EngineName: p.DisplayName(), DefaultPort: p.DefaultPort(), CatalogModel: p.EngineCatalogModel(),
		BuildDSN: p.BuildDSN, IsSystemNamespace: p.isSystemSchema, DescribeFacts: p.DescribeEngineCatalogFacts,
		TableEngine: "Doris",
	}
}

var _ plugin.GuardedQueryReadSessionProvider = (*DorisPlugin)(nil)

func (p *DorisPlugin) OpenGuardedQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery, authorize plugin.QueryReadAuthorizer) (plugin.GuardedQueryReadSession, error) {
	return shared.OpenGuardedMySQLCompatibleStatementReadSession(
		ctx, p, p.BuildDSN, plugin.SQLResultFieldMapper(dorisCatalogFieldType), prepared, authorize, p.resolveSessionSources,
	)
}

func (p *DorisPlugin) resolveSessionSources(ctx context.Context, conn *sql.Conn, request plugin.QueryRequest) (*plugin.QueryReadSet, error) {
	var catalog string
	if err := conn.QueryRowContext(ctx, "SELECT CURRENT_CATALOG()").Scan(&catalog); err != nil {
		return nil, fmt.Errorf("%w: read Doris execution catalog: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	if catalog != "internal" {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	return p.queryProvenance().ResolveSessionReadSet(ctx, conn, request)
}

func (p *DorisPlugin) SQLDialect() string {
	return commonquery.DialectMySQL
}

func (p *DorisPlugin) SupportsParameterizedQueries() bool {
	return true
}

func (p *DorisPlugin) ControlledReadOnlySQLBoundary() plugin.ControlledReadOnlySQLBoundary {
	return plugin.ControlledReadOnlySQLBoundaryValidatedStatement
}

func (p *DorisPlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	if opts.ReadOnly {
		return plugin.ExecuteSingleStatementReadOnlySQLWithConnectionPool(ctx, p, connInfo, sql, opts, plugin.SQLResultFieldMapper(dorisCatalogFieldType))
	}
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts, plugin.SQLResultFieldMapper(dorisCatalogFieldType))
}

func (p *DorisPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return plugin.ReadSQLBatch(ctx, p, connInfo, path, opts)
}

func (p *DorisPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *DorisPlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.BuildMySQLCompatibleDSN(connInfo, p.DefaultPort(), p.DisplayName(), map[string]string{
		"interpolateParams": "true",
		"parseTime":         "true",
		"timeout":           "10s",
	})
}

func (p *DorisPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}
	return plugin.TestSQLConnection(ctx, "mysql", connStr, "SELECT VERSION()")
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool 创建GORM连接池
func (p *DorisPlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	return plugin.OpenGORMPool(mysql.Open(connStr), poolConfig)
}

// GetDialect 获取数据库方言
func (p *DorisPlugin) GORMDialect() string {
	return "mysql" // Doris 兼容 MySQL 协议
}

// === EngineCatalogProvider / EngineCatalogFactsProvider 回调实现 ===

// listNamespaces 列出所有 Database。
func (p *DorisPlugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	return dorisCatalogFactsDialect.ListNamespaces(ctx, db, root, "database")
}

// ListTables 列出指定Schema下的所有表
func (p *DorisPlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]datatype.TableInfo, error) {
	return dorisCatalogFactsDialect.ListTables(ctx, db, schema)
}

// ListColumns 列出指定表的所有列
func (p *DorisPlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]datatype.FieldInfo, error) {
	return dorisCatalogFactsDialect.ListColumns(ctx, db, schema, table)
}

// GetTableRowCount 获取表的行数
func (p *DorisPlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	return dorisCatalogFactsDialect.RowCount(ctx, db, schema, table)
}

// isSystemSchema 判断是否为系统 Schema
// Doris 兼容 MySQL 协议，系统 schema 同 MySQL
func (p *DorisPlugin) isSystemSchema(schemaName string) bool {
	return dorisCatalogFactsDialect.IsSystemSchema(schemaName)
}
