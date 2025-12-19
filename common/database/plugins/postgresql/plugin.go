package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/database/plugin"
	_ "github.com/lib/pq"
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
