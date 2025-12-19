package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/database/plugin"
	_ "github.com/go-sql-driver/mysql"
)

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

func (p *MySQLPlugin) ConnectionCategory() string {
	return "relational_db"
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

func (p *MySQLPlugin) GenerateCapabilities() string {
	return `{"storage":[{"type":"relational_db","engine":"mysql","supports_query":true}],"compute":[{"type":"sql_query","description":"SQL查询","dev_modes":["sql"]}]}`
}

func (p *MySQLPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *MySQLPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
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
		return "", fmt.Errorf("missing required MySQL connection info (host, user)")
	}

	// MySQL DSN 格式：user:password@tcp(host:port)/database
	// 处理空密码的情况
	if password == "" {
		return fmt.Sprintf("%s@tcp(%s:%d)/%s?parseTime=true&timeout=10s",
			user, host, port, database), nil
	}

	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&timeout=10s",
		user, password, host, port, database), nil
}

func (p *MySQLPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
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

func (p *MySQLPlugin) SupportsTransactions() bool {
	return true
}

func (p *MySQLPlugin) DefaultDialect() string {
	return "mysql"
}
