package doris

import (
	"context"
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

func (p *DorisPlugin) DefaultPort() int {
	return 9030 // Doris 默认查询端口
}

func (p *DorisPlugin) RequiredFields() []string {
	// Doris 默认 root 用户密码为空，所以 password 不是必填
	return []string{"host", "user", "database"}
}

func (p *DorisPlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *DorisPlugin) ConnectionIdentityFields() []string {
	return []string{"host", "port", "database"}
}

func (p *DorisPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "database", plugin.TabularCapabilityOptions{
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
		WriterConnector:    "doris_insert",
	})
}

func (p *DorisPlugin) CatalogModel() plugin.CatalogModelSpec {
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
		ListColumns:           p.listColumns,
		RowCount:              p.getTableRowCount,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *DorisPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *DorisPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *DorisPlugin) DescribeCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return plugin.DescribeTabularCatalogFacts(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *DorisPlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *DorisPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return plugin.SampleSQLForCatalogPath(p.Type(), opts.Path, 10), "sql"
}

func (p *DorisPlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.ExecuteSQL(ctx, connInfo, req.Query, req.Options)
}

func (p *DorisPlugin) SQLDialect() string {
	return p.GetDialect()
}

func (p *DorisPlugin) SupportsParameterizedQueries() bool {
	return true
}

func (p *DorisPlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	if opts.ReadOnly {
		if err := commonquery.RequireReadOnly(sql); err != nil {
			return nil, fmt.Errorf("Doris 只读查询校验失败：%w", err)
		}
	}
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *DorisPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
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
func (p *DorisPlugin) GetDialect() string {
	return "mysql" // Doris 兼容 MySQL 协议
}

// === CatalogProvider / CatalogFactsProvider 回调实现 ===

// listNamespaces 列出所有 Database。
func (p *DorisPlugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.CatalogPath) ([]plugin.CatalogEntry, error) {
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
