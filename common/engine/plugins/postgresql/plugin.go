package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgreSQLPlugin PostgreSQL 数据库插件
type PostgreSQLPlugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&PostgreSQLPlugin{})
}

// Type 返回数据库类型标识
func (p *PostgreSQLPlugin) Type() string {
	return "postgresql"
}

// DisplayName 返回显示名称
func (p *PostgreSQLPlugin) DisplayName() string {
	return "PostgreSQL"
}

// EngineCategory 返回引擎分类
func (p *PostgreSQLPlugin) EngineCategory() string {
	return "standard"
}

// DefaultPort 返回默认端口
func (p *PostgreSQLPlugin) DefaultPort() int {
	return 5432
}

// RequiredFields 返回必填字段列表
func (p *PostgreSQLPlugin) RequiredFields() []string {
	return []string{"host", "user", "database"}
}

// SensitiveFields 返回敏感字段列表
func (p *PostgreSQLPlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *PostgreSQLPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "schema", plugin.TabularCapabilityOptions{
		Write:           true,
		BulkWrite:       true,
		Transactions:    true,
		SpatialMetadata: true,
		SupportsExplain: true,
		SupportsCancel:  true,
		WriterConnector: "postgres_copy",
	})
}

func (p *PostgreSQLPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel("schema")
}

func (p *PostgreSQLPlugin) tabularMetadataAdapter() plugin.TabularMetadataAdapter {
	return plugin.TabularMetadataAdapter{
		Plugin:        p,
		NamespaceTerm: "schema",
		ListSchemas:   p.listSchemas,
		ListTables:    p.listTables,
		ListColumns:   p.listColumns,
		RowCount:      p.getTableRowCount,
	}
}

func (p *PostgreSQLPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, parent, opts)
}

func (p *PostgreSQLPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path)
}

func (p *PostgreSQLPlugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeTabularItem(ctx, p.tabularMetadataAdapter(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: connInfo}, path, opts)
}

func (p *PostgreSQLPlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *PostgreSQLPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return "SELECT *\nFROM your_schema.your_table\nLIMIT 10", "sql"
}

func (p *PostgreSQLPlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.ExecuteSQL(ctx, connInfo, req.Query, req.Options)
}

func (p *PostgreSQLPlugin) SQLDialect() string {
	return p.GetDialect()
}

func (p *PostgreSQLPlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return plugin.ExecuteSQLWithConnectionPool(ctx, p, connInfo, sql, opts)
}

// ValidateConnectionInfo 验证连接信息
func (p *PostgreSQLPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildConnectionString 构建连接字符串
func (p *PostgreSQLPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
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
	sslMode := plugin.GetString(connInfo, "sslmode")
	if sslMode == "" {
		sslMode = "disable"
	}

	if host == "" || user == "" {
		return "", fmt.Errorf("missing required PostgreSQL connection info (host, user)")
	}

	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, database, sslMode), nil
}

