package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
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

// EngineCategory 返回引擎分类
func (p *ClickHousePlugin) EngineCategory() string {
	return "standard"
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

func (p *ClickHousePlugin) tabularMetadataAdapter() plugin.TabularMetadataAdapter {
	return plugin.TabularMetadataAdapter{
		Plugin:        p,
		NamespaceTerm: "database",
		ListSchemas:   p.listSchemas,
		ListTables:    p.listTables,
		ListColumns:   p.listColumns,
		RowCount:      p.getTableRowCount,
	}
}

func (p *ClickHousePlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *ClickHousePlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *ClickHousePlugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeTabularItem(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
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

// ValidateConnectionInfo 验证连接信息
func (p *ClickHousePlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildConnectionString 构建连接字符串
func (p *ClickHousePlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = p.DefaultPort()
	}

	// 兼容两种字段名：username 和 user
	user := plugin.GetString(connInfo, "user")
	if user == "" {
		user = plugin.GetString(connInfo, "username")
	}

	password := plugin.GetString(connInfo, "password")
	database := plugin.GetString(connInfo, "database")

	if host == "" || user == "" {
		return "", fmt.Errorf("missing required ClickHouse connection info (host, user)")
	}

	// ClickHouse DSN 格式：tcp://host:port?username=xxx&password=xxx&database=xxx
	return plugin.ClickHouseStyleDSN(user, password, host, port, database, map[string]string{
		"dial_timeout":       "10s",
		"max_execution_time": "60",
	}), nil
}

// TestConnection 测试数据库连接
func (p *ClickHousePlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	// 构建 DSN
	connStr, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 连接数据库
	db, err := sql.Open("clickhouse", connStr)
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	// 设置连接超时
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 测试连接
	if err := db.PingContext(testCtx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 执行简单查询验证
	var version string
	err = db.QueryRowContext(testCtx, "SELECT version()").Scan(&version)
	if err != nil {
		return fmt.Errorf("failed to query version: %w", err)
	}

	return nil
}

// SupportsTransactions 实现 SQLDatabasePlugin 接口
// ClickHouse 不支持传统事务，但支持原子性插入
func (p *ClickHousePlugin) SupportsTransactions() bool {
	return false
}

// DefaultDialect 实现 SQLDatabasePlugin 接口
func (p *ClickHousePlugin) DefaultDialect() string {
	return "clickhouse"
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool 创建GORM连接池
func (p *ClickHousePlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	// 构建连接字符串
	connStr, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建GORM连接
	db, err := gorm.Open(clickhouse.Open(connStr), &gorm.Config{
		DisableAutomaticPing: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gorm connection: %w", err)
	}

	// 获取底层的 *sql.DB 并配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// 配置连接池参数
	sqlDB.SetMaxOpenConns(poolConfig.MaxOpenConns)
	sqlDB.SetMaxIdleConns(poolConfig.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)

	return db, nil
}

// GetDialect 获取数据库方言
func (p *ClickHousePlugin) GetDialect() string {
	return "clickhouse"
}

// === MetadataPlugin 接口实现 ===

// ListSchemas 列出所有Database
func (p *ClickHousePlugin) listSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
	var schemas []plugin.SchemaInfo

	// ClickHouse 使用 SHOW DATABASES 命令
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

	err := db.WithContext(ctx).Raw(query).Scan(&schemas).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}

	return schemas, nil
}

// ListTables 列出指定Database下的所有表
func (p *ClickHousePlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
	var tables []plugin.TableInfo

	query := `
		SELECT
			database as schema,
			name as table_name,
			CASE
				WHEN engine LIKE '%View%' THEN 'view'
				ELSE 'table'
			END AS table_kind,
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
func (p *ClickHousePlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ColumnInfo, error) {
	var columns []plugin.ColumnInfo

	query := `
		SELECT
			name as column_name,
			type as data_type,
			IF(type LIKE '%Nullable%', 1, 0) as is_nullable,
			0 as is_primary_key,
			comment
		FROM system.columns
		WHERE database = ?
		  AND table = ?
		ORDER BY position
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&columns).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	return columns, nil
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

// IsSystemSchema 判断是否为系统 Database
func (p *ClickHousePlugin) IsSystemSchema(schemaName string) bool {
	systemDatabases := map[string]bool{
		"system":             true,
		"information_schema": true,
		"INFORMATION_SCHEMA": true,
	}
	return systemDatabases[schemaName]
}
