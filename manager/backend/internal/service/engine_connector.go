package service

import (
	"fmt"
	"sync"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// EngineConnector 引擎连接管理器
// 职责：
// 1. 管理数据库连接池
// 2. 提供连接获取和释放
// 3. 连接测试
// 4. 连接缓存（避免重复连接）
type EngineConnector struct {
	systemClient *commonClient.SystemClient
	mu           sync.RWMutex
	connections  map[uint]*gorm.DB // engineID -> connection
}

// NewEngineConnector 创建引擎连接管理器
func NewEngineConnector(systemClient *commonClient.SystemClient) *EngineConnector {
	return &EngineConnector{
		systemClient: systemClient,
		connections:  make(map[uint]*gorm.DB),
	}
}

// GetConnection 获取引擎的数据库连接
// 如果连接已缓存且有效，则返回缓存的连接
// 否则创建新连接并缓存
func (c *EngineConnector) GetConnection(engineID uint) (*gorm.DB, error) {
	// 1. 尝试从缓存获取连接
	c.mu.RLock()
	if conn, exists := c.connections[engineID]; exists {
		c.mu.RUnlock()
		// 测试连接是否有效
		if err := c.testConnection(conn); err == nil {
			return conn, nil
		}
		// 连接无效，需要重新创建
	} else {
		c.mu.RUnlock()
	}

	// 2. 创建新连接
	c.mu.Lock()
	defer c.mu.Unlock()

	// 双重检查（防止并发创建）
	if conn, exists := c.connections[engineID]; exists {
		if err := c.testConnection(conn); err == nil {
			return conn, nil
		}
		// 关闭无效连接
		sqlDB, _ := conn.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		delete(c.connections, engineID)
	}

	// 通过 SystemClient 获取引擎信息
	if c.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	engine, err := c.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	// 创建连接
	conn, err := c.createConnection(engine)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	// 缓存连接
	c.connections[engineID] = conn

	return conn, nil
}

// TestConnection 测试引擎连接
func (c *EngineConnector) TestConnection(engine *commonModels.Engine) error {
	conn, err := c.createConnection(engine)
	if err != nil {
		return err
	}

	// 测试连接
	if err := c.testConnection(conn); err != nil {
		return err
	}

	// 关闭测试连接（不缓存）
	sqlDB, _ := conn.DB()
	if sqlDB != nil {
		sqlDB.Close()
	}

	return nil
}

// CloseConnection 关闭并移除缓存的连接
func (c *EngineConnector) CloseConnection(engineID uint) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if conn, exists := c.connections[engineID]; exists {
		sqlDB, _ := conn.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		delete(c.connections, engineID)
	}
}

// CloseAll 关闭所有连接
func (c *EngineConnector) CloseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for engineID, conn := range c.connections {
		sqlDB, _ := conn.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		delete(c.connections, engineID)
	}
}

// 内部辅助方法

// createConnection 创建数据库连接
func (c *EngineConnector) createConnection(engine *commonModels.Engine) (*gorm.DB, error) {
	// 构建连接字符串
	connStr, err := dbbridge.BuildDSN(engine)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 根据引擎类型选择驱动
	var dialector gorm.Dialector

	switch engine.EngineType {
	case "postgresql", "PostgreSQL":
		dialector = postgres.Open(connStr)

	case "mysql", "MySQL", "doris", "Doris":
		dialector = mysql.Open(connStr)

	default:
		return nil, fmt.Errorf("unsupported database engine type: %s", engine.EngineType)
	}

	// 创建 GORM 连接
	db, err := gorm.Open(dialector, &gorm.Config{
		// 禁用默认事务（提高性能）
		SkipDefaultTransaction: true,
		// 准备语句缓存
		PrepareStmt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(5)          // 最大空闲连接数
	sqlDB.SetMaxOpenConns(20)         // 最大打开连接数
	sqlDB.SetConnMaxLifetime(0)       // 连接最大生命周期（0 表示永不过期）
	sqlDB.SetConnMaxIdleTime(0)       // 连接最大空闲时间（0 表示永不过期）

	return db, nil
}

// testConnection 测试连接是否有效
func (c *EngineConnector) testConnection(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// Ping 测试
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("connection ping failed: %w", err)
	}

	return nil
}
