package doris

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/database/plugin"
	_ "github.com/go-sql-driver/mysql"
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

	// Doris DSN 格式同 MySQL
	// 处理空密码的情况（Doris 默认 root 用户密码为空）
	if password == "" {
		return fmt.Sprintf("%s@tcp(%s:%d)/%s?parseTime=true&timeout=10s",
			user, host, port, database), nil
	}

	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&timeout=10s",
		user, password, host, port, database), nil
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
