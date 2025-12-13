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
	"github.com/addp/develop/backend/internal/repository"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// SQLExecutionService SQL执行服务
type SQLExecutionService struct {
	cfg              *config.Config
	executionRepo    *repository.ExecutionRepository
	systemClient     *commonClient.SystemClient
	connectionPools  map[uint]*gorm.DB // resourceID -> *gorm.DB
	mu               sync.RWMutex
	poolTTL          time.Duration
}

// NewSQLExecutionService 创建SQL执行服务
func NewSQLExecutionService(
	cfg *config.Config,
	executionRepo *repository.ExecutionRepository,
	systemClient *commonClient.SystemClient,
) *SQLExecutionService {
	return &SQLExecutionService{
		cfg:             cfg,
		executionRepo:   executionRepo,
		systemClient:    systemClient,
		connectionPools: make(map[uint]*gorm.DB),
		poolTTL:         30 * time.Minute, // 连接池30分钟TTL
	}
}

// Execute 执行SQL语句
func (s *SQLExecutionService) Execute(
	ctx context.Context,
	userID, tenantID, resourceID uint,
	sqlContent string,
	timeout int,
) (*models.ExecutionResponse, error) {
	startTime := time.Now()

	// 创建执行记录
	execution := &models.Execution{
		ResourceID:  resourceID,
		SQLContent:  sqlContent,
		Status:      "running",
		ExecutedBy:  userID,
		TenantID:    tenantID,
		StartedAt:   startTime,
	}

	if err := s.executionRepo.Create(execution); err != nil {
		return nil, fmt.Errorf("failed to create execution record: %w", err)
	}

	// 设置超时
	if timeout <= 0 {
		timeout = s.cfg.DefaultQueryTimeout
	}
	if timeout > s.cfg.MaxQueryTimeout {
		timeout = s.cfg.MaxQueryTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 执行SQL
	result, err := s.executeSQL(execCtx, resourceID, sqlContent)
	executionTime := int(time.Since(startTime).Milliseconds())

	// 更新执行记录
	now := time.Now()
	execution.CompletedAt = &now
	execution.ExecutionTimeMs = &executionTime

	if err != nil {
		execution.Status = "failed"
		execution.ErrorMessage = err.Error()
		_ = s.executionRepo.Update(execution)
		return nil, fmt.Errorf("SQL execution failed: %w", err)
	}

	execution.Status = "success"
	if result != nil {
		execution.RowsAffected = &result.RowsAffected
	}
	_ = s.executionRepo.Update(execution)

	// 返回响应
	if result == nil {
		result = &models.ExecutionResponse{
			Columns:         []string{},
			Rows:            []map[string]interface{}{},
			RowsAffected:    0,
			ExecutionTimeMs: executionTime,
			ExecutionID:     execution.ID,
		}
	}
	result.ExecutionTimeMs = executionTime
	result.ExecutionID = execution.ID

	return result, nil
}

// executeSQL 执行SQL（内部方法）
func (s *SQLExecutionService) executeSQL(
	ctx context.Context,
	resourceID uint,
	sqlContent string,
) (*models.ExecutionResponse, error) {
	// 获取数据库连接
	db, err := s.getConnection(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	// 执行SQL with context
	rows, err := db.WithContext(ctx).Raw(sqlContent).Rows()
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

	return &models.ExecutionResponse{
		Columns:      columns,
		Rows:         results,
		RowsAffected: len(results),
	}, nil
}

// getConnection 获取或创建数据库连接
func (s *SQLExecutionService) getConnection(resourceID uint) (*gorm.DB, error) {
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

	// 日志：检查接收到的密码
	if pwd, ok := resource.ConnectionInfo["password"].(string); ok {
		log.Printf("📥 [DEVELOP] 从 SystemClient 接收到的密码长度: %d | 前20字符: %s...",
			len(pwd), pwd[:min(len(pwd), 20)])
	} else {
		log.Printf("⚠️ [DEVELOP] 接收到的 ConnectionInfo 中没有 password 字段或类型不是 string")
	}

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 日志：脱敏后的连接字符串
	maskedConnStr := strings.ReplaceAll(connStr, "password=", "password=***MASKED*** ")
	log.Printf("🔗 [DEVELOP] 构建的连接字符串: %s", maskedConnStr)

	// 创建连接
	var db *gorm.DB
	switch resource.ResourceType {
	case "postgresql":
		db, err = gorm.Open(postgres.Open(connStr), &gorm.Config{})
	case "mysql":
		db, err = gorm.Open(mysql.Open(connStr), &gorm.Config{})
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

	log.Printf("✅ Created connection pool for resource %d (%s)", resourceID, resource.ResourceType)

	return db, nil
}

// Close 关闭所有连接池
func (s *SQLExecutionService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for resourceID, db := range s.connectionPools {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
			log.Printf("Closed connection pool for resource %d", resourceID)
		}
	}

	s.connectionPools = make(map[uint]*gorm.DB)
	return nil
}

// ExecuteDML 执行DML语句 (INSERT/UPDATE/DELETE)
func (s *SQLExecutionService) ExecuteDML(
	ctx context.Context,
	userID, tenantID, resourceID uint,
	sqlContent string,
	timeout int,
) (int64, error) {
	// 设置超时
	if timeout <= 0 {
		timeout = s.cfg.DefaultQueryTimeout
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

// TestConnection 测试数据库连接
func (s *SQLExecutionService) TestConnection(resourceID uint) error {
	db, err := s.getConnection(resourceID)
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	return sqlDB.Ping()
}

// TestConnectionWithToken 使用指定的 token 测试数据库连接
func (s *SQLExecutionService) TestConnectionWithToken(resourceID uint, token string) error {
	log.Printf("🔍 [TEST] 开始测试连接 resourceID=%d", resourceID)

	// 使用 Internal API Key 获取未加密的资源配置（服务间调用）
	// 注意：这里忽略了传入的 JWT token，因为内部 API 使用 Internal API Key 认证
	systemClient := commonClient.NewSystemClientWithInternalKey(s.cfg.SystemServiceURL, s.cfg.InternalAPIKey)

	// 获取资源配置
	resource, err := systemClient.GetResource(resourceID)
	if err != nil {
		log.Printf("❌ [TEST] 获取资源配置失败: %v", err)
		return fmt.Errorf("failed to get resource config: %w", err)
	}

	// 日志：检查接收到的密码
	if pwd, ok := resource.ConnectionInfo["password"].(string); ok {
		log.Printf("📥 [TEST] 从 SystemClient 接收到的密码长度: %d | 前20字符: %s...",
			len(pwd), pwd[:min(len(pwd), 20)])
	} else {
		log.Printf("⚠️ [TEST] 接收到的 ConnectionInfo 中没有 password 字段或类型不是 string")
	}

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		log.Printf("❌ [TEST] 构建连接字符串失败: %v", err)
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 日志：脱敏后的连接字符串
	maskedConnStr := strings.ReplaceAll(connStr, "password=", "password=***MASKED*** ")
	log.Printf("🔗 [TEST] 构建的连接字符串: %s", maskedConnStr)

	// 创建临时连接（不缓存）
	var db *gorm.DB
	switch resource.ResourceType {
	case "postgresql":
		db, err = gorm.Open(postgres.Open(connStr), &gorm.Config{})
	case "mysql":
		db, err = gorm.Open(mysql.Open(connStr), &gorm.Config{})
	default:
		return fmt.Errorf("unsupported database type: %s", resource.ResourceType)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// 测试连接
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close() // 测试完成后关闭连接

	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	log.Printf("✅ Database connection test passed for resource %d (%s)", resourceID, resource.ResourceType)
	return nil
}

// ListDatabaseResources 获取可用的数据库资源列表（仅 PostgreSQL 和 MySQL）
func (s *SQLExecutionService) ListDatabaseResources(ctx context.Context, tenantID uint) ([]commonModels.Resource, error) {
	// 调用 SystemClient 获取租户的所有资源
	allResources, err := s.systemClient.ListResources("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resources from system: %w", err)
	}

	// 过滤出数据库类型的资源
	var dbResources []commonModels.Resource
	for _, res := range allResources {
		resourceType := strings.ToLower(res.ResourceType)
		if resourceType == "postgresql" || resourceType == "mysql" {
			dbResources = append(dbResources, res)
		}
	}

	log.Printf("✅ Develop: 获取数据库资源列表成功 (tenant_id=%d, total=%d)", tenantID, len(dbResources))
	return dbResources, nil
}

// ListDatabaseResourcesWithToken 使用指定的 token 获取数据库资源列表
func (s *SQLExecutionService) ListDatabaseResourcesWithToken(ctx context.Context, tenantID uint, token string) ([]commonModels.Resource, error) {
	// 创建临时的 SystemClient（使用传入的 token）
	systemClient := commonClient.NewSystemClient(s.cfg.SystemServiceURL, token)

	// 调用 SystemClient 获取租户的所有资源
	allResources, err := systemClient.ListResources("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resources from system: %w", err)
	}

	// 过滤出数据库类型的资源
	var dbResources []commonModels.Resource
	for _, res := range allResources {
		resourceType := strings.ToLower(res.ResourceType)
		if resourceType == "postgresql" || resourceType == "mysql" {
			dbResources = append(dbResources, res)
		}
	}

	log.Printf("✅ Develop: 获取数据库资源列表成功 (tenant_id=%d, total=%d)", tenantID, len(dbResources))
	return dbResources, nil
}
