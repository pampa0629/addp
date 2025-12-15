package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/beltran/gohive"
)

// SparkSQLExecutor Spark SQL执行器（通过Hive Thrift Server）
type SparkSQLExecutor struct {
	cfg             *config.Config
	systemClient    *commonClient.SystemClient
	connectionPools map[uint]*gohive.Connection // resourceID -> *gohive.Connection
	mu              sync.RWMutex
	poolTTL         time.Duration
}

// NewSparkSQLExecutor 创建Spark SQL执行器
func NewSparkSQLExecutor(
	cfg *config.Config,
	systemClient *commonClient.SystemClient,
) *SparkSQLExecutor {
	return &SparkSQLExecutor{
		cfg:             cfg,
		systemClient:    systemClient,
		connectionPools: make(map[uint]*gohive.Connection),
		poolTTL:         30 * time.Minute, // 连接池30分钟TTL
	}
}

// Execute 执行Spark SQL语句
func (e *SparkSQLExecutor) Execute(
	ctx context.Context,
	resourceID uint,
	sqlContent string,
) (*models.ExecutionResponse, error) {
	startTime := time.Now()

	// 获取Spark SQL连接
	conn, err := e.getConnection(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get Spark SQL connection: %w", err)
	}

	// 创建Cursor执行SQL
	cursor := conn.Cursor()
	defer cursor.Close()

	// 执行SQL
	cursor.Exec(ctx, sqlContent)
	if cursor.Err != nil {
		return nil, fmt.Errorf("Spark SQL execution failed: %w", cursor.Err)
	}

	// 检查是否有结果集
	if !cursor.HasMore(ctx) {
		// DML操作（INSERT/UPDATE/DELETE）或DDL操作
		executionTime := int(time.Since(startTime).Milliseconds())
		return &models.ExecutionResponse{
			Columns:         []string{},
			Rows:            []map[string]interface{}{},
			RowsAffected:    0,
			ExecutionTimeMs: executionTime,
		}, nil
	}

	// 获取列信息
	var columns []string
	columnDescriptions := cursor.Description()
	for _, desc := range columnDescriptions {
		// Description() 返回 [][]string，每个元素是 [columnName, columnType, ...]
		// 我们只需要列名（第一个元素）
		if len(desc) > 0 {
			columns = append(columns, desc[0])
		}
	}

	// 提取结果行
	var rows []map[string]interface{}
	for cursor.HasMore(ctx) {
		var rowValues []interface{}
		cursor.FetchOne(ctx, &rowValues)
		if cursor.Err != nil {
			return nil, fmt.Errorf("failed to fetch row: %w", cursor.Err)
		}

		// 构造结果映射
		row := make(map[string]interface{})
		for i, col := range columns {
			if i < len(rowValues) {
				val := rowValues[i]
				// 处理 []byte 类型（转换为字符串）
				if b, ok := val.([]byte); ok {
					row[col] = string(b)
				} else {
					row[col] = val
				}
			}
		}
		rows = append(rows, row)
	}

	executionTime := int(time.Since(startTime).Milliseconds())
	return &models.ExecutionResponse{
		Columns:         columns,
		Rows:            rows,
		RowsAffected:    len(rows),
		ExecutionTimeMs: executionTime,
	}, nil
}

// getConnection 获取或创建Spark SQL连接
func (e *SparkSQLExecutor) getConnection(resourceID uint) (*gohive.Connection, error) {
	// 检查缓存
	e.mu.RLock()
	if conn, ok := e.connectionPools[resourceID]; ok {
		e.mu.RUnlock()
		return conn, nil
	}
	e.mu.RUnlock()

	// 获取资源配置
	resource, err := e.systemClient.GetResource(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource config: %w", err)
	}

	// 验证资源类型
	if strings.ToLower(resource.ResourceType) != "spark_sql" {
		return nil, fmt.Errorf("resource type must be spark_sql, got: %s", resource.ResourceType)
	}

	// 解析连接配置
	host, ok := resource.ConnectionInfo["host"].(string)
	if !ok || host == "" {
		return nil, fmt.Errorf("missing or invalid 'host' in connection_info")
	}

	port := 10000 // 默认Thrift Server端口
	if p, ok := resource.ConnectionInfo["port"].(float64); ok {
		port = int(p)
	} else if p, ok := resource.ConnectionInfo["port"].(int); ok {
		port = p
	}

	database := "default"
	if db, ok := resource.ConnectionInfo["database"].(string); ok && db != "" {
		database = db
	}

	// 连接配置
	configuration := gohive.NewConnectConfiguration()
	configuration.Database = database
	configuration.ConnectTimeout = 30 // 30秒连接超时

	// 可选：认证配置（如果Spark启用了认证）
	if username, ok := resource.ConnectionInfo["username"].(string); ok && username != "" {
		configuration.Username = username
	}
	if password, ok := resource.ConnectionInfo["password"].(string); ok && password != "" {
		configuration.Password = password
	}

	// 创建连接
	conn, err := gohive.Connect(host, port, "HIVE", configuration)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Spark Thrift Server at %s:%d: %w", host, port, err)
	}

	// 测试连接
	cursor := conn.Cursor()
	cursor.Exec(context.Background(), "SELECT 1")
	cursor.Close()
	if cursor.Err != nil {
		conn.Close()
		return nil, fmt.Errorf("connection test failed: %w", cursor.Err)
	}

	// 缓存连接
	e.mu.Lock()
	e.connectionPools[resourceID] = conn
	e.mu.Unlock()

	log.Printf("✅ Created Spark SQL connection for resource %d (%s:%d, db=%s)",
		resourceID, host, port, database)

	return conn, nil
}

// TestConnection 测试Spark SQL连接
func (e *SparkSQLExecutor) TestConnection(resourceID uint) error {
	conn, err := e.getConnection(resourceID)
	if err != nil {
		return err
	}

	// 执行简单查询测试
	cursor := conn.Cursor()
	defer cursor.Close()

	cursor.Exec(context.Background(), "SELECT 1 AS test")
	if cursor.Err != nil {
		return fmt.Errorf("test query failed: %w", cursor.Err)
	}

	log.Printf("✅ Spark SQL connection test passed for resource %d", resourceID)
	return nil
}

// Close 关闭所有连接池
func (e *SparkSQLExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for resourceID, conn := range e.connectionPools {
		conn.Close()
		log.Printf("Closed Spark SQL connection for resource %d", resourceID)
	}

	e.connectionPools = make(map[uint]*gohive.Connection)
	return nil
}

// ListSparkSQLResources 获取可用的Spark SQL资源列表
func (e *SparkSQLExecutor) ListSparkSQLResources(ctx context.Context, tenantID uint) ([]commonModels.Resource, error) {
	// 调用 SystemClient 获取租户的所有资源
	allResources, err := e.systemClient.ListResources("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resources from system: %w", err)
	}

	// 过滤出Spark SQL类型的资源
	var sparkResources []commonModels.Resource
	for _, res := range allResources {
		resourceType := strings.ToLower(res.ResourceType)
		if resourceType == "spark_sql" {
			sparkResources = append(sparkResources, res)
		}
	}

	log.Printf("✅ Develop: 获取Spark SQL资源列表成功 (tenant_id=%d, total=%d)", tenantID, len(sparkResources))
	return sparkResources, nil
}
