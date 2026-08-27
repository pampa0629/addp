package mysql

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	"github.com/addp/common/format"
	_ "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var mysqlCatalogFactsDialect = shared.MySQLCompatibleCatalogFactsDialect{
	SystemSchemas: map[string]bool{
		"information_schema": true,
		"mysql":              true,
		"performance_schema": true,
		"sys":                true,
	},
	IncludeComment: true,
	IncludeEngine:  true,
	MapFieldType:   mysqlCatalogFieldType,
}

func mysqlCatalogFieldType(nativeType string) datatype.FieldType {
	return mysqlCommonFieldType(mysqlColumnInfo{NativeType: nativeType})
}

// MySQLPlugin MySQL 数据库插件
type MySQLPlugin struct{}

var (
	_ plugin.BoundedWatermarkReadProvider        = (*MySQLPlugin)(nil)
	_ plugin.SpatialFeatureReadProvider          = (*MySQLPlugin)(nil)
	_ plugin.TableReadSessionProvider            = (*MySQLPlugin)(nil)
	_ plugin.TableUpsertProvider                 = (*MySQLPlugin)(nil)
	_ plugin.PartitionedTableChangeApplyProvider = (*MySQLPlugin)(nil)
)

func init() {
	plugin.Register(&MySQLPlugin{})
}

func (p *MySQLPlugin) Type() string {
	return "mysql"
}

func (p *MySQLPlugin) DisplayName() string {
	return "MySQL"
}

func (p *MySQLPlugin) EngineOrigin() string {
	return "general"
}

func (p *MySQLPlugin) DefaultPort() int {
	return 3306
}

func (p *MySQLPlugin) RequiredFields() []string {
	return []string{"host", "user", "database"}
}

func (p *MySQLPlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *MySQLPlugin) ConnectionIdentityFields() []string {
	return []string{"host", "port", "database"}
}

func (p *MySQLPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "database", plugin.TabularCapabilityOptions{
		Constraints:               true,
		Write:                     true,
		BulkWrite:                 true,
		BatchWrite:                true,
		TableReadSession:          true,
		TableReadSpatialTransform: true,
		TableWriteSession:         true,
		TableWritePrepare:         true,
		BoundedWatermarkRead:      true,
		TableUpsert:               true,
		PartitionedTableChangeApplyOperations: []string{
			plugin.TableChangeOperationUpsert,
			plugin.TableChangeOperationDelete,
			plugin.TableChangeOperationSkip,
		},
		TableSpatialEncoding: &plugin.NativeTableSpatialEncodingCapability{
			GeometryReadEncodings:  []string{string(format.GeometryEncodingEWKB), string(format.GeometryEncodingGeoJSON)},
			GeometryWriteEncodings: []string{string(format.GeometryEncodingEWKB)},
			ReadTransform:          true,
			NativeSpatialFunctions: true,
		},
		Delete:             true,
		SpatialFacts:       true,
		SupportsExplain:    true,
		SupportsCancel:     true,
		SupportsParameters: true,
		WriterConnector:    "mysql_insert",
	})
}

func (p *MySQLPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.TabularCatalogModel("database")
}

func (p *MySQLPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *MySQLPlugin) tabularCatalogCallbacks() plugin.TabularCatalogCallbacks {
	return plugin.TabularCatalogCallbacks{
		NamespaceTerm:         "database",
		ListNamespaces:        p.listNamespaces,
		ListTables:            p.listTables,
		ListColumns:           p.listColumns,
		RowCount:              p.getTableRowCount,
		DescribeSpatial:       p.describeSpatialFacts,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *MySQLPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *MySQLPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *MySQLPlugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return plugin.DescribeTabularCatalogFacts(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *MySQLPlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *MySQLPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return plugin.SampleSQLForEngineCatalogPath(p.Type(), opts.Path, 10), "sql"
}

func (p *MySQLPlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.ExecuteSQL(ctx, connInfo, req.Query, req.Options)
}

func (p *MySQLPlugin) SQLDialect() string {
	return p.GetDialect()
}

func (p *MySQLPlugin) SupportsParameterizedQueries() bool {
	return true
}

func (p *MySQLPlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *MySQLPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return p.readBatch(ctx, connInfo, path, opts)
}

func (p *MySQLPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *MySQLPlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.BuildMySQLCompatibleDSN(connInfo, p.DefaultPort(), p.DisplayName(), map[string]string{
		"parseTime": "true",
		"timeout":   "10s",
		"charset":   "utf8mb4",
		"collation": "utf8mb4_unicode_ci",
	})
}

func (p *MySQLPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}
	return plugin.TestSQLConnection(ctx, "mysql", connStr, "SELECT VERSION()")
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool 创建GORM连接池
func (p *MySQLPlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	return plugin.OpenGORMPool(mysql.Open(connStr), poolConfig)
}

// GetDialect 获取数据库方言
func (p *MySQLPlugin) GetDialect() string {
	return "mysql"
}

// === EngineCatalogProvider / EngineCatalogFactsProvider 回调实现 ===

// listNamespaces 列出所有 Database。
func (p *MySQLPlugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	return mysqlCatalogFactsDialect.ListNamespaces(ctx, db, root, "database")
}

// ListTables 列出指定Schema下的所有表
func (p *MySQLPlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]datatype.TableInfo, error) {
	return mysqlCatalogFactsDialect.ListTables(ctx, db, schema)
}

// ListColumns 列出指定表的所有列
func (p *MySQLPlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]datatype.FieldInfo, error) {
	return mysqlCatalogFactsDialect.ListColumns(ctx, db, schema, table)
}

// GetTableRowCount 获取表的行数
func (p *MySQLPlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	return mysqlCatalogFactsDialect.RowCount(ctx, db, schema, table)
}

// isSystemSchema 判断是否为系统 Schema
func (p *MySQLPlugin) isSystemSchema(schemaName string) bool {
	return mysqlCatalogFactsDialect.IsSystemSchema(schemaName)
}
