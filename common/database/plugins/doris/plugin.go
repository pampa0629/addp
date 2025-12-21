package doris

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/database/plugin"
	_ "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

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

func (p *DorisPlugin) ConnectionCategory() string {
	return "relational_db"
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

func (p *DorisPlugin) GenerateCapabilities() string {
	return `{"storage":[{"type":"relational_db","engine":"doris","supports_query":true}],"compute":[{"type":"sql_query","description":"OLAP分析查询","dev_modes":["sql"]}]}`
}

func (p *DorisPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *DorisPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
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
		return "", fmt.Errorf("missing required Doris connection info (host, user)")
	}

	// DEBUG: 打印密码信息
	fmt.Printf("[DEBUG] Doris BuildConnectionString - password: '%s', length: %d\n", password, len(password))

	// Doris DSN 格式同 MySQL
	// 处理空密码的情况（Doris 默认 root 用户密码为空）
	if password == "" {
		dsn := fmt.Sprintf("%s@tcp(%s:%d)/%s?parseTime=true&timeout=10s",
			user, host, port, database)
		fmt.Printf("[DEBUG] Doris DSN (empty password): %s\n", dsn)
		return dsn, nil
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&timeout=10s",
		user, password, host, port, database)
	fmt.Printf("[DEBUG] Doris DSN (with password): %s:***@tcp(%s:%d)/%s\n", user, host, port, database)
	return dsn, nil
}

func (p *DorisPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	connStr, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	// 设置连接参数
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(10 * time.Second)

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := db.PingContext(testCtx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 执行简单查询验证
	var version string
	err = db.QueryRowContext(testCtx, "SELECT VERSION()").Scan(&version)
	if err != nil {
		return fmt.Errorf("failed to query version: %w", err)
	}

	return nil
}

func (p *DorisPlugin) SupportsTransactions() bool {
	return true
}

func (p *DorisPlugin) DefaultDialect() string {
	return "mysql" // Doris 兼容 MySQL 协议
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool 创建GORM连接池
func (p *DorisPlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	// 构建连接字符串
	connStr, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 创建GORM连接（使用MySQL驱动，因为Doris兼容MySQL协议）
	db, err := gorm.Open(mysql.Open(connStr), &gorm.Config{
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
func (p *DorisPlugin) GetDialect() string {
	return "mysql" // Doris 兼容 MySQL 协议
}

// === MetadataPlugin 接口实现 ===

// ListSchemas 列出所有Schema（Doris中对应Database）
func (p *DorisPlugin) ListSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
	var schemas []plugin.SchemaInfo

	// Doris使用与MySQL相同的information_schema
	query := `
		SELECT
			schema_name as name,
			(SELECT COUNT(*)
			 FROM information_schema.tables
			 WHERE table_schema = s.schema_name
			   AND table_type = 'BASE TABLE') as table_count
		FROM information_schema.schemata s
		WHERE schema_name NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys', '__internal_schema')
		ORDER BY schema_name
	`

	err := db.WithContext(ctx).Raw(query).Scan(&schemas).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}

	return schemas, nil
}

// ListTables 列出指定Schema下的所有表
func (p *DorisPlugin) ListTables(ctx context.Context, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
	var tables []plugin.TableInfo

	// Doris的information_schema与MySQL基本兼容
	query := `
		SELECT
			table_schema as schema,
			table_name,
			COALESCE(table_rows, 0) as row_count,
			COALESCE(data_length + index_length, 0) as size_bytes
		FROM information_schema.tables
		WHERE table_schema = ?
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`

	err := db.WithContext(ctx).Raw(query, schema).Scan(&tables).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	return tables, nil
}

// ListColumns 列出指定表的所有列
func (p *DorisPlugin) ListColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ColumnInfo, error) {
	var columns []plugin.ColumnInfo

	// Doris的列信息查询与MySQL兼容
	query := `
		SELECT
			column_name,
			data_type,
			IF(is_nullable = 'YES', true, false) as is_nullable,
			IF(column_key = 'PRI', true, false) as is_primary_key,
			COALESCE(column_comment, '') as comment
		FROM information_schema.columns
		WHERE table_schema = ?
		  AND table_name = ?
		ORDER BY ordinal_position
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&columns).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}

	// 填充标准化类型
	for i := range columns {
		columns[i].StdType = mapDorisType(columns[i].DataType)
	}

	return columns, nil
}

// GetTableRowCount 获取表的行数
func (p *DorisPlugin) GetTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64

	// 使用information_schema中的统计数据（快速但可能不精确）
	query := `
		SELECT COALESCE(table_rows, 0)
		FROM information_schema.tables
		WHERE table_schema = ?
		  AND table_name = ?
	`

	err := db.WithContext(ctx).Raw(query, schema, table).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	return count, nil
}

// mapDorisType 将Doris原生类型映射为标准类型
// Doris支持MySQL的所有类型，并额外支持一些特殊类型
func mapDorisType(dorisType string) string {
	switch dorisType {
	case "tinyint", "smallint", "int", "integer", "bigint", "largeint":
		return "integer"
	case "float", "double", "decimal", "decimalv3":
		return "number"
	case "char", "varchar", "string", "text":
		return "string"
	case "boolean":
		return "boolean"
	case "date", "datetime", "datev2", "datetimev2", "timestamp":
		return "datetime"
	case "json", "jsonb":
		return "json"
	case "array":
		return "array"
	case "map", "struct":
		return "json"
	case "hll", "bitmap":
		return "binary"
	default:
		return "string"
	}
}
