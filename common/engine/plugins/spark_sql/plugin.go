package spark_sql

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
	"github.com/addp/common/sqleffect"
	"github.com/beltran/gohive"
	"gorm.io/gorm"
)

// SparkSQLPlugin Apache Spark Thrift Server 插件
type SparkSQLPlugin struct {
	query sparkQueryFunc
}

func init() {
	plugin.Register(&SparkSQLPlugin{})
}

func (p *SparkSQLPlugin) Type() string {
	return "spark"
}

func (p *SparkSQLPlugin) DisplayName() string {
	return "Apache Spark"
}

func (p *SparkSQLPlugin) EngineOrigin() string {
	return "general"
}

func (p *SparkSQLPlugin) DefaultPort() int {
	return 10000 // Apache Spark Thrift Server 默认端口
}

func (p *SparkSQLPlugin) RequiredFields() []string {
	return []string{"host"}
}

func (p *SparkSQLPlugin) SensitiveFields() []string {
	return []string{"password"}
}

func (p *SparkSQLPlugin) ConnectionIdentityFields() []string {
	return []string{"host", "port", "database"}
}

func (p *SparkSQLPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities(p.Type(), "database", plugin.TabularCapabilityOptions{
		Write:            false,
		TableReadSession: true,
		SupportsExplain:  true,
	})
}

func (p *SparkSQLPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel("database")
}

func (p *SparkSQLPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *SparkSQLPlugin) QueryLanguages() []string {
	return []string{"sql"}
}

func (p *SparkSQLPlugin) GenerateSampleQuery(ctx context.Context, connInfo plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return plugin.SampleSQLForCatalogPath(p.Type(), opts.Path, 10), "sql"
}

func (p *SparkSQLPlugin) ExecuteRuntimeQuery(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return p.ExecuteSQL(ctx, connInfo, req.Query, req.Options)
}

func (p *SparkSQLPlugin) SQLDialect() string {
	return "spark_sql"
}

func (p *SparkSQLPlugin) ExecuteSQL(ctx context.Context, connInfo plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	if opts.ReadOnly {
		if err := sqleffect.RequireReadOnly(sql); err != nil {
			return nil, fmt.Errorf("Spark 只读查询校验失败：%w", err)
		}
	}
	if opts.Limit > 0 {
		sql = sqldialect.PaginateQuerySQL(sql, opts.Limit, 0)
	}
	return p.runQuery(ctx, connInfo, sql)
}

func (p *SparkSQLPlugin) ReadBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	return plugin.ReadSQLBatch(ctx, p, connInfo, path, opts)
}

func (p *SparkSQLPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *SparkSQLPlugin) BuildDSN(connInfo plugin.ConnectionInfo) (string, error) {
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
		return "", fmt.Errorf("missing required Apache Spark connection info: host")
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

	// 设置超时 - 增加到 30 秒
	configuration.ConnectTimeout = 30 * time.Second
	configuration.SocketTimeout = 30 * time.Second

	// 连接到 Apache Spark Thrift Server
	connection, err := gohive.Connect(host, port, "NONE", configuration)
	if err != nil {
		return fmt.Errorf("failed to connect to Apache Spark: %w", err)
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

// ParseConnectionString 解析 Apache Spark 连接字符串
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
		return "", 0, "", fmt.Errorf("invalid Apache Spark connection string format: %s", connStr)
	}

	host = parts[0]
	database = parts[2]

	port, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid port number: %s", parts[1])
	}

	return host, port, database, nil
}

// === ConnectionPoolPlugin 接口实现 ===

// CreateConnectionPool rejects SQL-driver pooling because Spark Thrift Server
// does not speak the MySQL wire protocol.
func (p *SparkSQLPlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
	return nil, fmt.Errorf("Spark Thrift Server does not support GORM connection pools")
}

// GetDialect 获取数据库方言
func (p *SparkSQLPlugin) GetDialect() string {
	return p.SQLDialect()
}

type sparkQueryFunc func(context.Context, plugin.ConnectionInfo, string) (*plugin.QueryResult, error)

func (p *SparkSQLPlugin) runQuery(ctx context.Context, connInfo plugin.ConnectionInfo, query string) (*plugin.QueryResult, error) {
	if p != nil && p.query != nil {
		return p.query(ctx, connInfo, query)
	}
	return executeSparkSQL(ctx, connInfo, query)
}

func executeSparkSQL(ctx context.Context, connInfo plugin.ConnectionInfo, query string) (*plugin.QueryResult, error) {
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = (&SparkSQLPlugin{}).DefaultPort()
	}

	database := plugin.GetString(connInfo, "database")
	if database == "" {
		database = "default"
	}
	user := plugin.GetString(connInfo, "user")
	password := plugin.GetString(connInfo, "password")

	if host == "" {
		return nil, fmt.Errorf("Spark 引擎缺少 host 配置")
	}

	configuration := gohive.NewConnectConfiguration()
	if user != "" {
		configuration.Username = user
		if password != "" {
			configuration.Password = password
		}
	}
	configuration.ConnectTimeout = 30 * time.Second
	configuration.SocketTimeout = 30 * time.Second

	connection, err := gohive.Connect(host, port, "NONE", configuration)
	if err != nil {
		return nil, fmt.Errorf("连接 Spark Thrift Server 失败：%w", err)
	}
	defer connection.Close()

	cursor := connection.Cursor()

	if database != "default" && database != "" {
		cursor.Exec(ctx, fmt.Sprintf("USE `%s`", database))
		if cursor.Err != nil {
			return nil, fmt.Errorf("切换数据库失败：%w", cursor.Err)
		}
	}

	cursor.Exec(ctx, query)
	if cursor.Err != nil {
		return nil, fmt.Errorf("执行 Spark SQL 失败：%w", cursor.Err)
	}

	var resultRows []map[string]interface{}
	var columns []string

	for cursor.HasMore(ctx) {
		row := cursor.RowMap(ctx)
		if cursor.Err != nil {
			return nil, fmt.Errorf("读取 Spark 结果失败：%w", cursor.Err)
		}
		if len(columns) == 0 {
			for k := range row {
				columns = append(columns, k)
			}
		}
		resultRows = append(resultRows, row)
	}

	return &plugin.QueryResult{Columns: columns, Rows: resultRows}, nil
}

// quoteSparkIdentifier 为 Spark SQL 标识符添加反引号以保留大小写
func quoteSparkIdentifier(identifier string) string {
	// Spark SQL 使用反引号（backtick）引用标识符
	// 转义标识符中的反引号（使用双反引号）
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}
