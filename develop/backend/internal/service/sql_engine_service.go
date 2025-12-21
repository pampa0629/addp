package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/dbbridge"
	"github.com/addp/develop/backend/internal/config"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"github.com/beltran/gohive"
	"gorm.io/gorm"
)

// SQLEngineService SQL执行引擎服务
// 使用插件系统的连接池管理，执行SQL语句
type SQLEngineService struct {
	cfg          *config.Config
	systemClient *commonClient.SystemClient
}

// NewSQLEngineService 创建SQL执行引擎服务
func NewSQLEngineService(
	cfg *config.Config,
	systemClient *commonClient.SystemClient,
) *SQLEngineService {
	return &SQLEngineService{
		cfg:          cfg,
		systemClient: systemClient,
	}
}

// SQLResult SQL执行结果
type SQLResult struct {
	Columns      []string                 `json:"columns"`
	Rows         []map[string]interface{} `json:"rows"`
	RowsAffected int64                    `json:"rows_affected"`
}

// ExecuteSQL 执行SQL语句（查询）
func (s *SQLEngineService) ExecuteSQL(
	ctx context.Context,
	resourceID uint,
	sqlContent string,
	timeout int,
) (*SQLResult, error) {
	// 设置超时
	if timeout <= 0 {
		timeout = s.cfg.DefaultQueryTimeout
	}
	if timeout > s.cfg.MaxQueryTimeout {
		timeout = s.cfg.MaxQueryTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 获取资源配置
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource config: %w", err)
	}

	// Spark SQL 特殊处理
	if strings.ToLower(resource.ResourceType) == "spark_sql" {
		return s.executeSparkSQL(execCtx, resource, sqlContent)
	}

	// 获取数据库连接（PostgreSQL、MySQL、Doris）
	db, err := s.getConnection(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	// 执行SQL with context
	rows, err := db.WithContext(execCtx).Raw(sqlContent).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 获取列名
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// 扫描结果
	var results []map[string]interface{}
	for rows.Next() {
		// 创建值容器
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// 扫描行
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// 构造结果映射
		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			// 处理 []byte 类型（转换为字符串）
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return &SQLResult{
		Columns:      columns,
		Rows:         results,
		RowsAffected: int64(len(results)),
	}, nil
}

// ExecuteDML 执行DML语句 (INSERT/UPDATE/DELETE)
func (s *SQLEngineService) ExecuteDML(
	ctx context.Context,
	resourceID uint,
	sqlContent string,
	timeout int,
) (int64, error) {
	// 设置超时
	if timeout <= 0 {
		timeout = s.cfg.DefaultQueryTimeout
	}
	if timeout > s.cfg.MaxQueryTimeout {
		timeout = s.cfg.MaxQueryTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 获取连接
	db, err := s.getConnection(resourceID)
	if err != nil {
		return 0, err
	}

	// 执行DML
	result := db.WithContext(execCtx).Exec(sqlContent)
	if result.Error != nil {
		return 0, result.Error
	}

	return result.RowsAffected, nil
}

// getConnection 从插件系统获取或创建数据库连接
func (s *SQLEngineService) getConnection(resourceID uint) (*gorm.DB, error) {
	// 获取资源配置
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource config: %w", err)
	}

	// Spark SQL 特殊处理：不支持GORM连接池
	if strings.ToLower(resource.ResourceType) == "spark_sql" {
		return nil, fmt.Errorf("Spark SQL does not support connection pooling via GORM, please use direct SQL execution")
	}

	// 从插件系统获取或创建连接池
	db, err := dbbridge.GetOrCreatePool(resource, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}

	log.Printf("✅ [SQL Engine] 获取连接池成功 resource_id=%d type=%s", resourceID, resource.ResourceType)

	return db, nil
}

// TestConnection 测试数据库连接
func (s *SQLEngineService) TestConnection(resourceID uint) error {
	// 获取资源配置
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return fmt.Errorf("failed to get resource config: %w", err)
	}

	// Spark SQL 特殊处理
	if strings.ToLower(resource.ResourceType) == "spark_sql" {
		return s.testSparkSQLConnection(resource)
	}

	// 使用插件系统测试连接
	log.Printf("🔌 [SQL Engine] 测试连接 resource_id=%d type=%s", resourceID, resource.ResourceType)

	if err := dbbridge.TestConnection(context.Background(), resource); err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}

	log.Printf("✅ [SQL Engine] 连接测试成功 resource_id=%d type=%s", resourceID, resource.ResourceType)
	return nil
}

