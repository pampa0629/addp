package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/develop/backend/internal/config"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"gorm.io/gorm"
)

// SQLEngineService SQL执行引擎服务
// 使用 dbbridge 统一执行层，不再包含引擎特定逻辑
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
	GraphData    *plugin.GraphData        `json:"graph_data,omitempty"` // 图数据（仅图数据库引擎返回）
}

// GetEngine 获取引擎配置（供 handler 层判断引擎类型）
func (s *SQLEngineService) GetEngine(engineID uint) (*commonModels.Engine, error) {
	return s.systemClient.GetEngine(engineID)
}

// ExecuteSQL 执行查询语句（统一路由到 dbbridge.ExecuteQuery）
func (s *SQLEngineService) ExecuteSQL(
	ctx context.Context,
	engineID uint,
	sqlContent string,
	timeout int,
) (*SQLResult, error) {
	if timeout <= 0 {
		timeout = s.cfg.DefaultQueryTimeout
	}
	if timeout > s.cfg.MaxQueryTimeout {
		timeout = s.cfg.MaxQueryTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	resource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("获取引擎配置失败：%w", err)
	}

	qr, err := dbbridge.ExecuteGraphQuery(execCtx, resource, sqlContent)
	if err != nil {
		return nil, err
	}

	rows := qr.Rows
	if rows == nil {
		rows = []map[string]interface{}{}
	}

	return &SQLResult{
		Columns:      qr.Columns,
		Rows:         rows,
		RowsAffected: int64(len(rows)),
		GraphData:    qr.GraphData,
	}, nil
}

// ExecuteDML 执行 DML 语句（INSERT/UPDATE/DELETE），仅适用于 SQL 引擎
func (s *SQLEngineService) ExecuteDML(
	ctx context.Context,
	engineID uint,
	sqlContent string,
	timeout int,
) (int64, error) {
	if timeout <= 0 {
		timeout = s.cfg.DefaultQueryTimeout
	}
	if timeout > s.cfg.MaxQueryTimeout {
		timeout = s.cfg.MaxQueryTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	resource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return 0, fmt.Errorf("获取引擎配置失败：%w", err)
	}

	return dbbridge.ExecuteDML(execCtx, resource, sqlContent)
}

// TestConnection 测试数据库连接
func (s *SQLEngineService) TestConnection(engineID uint) error {
	resource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return fmt.Errorf("获取引擎配置失败：%w", err)
	}

	log.Printf("🔌 [SQL Engine] 测试连接 engine_id=%d type=%s", engineID, resource.EngineType)

	if err := dbbridge.TestConnection(context.Background(), resource); err != nil {
		return fmt.Errorf("连接测试失败：%w", err)
	}

	log.Printf("✅ [SQL Engine] 连接测试成功 engine_id=%d type=%s", engineID, resource.EngineType)
	return nil
}

// ListDatabaseResources 获取可用的数据库资源列表（支持 query 模式的引擎）
func (s *SQLEngineService) ListDatabaseResources(ctx context.Context, tenantID uint) ([]commonModels.Engine, error) {
	allResources, err := s.systemClient.ListEngines("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取引擎列表失败：%w", err)
	}

	var dbResources []commonModels.Engine
	for _, res := range allResources {
		if utils.SupportsDevMode(&res, "query") {
			dbResources = append(dbResources, res)
		}
	}

	log.Printf("✅ [SQL Engine] 获取数据库资源列表成功 (tenant_id=%d, total=%d)", tenantID, len(dbResources))
	return dbResources, nil
}

// GenerateSampleQuery 生成该引擎的可执行样例查询（用于查询工作台切换引擎时自动填充）
func (s *SQLEngineService) GenerateSampleQuery(ctx context.Context, engineID uint) (query string, language string, err error) {
	resource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return "", "", fmt.Errorf("获取引擎配置失败：%w", err)
	}
	q, lang := dbbridge.GenerateSampleQuery(ctx, resource)
	return q, lang, nil
}

// getConnection 保留供遗留代码使用，新代码请直接使用 dbbridge.ExecuteQuery
// Deprecated: 使用 dbbridge.ExecuteQuery 替代
func (s *SQLEngineService) getConnection(engineID uint) (*gorm.DB, error) {
	resource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("获取引擎配置失败：%w", err)
	}
	return dbbridge.GetOrCreatePool(resource, dbbridge.DefaultPoolConfig())
}
