package mysql

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	_ "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var mysqlMetadataDialect = plugin.MySQLCompatibleMetadataDialect{
	SystemSchemas: map[string]bool{
		"information_schema": true,
		"mysql":              true,
		"performance_schema": true,
		"sys":                true,
	},
	IncludeComment: true,
}

// MySQLPlugin MySQL 数据库插件
type MySQLPlugin struct{}

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

func (p *MySQLPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "database", plugin.TabularCapabilityOptions{
		Write:           true,
		SupportsExplain: true,
		SupportsCancel:  true,
		WriterConnector: "jdbc",
	})
}

func (p *MySQLPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel("database")
}

func (p *MySQLPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *MySQLPlugin) tabularMetadataAdapter() plugin.TabularMetadataAdapter {
	return plugin.TabularMetadataAdapter{
		Plugin:        p,
		NamespaceTerm: "database",
		ListSchemas:   p.listSchemas,
		ListTables:    p.listTables,
		ListColumns:   p.listColumns,
		RowCount:      p.getTableRowCount,
	}
}

func (p *MySQLPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *MySQLPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *MySQLPlugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeTabularItem(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *MySQLPlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *MySQLPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return "SELECT *\nFROM your_database.your_table\nLIMIT 10", "sql"
}

func (p *MySQLPlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.ExecuteSQL(ctx, connInfo, req.Query, req.Options)
}

func (p *MySQLPlugin) SQLDialect() string {
	return p.GetDialect()
}

func (p *MySQLPlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *MySQLPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return plugin.ReadSQLBatch(ctx, p, connInfo, path, opts)
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

func (p *MySQLPlugin) SupportsTransactions() bool {
	return true
}

func (p *MySQLPlugin) DefaultDialect() string {
	return "mysql"
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

// === MetadataPlugin 接口实现 ===

// ListSchemas 列出所有Schema（MySQL中对应Database）
func (p *MySQLPlugin) listSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
	return mysqlMetadataDialect.ListSchemas(ctx, db)
}

// ListTables 列出指定Schema下的所有表
func (p *MySQLPlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
	return mysqlMetadataDialect.ListTables(ctx, db, schema)
}

// ListColumns 列出指定表的所有列
func (p *MySQLPlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ColumnInfo, error) {
	return mysqlMetadataDialect.ListColumns(ctx, db, schema, table)
}

// GetTableRowCount 获取表的行数
func (p *MySQLPlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	return mysqlMetadataDialect.RowCount(ctx, db, schema, table)
}

// IsSystemSchema 判断是否为系统 Schema
func (p *MySQLPlugin) IsSystemSchema(schemaName string) bool {
	return mysqlMetadataDialect.IsSystemSchema(schemaName)
}
