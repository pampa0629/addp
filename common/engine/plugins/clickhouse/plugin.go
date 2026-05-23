package clickhouse

import (
	"context"
	"fmt"
	"strings"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
)

// ClickHousePlugin ClickHouse 数据库插件
type ClickHousePlugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&ClickHousePlugin{})
}

// Type 返回数据库类型标识
func (p *ClickHousePlugin) Type() string {
	return "clickhouse"
}

// DisplayName 返回显示名称
func (p *ClickHousePlugin) DisplayName() string {
	return "ClickHouse"
}

// EngineOrigin 返回引擎分类
func (p *ClickHousePlugin) EngineOrigin() string {
	return "general"
}

// DefaultPort 返回默认端口
func (p *ClickHousePlugin) DefaultPort() int {
	return 9000 // ClickHouse Native 协议端口
}

// RequiredFields 返回必填字段列表
func (p *ClickHousePlugin) RequiredFields() []string {
	return []string{"host", "user", "database"}
}

// SensitiveFields 返回敏感字段列表
func (p *ClickHousePlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *ClickHousePlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "database", plugin.TabularCapabilityOptions{
		Write:           true,
		BulkWrite:       true,
		SupportsExplain: true,
		WriterConnector: "jdbc",
	})
}

func (p *ClickHousePlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel("database")
}

func (p *ClickHousePlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *ClickHousePlugin) tabularCatalogCallbacks() plugin.TabularCatalogCallbacks {
	return plugin.TabularCatalogCallbacks{
		NamespaceTerm:         "database",
		ListNamespaces:        p.listNamespaces,
		ListTables:            p.listTables,
		ListColumns:           p.listColumns,
		RowCount:              p.getTableRowCount,
		IsSystemNamespaceFunc: p.isSystemSchema,
	}
}

func (p *ClickHousePlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *ClickHousePlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *ClickHousePlugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeTabularItem(ctx, p.tabularCatalogCallbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *ClickHousePlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *ClickHousePlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return "SELECT *\nFROM your_database.your_table\nLIMIT 10", "sql"
}

func (p *ClickHousePlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.ExecuteSQL(ctx, connInfo, req.Query, req.Options)
}

func (p *ClickHousePlugin) SQLDialect() string {
	return p.GetDialect()
}

func (p *ClickHousePlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

func (p *ClickHousePlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return plugin.ReadSQLBatch(ctx, p, connInfo, path, opts)
}

// ValidateConnectionInfo 验证连接信息
func (p *ClickHousePlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildDSN 构建连接字符串
func (p *ClickHousePlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
	return plugin.BuildClickHouseDSN(connInfo, p.DefaultPort(), map[string]string{
		"dial_timeout":       "10s",
		"max_execution_time": "60",
	})
}

// TestConnection 测试数据库连接
func (p *ClickHousePlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}
	return plugin.TestSQLConnection(ctx, "clickhouse", connStr, "SELECT version()")
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool 创建GORM连接池
func (p *ClickHousePlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	return plugin.OpenGORMPool(clickhouse.Open(connStr), poolConfig)
}

// GetDialect 获取数据库方言
func (p *ClickHousePlugin) GetDialect() string {
	return "clickhouse"
}

// === MetadataPlugin 接口实现 ===

// listNamespaces 列出所有 Database。
func (p *ClickHousePlugin) listNamespaces(ctx context.Context, db *gorm.DB) ([]plugin.NamespaceInfo, error) {
	var namespaces []plugin.NamespaceInfo

	// ClickHouse 使用 system.databases 获取 database 列表和表数量统计。
	query := `
		SELECT
			name,
			(SELECT COUNT(*)
			 FROM system.tables
			 WHERE database = d.name) as table_count
		FROM system.databases d
		WHERE name NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA')
		ORDER BY name
	`

	err := db.WithContext(ctx).Raw(query).Scan(&namespaces).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	return namespaces, nil
}

// ListTables 列出指定Database下的所有表
func (p *ClickHousePlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]datatype.TableInfo, error) {
	var tables []datatype.TableInfo

	query := `
		SELECT
			name,
			CASE
				WHEN engine = 'MaterializedView' THEN 'materialized_view'
				WHEN engine = 'View' THEN 'view'
				WHEN engine LIKE '%View%' THEN 'view'
				ELSE 'table'
			END AS kind,
			comment,
			total_rows as row_count,
			total_bytes as size_bytes
		FROM system.tables
		WHERE database = ?
		ORDER BY name
	`

	err := db.WithContext(ctx).Raw(query, schema).Scan(&tables).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	return tables, nil
}

// ListColumns 列出指定表的所有列
func (p *ClickHousePlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]datatype.FieldInfo, error) {
	var fields []datatype.FieldInfo

	query := `
		SELECT
			name,
			type as native_type,
			IF(type LIKE '%Nullable%', 1, 0) as nullable,
			0 as primary_key,
			comment
		FROM system.columns
		WHERE database = ?
		  AND table = ?
		ORDER BY position
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&fields).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	return plugin.NormalizeFieldInfos(fields), nil
}

// GetTableRowCount 获取表的行数
func (p *ClickHousePlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64

	// 使用 system.tables 中的统计数据（快速）
	query := `
		SELECT total_rows
		FROM system.tables
		WHERE database = ?
		  AND name = ?
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	return count, nil
}

// isSystemSchema 判断是否为系统 Database
func (p *ClickHousePlugin) isSystemSchema(schemaName string) bool {
	systemDatabases := map[string]bool{
		"system":             true,
		"information_schema": true,
	}
	return systemDatabases[strings.ToLower(schemaName)]
}
