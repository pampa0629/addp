package spark_sql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/beltran/gohive"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// SparkSQLPlugin Apache Spark Thrift Server 插件
type SparkSQLPlugin struct{}

func init() {
	plugin.Register(&SparkSQLPlugin{})
}

func (p *SparkSQLPlugin) Type() string {
	return "spark"
}

func (p *SparkSQLPlugin) DisplayName() string {
	return "Apache Spark"
}

func (p *SparkSQLPlugin) EngineCategory() string {
	return "standard"
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

func (p *SparkSQLPlugin) GenerateCapabilities() string {
	return `{"compute":[{"dev_modes":["query"],"description":"Apache Spark查询","features":["distributed","big_data"]}]}`
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

// CreateConnectionPool 创建GORM连接池
// 注意：Apache Spark 使用Thrift协议，这里创建一个兼容的连接池
func (p *SparkSQLPlugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
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
		return nil, fmt.Errorf("missing required field: host")
	}

	// Apache Spark 通过Thrift协议，我们使用MySQL兼容模式
	// 构建DSN: user:password@tcp(host:port)/database
	var dsn string
	if user != "" && password != "" {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", user, password, host, port, database)
	} else if user != "" {
		dsn = fmt.Sprintf("%s@tcp(%s:%d)/%s", user, host, port, database)
	} else {
		dsn = fmt.Sprintf("tcp(%s:%d)/%s", host, port, database)
	}

	// 创建GORM连接（使用MySQL驱动，因为 Apache Spark 兼容MySQL协议）
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
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
func (p *SparkSQLPlugin) GetDialect() string {
	return "mysql" // Apache Spark 使用MySQL兼容协议
}

// === MetadataPlugin 接口实现 ===

// ListSchemas 列出所有Schema（Apache Spark 中对应Database）
func (p *SparkSQLPlugin) ListSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
	var schemas []plugin.SchemaInfo

	// Apache Spark 使用 SHOW DATABASES 命令
	rows, err := db.WithContext(ctx).Raw("SHOW DATABASES").Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			continue
		}

		// 获取每个数据库的表数量
		var tableCount int
		// 重要：使用 quoteSparkIdentifier 保留标识符大小写
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.information_schema.tables WHERE table_schema = ?",
			quoteSparkIdentifier(dbName))
		db.WithContext(ctx).Raw(countQuery, dbName).Scan(&tableCount)

		schemas = append(schemas, plugin.SchemaInfo{
			Name:       dbName,
			TableCount: tableCount,
		})
	}

	return schemas, nil
}

// ListTables 列出指定Schema下的所有表
func (p *SparkSQLPlugin) ListTables(ctx context.Context, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
	var tables []plugin.TableInfo

	// 切换到指定数据库
	// 重要：使用 quoteSparkIdentifier 保留标识符大小写
	if err := db.WithContext(ctx).Exec(fmt.Sprintf("USE %s", quoteSparkIdentifier(schema))).Error; err != nil {
		return nil, fmt.Errorf("failed to use database: %w", err)
	}

	// 使用 SHOW TABLES 命令
	rows, err := db.WithContext(ctx).Raw("SHOW TABLES").Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}

		// 获取表信息
		tableInfo := plugin.TableInfo{
			Schema:    schema,
			TableName: tableName,
			RowCount:  0,
			SizeBytes: 0,
		}

		// 尝试获取行数（使用DESCRIBE EXTENDED可能会更准确）
		var count sql.NullInt64
		// 重要：使用 quoteSparkIdentifier 保留标识符大小写
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s LIMIT 1",
			quoteSparkIdentifier(schema), quoteSparkIdentifier(tableName))
		db.WithContext(ctx).Raw(countQuery).Scan(&count)
		if count.Valid {
			tableInfo.RowCount = count.Int64
		}

		tables = append(tables, tableInfo)
	}

	return tables, nil
}

// ListColumns 列出指定表的所有列
func (p *SparkSQLPlugin) ListColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ColumnInfo, error) {
	var columns []plugin.ColumnInfo

	// 切换到指定数据库
	// 重要：使用 quoteSparkIdentifier 保留标识符大小写
	if err := db.WithContext(ctx).Exec(fmt.Sprintf("USE %s", quoteSparkIdentifier(schema))).Error; err != nil {
		return nil, fmt.Errorf("failed to use database: %w", err)
	}

	// 使用 DESCRIBE 命令
	// 重要：使用 quoteSparkIdentifier 保留标识符大小写
	query := fmt.Sprintf("DESCRIBE %s", quoteSparkIdentifier(table))
	rows, err := db.WithContext(ctx).Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var colName, dataType, comment sql.NullString
		if err := rows.Scan(&colName, &dataType, &comment); err != nil {
			continue
		}

		if !colName.Valid || !dataType.Valid {
			continue
		}

		column := plugin.ColumnInfo{
			ColumnName:   colName.String,
			DataType:     dataType.String,
			IsNullable:   true, // Apache Spark 默认允许NULL
			IsPrimaryKey: false,
			Comment:      "",
		}

		if comment.Valid {
			column.Comment = comment.String
		}

		columns = append(columns, column)
	}

	return columns, nil
}

// GetTableRowCount 获取表的行数
func (p *SparkSQLPlugin) GetTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
	var count int64

	// 切换到指定数据库
	if err := db.WithContext(ctx).Exec(fmt.Sprintf("USE %s", schema)).Error; err != nil {
		return 0, fmt.Errorf("failed to use database: %w", err)
	}

	// 执行COUNT查询
	// 重要：使用 quoteSparkIdentifier 保留标识符大小写
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteSparkIdentifier(table))
	err := db.WithContext(ctx).Raw(query).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	return count, nil
}

// quoteSparkIdentifier 为 Spark SQL 标识符添加反引号以保留大小写
func quoteSparkIdentifier(identifier string) string {
	// Spark SQL 使用反引号（backtick）引用标识符
	// 转义标识符中的反引号（使用双反引号）
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}