// TestConnection 测试数据库连接
func (p *PostgreSQLPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	// 构建 DSN
	connStr, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 连接数据库
	db, err := sql.Open("postgres", connStr)
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
func (p *PostgreSQLPlugin) SupportsTransactions() bool {
	return true
}

// DefaultDialect 实现 SQLDatabasePlugin 接口
func (p *PostgreSQLPlugin) DefaultDialect() string {
	return "postgres"
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool 创建GORM连接池
func (p *PostgreSQLPlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	// 构建连接字符串
	connStr, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建GORM连接
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		// 禁用自动ping，我们手动控制
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
func (p *PostgreSQLPlugin) GetDialect() string {
	return "postgres"
}

// === MetadataPlugin 接口实现 ===

// ListSchemas 列出所有Schema
func (p *PostgreSQLPlugin) listSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
	var schemas []plugin.SchemaInfo

	query := `
		SELECT
			schema_name as name,
			(SELECT COUNT(*)
			 FROM information_schema.tables
			 WHERE table_schema = s.schema_name
			   AND table_type = 'BASE TABLE') as table_count
		FROM information_schema.schemata s
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY schema_name
	`

	err := db.WithContext(ctx).Raw(query).Scan(&schemas).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}

	return schemas, nil
}

// ListTables 列出指定Schema下的所有表
func (p *PostgreSQLPlugin) listTables(ctx context.Context, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
	var tables []plugin.TableInfo

	query := `
		SELECT
			t.table_schema as schema,
			t.table_name,
			COALESCE(pg_total_relation_size(quote_ident(t.table_schema)||'.'||quote_ident(t.table_name)), 0) as size_bytes,
			GREATEST(
				s.last_autoanalyze,
				s.last_autovacuum,
				s.last_analyze,
				s.last_vacuum
			) as last_modified
		FROM information_schema.tables t
		LEFT JOIN pg_stat_user_tables s
			ON t.table_schema = s.schemaname AND t.table_name = s.relname
		WHERE t.table_schema = $1
		  AND t.table_type = 'BASE TABLE'
		ORDER BY t.table_name
	`

	err := db.WithContext(ctx).Raw(query, schema).Scan(&tables).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	// 获取每个表的行数（使用统计估算，避免慢查询）
	for i := range tables {
		var rowCount sql.NullInt64
		countQuery := `
			SELECT reltuples::bigint
			FROM pg_class
			WHERE oid = $1::regclass
		`
		fullTableName := fmt.Sprintf("%s.%s", schema, tables[i].TableName)
		err := db.WithContext(ctx).Raw(countQuery, fullTableName).Scan(&rowCount).Error
		if err == nil && rowCount.Valid {
			// 如果 reltuples = -1，说明表从未 ANALYZE，主动触发 ANALYZE 更新统计信息
			if rowCount.Int64 == -1 {
				// 执行 ANALYZE（采样分析，快速）
				analyzeQuery := fmt.Sprintf("ANALYZE %s.%s",
					db.Statement.Quote(schema),
					db.Statement.Quote(tables[i].TableName))
				analyzeErr := db.WithContext(ctx).Exec(analyzeQuery).Error
				if analyzeErr == nil {
					// ANALYZE 成功后重新读取 reltuples
					var updatedCount sql.NullInt64
					rereadErr := db.WithContext(ctx).Raw(countQuery, fullTableName).Scan(&updatedCount).Error
					if rereadErr == nil && updatedCount.Valid {
						tables[i].RowCount = updatedCount.Int64
					} else {
						// 读取失败，保持 -1
						tables[i].RowCount = rowCount.Int64
					}
				} else {
					// ANALYZE 失败（可能是权限问题），保持 -1
					tables[i].RowCount = rowCount.Int64
				}
			} else {
				tables[i].RowCount = rowCount.Int64
			}
		}
	}

	return tables, nil
}

// ListColumns 列出指定表的所有列
func (p *PostgreSQLPlugin) listColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ColumnInfo, error) {
	var columns []plugin.ColumnInfo

	query := `
		SELECT
			c.column_name,
			CASE
				WHEN c.data_type = 'USER-DEFINED' THEN c.udt_name
				ELSE c.data_type
			END as data_type,
			CASE WHEN c.is_nullable = 'YES' THEN true ELSE false END as is_nullable,
			CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END as is_primary_key,
			COALESCE(
				col_description(
					(quote_ident(c.table_schema)||'.'||quote_ident(c.table_name))::regclass,
					c.ordinal_position
				),
				''
			) as comment
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT kcu.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
				ON tc.constraint_name = kcu.constraint_name
				AND tc.table_schema = kcu.table_schema
			WHERE tc.table_schema = $1
			  AND tc.table_name = $2
			  AND tc.constraint_type = 'PRIMARY KEY'
		) pk ON c.column_name = pk.column_name
		WHERE c.table_schema = $1
		  AND c.table_name = $2
		ORDER BY c.ordinal_position
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&columns).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	return columns, nil
}

// GetTableRowCount 获取表的行数
func (p *PostgreSQLPlugin) getTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64

	// 使用统计估算（快速）
	query := `
		SELECT reltuples::bigint
		FROM pg_class
		WHERE oid = $1::regclass
	`
	fullTableName := fmt.Sprintf("%s.%s", schema, table)

	err := db.WithContext(ctx).Raw(query, fullTableName).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	return count, nil
}

// IsSystemSchema 判断是否为系统 Schema
func (p *PostgreSQLPlugin) IsSystemSchema(schemaName string) bool {
	systemSchemas := map[string]bool{
		"pg_catalog":         true,
		"information_schema": true,
		"pg_toast":           true,
		"pg_temp_1":          true,
		"pg_toast_temp_1":    true,
	}

	// 检查是否在系统 schema 列表中
	if systemSchemas[schemaName] {
		return true
	}

	// 检查是否以 pg_toast_ 或 pg_temp_ 开头
	if len(schemaName) >= 9 && (schemaName[:9] == "pg_toast_" || schemaName[:8] == "pg_temp_") {
		return true
	}

	return false
}
