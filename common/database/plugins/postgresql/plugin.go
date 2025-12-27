package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/database/plugin"
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

// ConnectionCategory 返回连接类别
func (p *PostgreSQLPlugin) ConnectionCategory() string {
	return "relational_db"
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

// GenerateCapabilities 生成资源能力描述
func (p *PostgreSQLPlugin) GenerateCapabilities() string {
	return `{"storage":[{"type":"relational_db","engine":"postgresql","supports_query":true}],"compute":[{"type":"sql_query","description":"SQL查询","dev_modes":["sql"]}]}`
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
func (p *PostgreSQLPlugin) ListSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
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
func (p *PostgreSQLPlugin) ListTables(ctx context.Context, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
	var tables []plugin.TableInfo

	query := `
		SELECT
			table_schema as schema,
			table_name,
			COALESCE(pg_total_relation_size(quote_ident(table_schema)||'.'||quote_ident(table_name)), 0) as size_bytes
		FROM information_schema.tables
		WHERE table_schema = $1
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name
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
			tables[i].RowCount = rowCount.Int64
		}
	}

	return tables, nil
}

// ListColumns 列出指定表的所有列
func (p *PostgreSQLPlugin) ListColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ColumnInfo, error) {
	var columns []plugin.ColumnInfo

	query := `
		SELECT
			c.column_name,
			c.data_type,
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

	// 填充标准化类型（使用通用映射工具）
	for i := range columns {
		columns[i].StdType = plugin.MapToStandardType(columns[i].DataType)
	}

	return columns, nil
}

// GetTableRowCount 获取表的行数
func (p *PostgreSQLPlugin) GetTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
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