// ListDatabaseResources 获取可用的数据库资源列表
func (s *SQLEngineService) ListDatabaseResources(ctx context.Context, tenantID uint) ([]commonModels.Resource, error) {
	// 调用 SystemClient 获取租户的所有资源
	allResources, err := s.systemClient.ListResources("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resources from system: %w", err)
	}

	// 过滤出支持 SQL 开发模式的资源
	var dbResources []commonModels.Resource
	for _, res := range allResources {
		if utils.SupportsDevMode(&res, "sql") {
			dbResources = append(dbResources, res)
		}
	}

	log.Printf("✅ [SQL Engine] 获取数据库资源列表成功 (tenant_id=%d, total=%d)", tenantID, len(dbResources))
	return dbResources, nil
}


// executeSparkSQL 执行 Spark SQL 查询
func (s *SQLEngineService) executeSparkSQL(
	ctx context.Context,
	resource *commonModels.Resource,
	sqlContent string,
) (*SQLResult, error) {
	// 解析连接信息
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 解析连接字符串 (格式: host:port:database)
	parts := strings.Split(connStr, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid Spark SQL connection string format: %s", connStr)
	}

	host := parts[0]
	portStr := parts[1]
	database := parts[2]

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port number: %s", portStr)
	}

	// 获取认证信息
	connInfo := resource.ConnectionInfo
	user, _ := connInfo["user"].(string)
	password, _ := connInfo["password"].(string)

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
		return nil, fmt.Errorf("failed to connect to Spark SQL: %w", err)
	}
	defer connection.Close()

	// 创建 cursor
	cursor := connection.Cursor()

	// 切换到指定数据库
	if database != "default" && database != "" {
		cursor.Exec(ctx, fmt.Sprintf("USE %s", database))
		if cursor.Err != nil {
			return nil, fmt.Errorf("failed to use database '%s': %w", database, cursor.Err)
		}
	}

	// 执行查询
	cursor.Exec(ctx, sqlContent)
	if cursor.Err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", cursor.Err)
	}

	// 获取结果
	var results []map[string]interface{}
	var columns []string

	for cursor.HasMore(ctx) {
		row := cursor.RowMap(ctx)
		if cursor.Err != nil {
			return nil, fmt.Errorf("failed to fetch row: %w", cursor.Err)
		}

		// 从第一行提取列名
		if len(columns) == 0 && len(row) > 0 {
			for colName := range row {
				columns = append(columns, colName)
			}
		}

		results = append(results, row)
	}

	log.Printf("✅ [SQL Engine] Spark SQL 查询成功，返回 %d 行", len(results))

	return &SQLResult{
		Columns:      columns,
		Rows:         results,
		RowsAffected: int64(len(results)),
	}, nil
}

// testSparkSQLConnection 测试 Spark SQL Thrift Server 连接
func (s *SQLEngineService) testSparkSQLConnection(resource *commonModels.Resource) error {
	// 解析连接信息
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 解析连接字符串 (格式: host:port:database)
	parts := strings.Split(connStr, ":")
	if len(parts) != 3 {
		return fmt.Errorf("invalid Spark SQL connection string format: %s", connStr)
	}

	host := parts[0]
	portStr := parts[1]
	database := parts[2]

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid port number: %s", portStr)
	}

	// 获取认证信息
	connInfo := resource.ConnectionInfo
	user, _ := connInfo["user"].(string)
	password, _ := connInfo["password"].(string)

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
	if database != "default" && database != "" {
		cursor.Exec(context.Background(), fmt.Sprintf("USE %s", database))
		if cursor.Err != nil {
			return fmt.Errorf("failed to use database '%s': %w", database, cursor.Err)
		}
	}

	// 执行简单查询验证
	cursor.Exec(context.Background(), "SELECT 1")
	if cursor.Err != nil {
		return fmt.Errorf("failed to execute test query: %w", cursor.Err)
	}

	log.Printf("✅ [SQL Engine] Spark SQL 连接测试成功")
	return nil
}
