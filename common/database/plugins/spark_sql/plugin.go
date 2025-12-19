package spark_sql

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/addp/common/database/plugin"
	"github.com/beltran/gohive"
)

// SparkSQLPlugin Spark SQL Thrift Server 插件
type SparkSQLPlugin struct{}

func init() {
	plugin.Register(&SparkSQLPlugin{})
}

func (p *SparkSQLPlugin) Type() string {
	return "spark_sql"
}

func (p *SparkSQLPlugin) DisplayName() string {
	return "Spark SQL"
}

func (p *SparkSQLPlugin) ConnectionCategory() string {
	return "compute_engine"
}

func (p *SparkSQLPlugin) DefaultPort() int {
	return 10000 // Spark SQL Thrift Server 默认端口
}

func (p *SparkSQLPlugin) RequiredFields() []string {
	return []string{"host"}
}

func (p *SparkSQLPlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *SparkSQLPlugin) GenerateCapabilities() string {
	return `{"compute":[{"type":"sql_query","description":"Spark SQL查询","dev_modes":["sql"],"features":["distributed","big_data"]}]}`
}

func (p *SparkSQLPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *SparkSQLPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = p.DefaultPort()
	}

	database := plugin.GetString(connInfo, "database")
	if database == "" {
		database = "default"
	}

	if host == "" {
		return "", fmt.Errorf("missing required Spark SQL connection info: host")
	}

	// 返回格式：host:port:database（用冒号分隔，方便解析）
	return fmt.Sprintf("%s:%d:%s", host, port, database), nil
}

func (p *SparkSQLPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	// 解析连接参数
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = p.DefaultPort()
	}

	database := plugin.GetString(connInfo, "database")
	if database == "" {
		database = "default"
	}

	user := plugin.GetString(connInfo, "user")
	password := plugin.GetString(connInfo, "password")

	if host == "" {
		return fmt.Errorf("missing required field: host")
	}

	// 配置连接
	configuration := gohive.NewConnectConfiguration()
	if user != "" {
		configuration.Username = user
		if password != "" {
			configuration.Password = password
		}
	}

	// 设置超时
	configuration.ConnectTimeout = 10 * time.Second
	configuration.SocketTimeout = 10 * time.Second

	// 连接到 Spark SQL Thrift Server
	connection, err := gohive.Connect(host, port, "NONE", configuration)
	if err != nil {
		return fmt.Errorf("failed to connect to Spark SQL: %w", err)
	}
	defer connection.Close()

	// 创建 cursor
	cursor := connection.Cursor()

	// 切换到指定数据库
	if database != "default" {
		cursor.Exec(ctx, fmt.Sprintf("USE %s", database))
		if cursor.Err != nil {
			return fmt.Errorf("failed to use database '%s': %w", database, cursor.Err)
		}
	}

	// 执行简单查询验证
	cursor.Exec(ctx, "SELECT 1")
	if cursor.Err != nil {
		return fmt.Errorf("failed to execute test query: %w", cursor.Err)
	}

	return nil
}

// ParseConnectionString 解析 Spark SQL 连接字符串
// 格式: "host:port:database"
func ParseConnectionString(connStr string) (host string, port int, database string, err error) {
	// 简单的字符串解析
	var parts []string
	currentPart := ""
	colonCount := 0

	for _, char := range connStr {
		if char == ':' {
			parts = append(parts, currentPart)
			currentPart = ""
			colonCount++
		} else {
			currentPart += string(char)
		}
	}
	// 添加最后一部分
	if currentPart != "" {
		parts = append(parts, currentPart)
	}

	if len(parts) != 3 {
		return "", 0, "", fmt.Errorf("invalid Spark SQL connection string format: %s", connStr)
	}

	host = parts[0]
	database = parts[2]

	port, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid port number: %s", parts[1])
	}

	return host, port, database, nil
}
