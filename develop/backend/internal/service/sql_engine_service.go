package service

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/addp/develop/backend/internal/config"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"github.com/beltran/gohive"
	_ "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// SQLEngineService SQL执行引擎服务
// 负责管理数据库连接池，执行SQL语句
type SQLEngineService struct {
	cfg             *config.Config
	systemClient    *commonClient.SystemClient
	connectionPools map[uint]*gorm.DB // resourceID -> *gorm.DB
	mu              sync.RWMutex
	poolTTL         time.Duration
}

// NewSQLEngineService 创建SQL执行引擎服务
func NewSQLEngineService(
	cfg *config.Config,
	systemClient *commonClient.SystemClient,
) *SQLEngineService {
	return &SQLEngineService{
		cfg:             cfg,
		systemClient:    systemClient,
		connectionPools: make(map[uint]*gorm.DB),
		poolTTL:         30 * time.Minute, // 连接池30分钟TTL
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

// getConnection 获取或创建数据库连接
func (s *SQLEngineService) getConnection(resourceID uint) (*gorm.DB, error) {
	// 检查缓存
	s.mu.RLock()
	if db, ok := s.connectionPools[resourceID]; ok {
		s.mu.RUnlock()
		return db, nil
	}
	s.mu.RUnlock()

	// 获取资源配置
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource config: %w", err)
	}

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 日志：脱敏后的连接字符串
	maskedConnStr := maskConnectionString(connStr)
	log.Printf("🔗 [SQL Engine] 构建的连接字符串: %s", maskedConnStr)

	// 创建连接
	var db *gorm.DB
	switch strings.ToLower(resource.ResourceType) {
	case "postgresql":
		db, err = gorm.Open(postgres.Open(connStr), &gorm.Config{})
	case "mysql", "doris":
		db, err = gorm.Open(mysql.Open(connStr), &gorm.Config{})
	case "spark_sql":
		// Spark SQL 不支持 GORM，需要使用原生 database/sql
		return nil, fmt.Errorf("Spark SQL does not support connection pooling via GORM, please use direct SQL execution")
	default:
		return nil, fmt.Errorf("unsupported database type: %s", resource.ResourceType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 缓存连接
	s.mu.Lock()
	s.connectionPools[resourceID] = db
	s.mu.Unlock()

	log.Printf("✅ [SQL Engine] 创建连接池成功 resource_id=%d type=%s", resourceID, resource.ResourceType)

	return db, nil
}

// TestConnection 测试数据库连接
func (s *SQLEngineService) TestConnection(resourceID uint) error {
	// 获取资源配置
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return fmt.Errorf("failed to get resource config: %w", err)
	}

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 日志
	maskedConnStr := maskConnectionString(connStr)
	log.Printf("🔌 [SQL Engine] 测试连接: %s", maskedConnStr)

	// 创建临时连接（不缓存）
	switch strings.ToLower(resource.ResourceType) {
	case "postgresql":
		db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}

		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		defer sqlDB.Close()

		if err := sqlDB.Ping(); err != nil {
			return fmt.Errorf("database ping failed: %w", err)
		}

	case "mysql", "doris":
		// 使用 database/sql 直接连接（Doris 兼容性更好）
		sqlDB, err := sql.Open("mysql", connStr)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer sqlDB.Close()

		// 设置连接参数
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		sqlDB.SetConnMaxLifetime(10 * time.Second)

		// 设置连接超时
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 测试连接
		if err := sqlDB.PingContext(ctx); err != nil {
			return fmt.Errorf("database ping failed: %w", err)
		}

		// 执行简单查询验证
		var version string
		err = sqlDB.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
		if err != nil {
			return fmt.Errorf("failed to query version: %w", err)
		}
		log.Printf("✅ [SQL Engine] 数据库版本: %s", version)

	case "spark_sql":
		// Spark SQL Thrift Server 连接测试
		return s.testSparkSQLConnection(resource)

	default:
		return fmt.Errorf("unsupported database type: %s", resource.ResourceType)
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

// Close 关闭所有连接池
func (s *SQLEngineService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for resourceID, db := range s.connectionPools {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
			log.Printf("🔌 [SQL Engine] 关闭连接池 resource_id=%d", resourceID)
		}
	}

	s.connectionPools = make(map[uint]*gorm.DB)
	return nil
}

// maskConnectionString 脱敏连接字符串
func maskConnectionString(connStr string) string {
	masked := connStr
	// PostgreSQL 格式: password=XXX
	masked = strings.ReplaceAll(masked, "password=", "password=***MASKED*** ")
	// MySQL/Doris 格式: user:password@tcp (脱敏密码部分)
	if strings.Contains(masked, "@tcp") {
		parts := strings.Split(masked, "@tcp")
		if len(parts) == 2 {
			userPart := parts[0]
			if colonIdx := strings.LastIndex(userPart, ":"); colonIdx != -1 {
				masked = userPart[:colonIdx+1] + "***MASKED***" + "@tcp" + parts[1]
			}
		}
	}
	return masked
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
