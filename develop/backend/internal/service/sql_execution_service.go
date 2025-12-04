package service

import (
	"context"
	"fmt"
	"log"
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

	// 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

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
