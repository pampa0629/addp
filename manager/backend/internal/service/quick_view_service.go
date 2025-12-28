package service

import (
	"context"
	"fmt"

	"github.com/addp/common/logger"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/worker"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// QuickViewService 快显服务层
type QuickViewService struct {
	repo         *repository.QuickViewRepository
	taskQueue    *worker.TaskQueue
	systemClient *commonClient.SystemClient
	metaClient   *commonClient.MetaClient
}

// NewQuickViewService 创建快显服务
func NewQuickViewService(
	db *gorm.DB,
	taskQueue *worker.TaskQueue,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
) *QuickViewService {
	return &QuickViewService{
		repo:         repository.NewQuickViewRepository(db),
		taskQueue:    taskQueue,
		systemClient: systemClient,
		metaClient:   metaClient,
	}
}

// TriggerQuickViewParams 触发快显参数
type TriggerQuickViewParams struct {
	TenantID   uint
	ResourceID uint
	SchemaName string
	TableName  string
	MinZoom    *int    // 可选，不指定则自动计算
	MaxZoom    int     // 默认18
	Concurrency int    // 默认10
	Priority   string  // "critical", "default", "low"
}

// TriggerQuickView 触发快显缓存生成
func (s *QuickViewService) TriggerQuickView(ctx context.Context, params TriggerQuickViewParams) error {
	// 1. 检查是否已在生成中（并发控制）
	isGenerating, err := s.repo.IsGenerating(params.TenantID, params.ResourceID, params.SchemaName, params.TableName)
	if err != nil {
		return fmt.Errorf("failed to check generating status: %w", err)
	}

	if isGenerating {
		return fmt.Errorf("quick view is already generating for this table")
	}

	// 2. 从Meta获取空间元数据（通过SystemClient或直接查询meta数据库）
	// 这里需要调用Meta的API或直接查询meta_item表
	spatialMeta, err := s.getSpatialMetadataFromMeta(ctx, params.TenantID, params.ResourceID, params.SchemaName, params.TableName)
	if err != nil {
		return fmt.Errorf("failed to get spatial metadata: %w", err)
	}

	// 2.5 获取表记录数（从 pg_stat_user_tables，高性能）
	recordCount, err := s.getTableRecordCount(ctx, params.ResourceID, params.SchemaName, params.TableName)
	if err != nil {
		logger.L().Warn("⚠️  Failed to get record count, will use default MaxZoom",
			"error", err)
		recordCount = 0
	}
	spatialMeta.RecordCount = recordCount

	// 3. 计算fingerprint（使用 resource_id + schema + table 的组合）
	fingerprint := calculateFingerprint(params.ResourceID, params.SchemaName, params.TableName)

	// 5. 验证必需参数（前端必须提供用户确认的值）
	if params.MinZoom == nil {
		return fmt.Errorf("min_zoom 是必需参数（必须由用户确认）")
	}
	if params.MaxZoom == 0 {
		return fmt.Errorf("max_zoom 是必需参数（必须由用户确认）")
	}

	minZoom := *params.MinZoom
	// params.MaxZoom 直接使用，无需计算

	// 6. 设置默认并发数
	if params.Concurrency == 0 {
		params.Concurrency = 20 // 提高默认并发数（原来是10）
	}

	// 7. 创建或更新快显记录
	qv := models.QuickView{
		TenantID:            params.TenantID,
		EngineID:            params.ResourceID,
		SchemaName:          params.SchemaName,
		Table:               params.TableName,
		Status:              "generating",
		MinZoom:             &minZoom,
		MaxZoom:             params.MaxZoom,
		Fingerprint:         fingerprint,
		Extent:              spatialMeta.Extent,
		ExtentSRID:          spatialMeta.ExtentSRID,
		StopThresholdTimeMs: 300,
		StopThresholdSizeKB: 100,
	}

	// 检查是否已存在记录
	exists, _ := s.repo.Exists(params.TenantID, params.ResourceID, params.SchemaName, params.TableName)
	if exists {
		// 更新现有记录
		existingQV, err := s.repo.GetByTable(params.TenantID, params.ResourceID, params.SchemaName, params.TableName)
		if err != nil {
			return fmt.Errorf("failed to get existing quick view: %w", err)
		}
		qv.ID = existingQV.ID
		if err := s.repo.Update(&qv); err != nil {
			return fmt.Errorf("failed to update quick view: %w", err)
		}
	} else {
		// 创建新记录
		if err := s.repo.Create(&qv); err != nil {
			return fmt.Errorf("failed to create quick view: %w", err)
		}
	}

	// 8. 入队任务
	payload := worker.QuickViewTaskPayload{
		TenantID:        params.TenantID,
		EngineID:        params.ResourceID,
		SchemaName:      params.SchemaName,
		TableName:       params.TableName,
		GeomColumn:      spatialMeta.GeomColumn,
		SRID:            spatialMeta.SRID,
		PrimaryKey:      spatialMeta.PrimaryKey,
		Extent:          spatialMeta.Extent,
		MinZoom:         minZoom,
		MaxZoom:         params.MaxZoom,
		Concurrency:     params.Concurrency,
		StopThresholdMs: 300,
		StopThresholdKB: 100,
		Fingerprint:     fingerprint,
	}

	if params.Priority != "" {
		err = s.taskQueue.EnqueueQuickViewTaskWithPriority(ctx, payload, params.Priority)
	} else {
		err = s.taskQueue.EnqueueQuickViewTask(ctx, payload)
	}

	if err != nil {
		// 更新状态为失败
		s.repo.UpdateStatus(qv.ID, "failed", err.Error())
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	logger.L().Info("Quick view task enqueued",
		"engine_id", params.ResourceID,
		"table", fmt.Sprintf("%s.%s", params.SchemaName, params.TableName),
		"min_zoom", minZoom,
		"max_zoom", params.MaxZoom)

	return nil
}

// SpatialMetadataResult 空间元数据结果
type SpatialMetadataResult struct {
	GeomColumn  string
	SRID        int   // 表的原始坐标系
	ExtentSRID  int   // extent 的坐标系
	PrimaryKey  string
	Extent      []float64
	RecordCount int64 // 表记录数
}

// getSpatialMetadataFromMeta 从Meta模块获取空间元数据
func (s *QuickViewService) getSpatialMetadataFromMeta(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) (*SpatialMetadataResult, error) {
	if s.metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized, cannot query spatial metadata")
	}

	// 通过 Meta API 查询空间元数据
	spatialMeta, err := s.metaClient.GetTableSpatialMetadata(engineID, schema, table)
	if err != nil {
		return nil, fmt.Errorf("failed to get spatial metadata from Meta API: %w", err)
	}

	// 转换为 QuickViewService 内部的数据结构
	return &SpatialMetadataResult{
		GeomColumn:  spatialMeta.GeometryColumn,
		SRID:        spatialMeta.SRID,
		ExtentSRID:  spatialMeta.ExtentSRID,
		Extent:      spatialMeta.Extent,
		PrimaryKey:  spatialMeta.PrimaryKey,
		RecordCount: 0, // 初始为 0，由 getTableRecordCount 填充
	}, nil
}

// getTableRecordCount 获取表记录数
// 优先从元数据获取，如果没有则从 pg_stat_user_tables 查询（高性能，无需 COUNT）
func (s *QuickViewService) getTableRecordCount(
	ctx context.Context,
	resourceID uint,
	schema, table string,
) (int64, error) {
	// 1. 先从 SystemClient 获取资源连接信息
	if s.systemClient == nil {
		return 0, fmt.Errorf("system client not initialized")
	}

	resource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return 0, fmt.Errorf("failed to get resource: %w", err)
	}

	// 2. 构建业务数据库连接
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return 0, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. 连接业务数据库
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		return 0, fmt.Errorf("failed to connect to business database: %w", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	// 4. 从 pg_stat_user_tables 查询（性能最优，无需 COUNT）
	var recordCount int64
	query := `
		SELECT COALESCE(n_live_tup, 0)
		FROM pg_stat_user_tables
		WHERE schemaname = $1 AND relname = $2
	`
	err = db.Raw(query, schema, table).Scan(&recordCount).Error
	if err != nil {
		logger.L().Warn("Failed to get record count from pg_stat_user_tables",
			"schema", schema, "table", table, "error", err)
		return 0, err
	}

	logger.L().Info("📊 从 pg_stat_user_tables 获取记录数",
		"schema", schema, "table", table, "count", recordCount)

	return recordCount, nil
}

// GetStatus 获取快显状态
func (s *QuickViewService) GetStatus(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) (*models.QuickView, error) {
	qv, err := s.repo.GetByTable(tenantID, engineID, schema, table)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 返回默认状态
			fingerprint := calculateFingerprint(engineID, schema, table)
			return &models.QuickView{
				TenantID:    tenantID,
				EngineID:  engineID,
				SchemaName:  schema,
				Table:       table,
				Status:      "none",
				Fingerprint: fingerprint,
			}, nil
		}
		return nil, err
	}

	return qv, nil
}

// ListAll 列出所有快显任务
func (s *QuickViewService) ListAll(
	ctx context.Context,
	tenantID uint,
	params repository.ListParams,
) ([]models.QuickView, int64, error) {
	return s.repo.ListAll(tenantID, params)
}

// GetStatistics 获取统计信息
func (s *QuickViewService) GetStatistics(
	ctx context.Context,
	tenantID uint,
) (*repository.Statistics, error) {
	return s.repo.GetStatistics(tenantID)
}

// ClearQuickView 清除快显缓存
func (s *QuickViewService) ClearQuickView(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) error {
	// 1. 获取快显记录
	qv, err := s.repo.GetByTable(tenantID, engineID, schema, table)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil // 记录不存在，无需清除
		}
		return fmt.Errorf("failed to get quick view: %w", err)
	}

	// 2. TODO: 删除MinIO中的瓦片
	// 需要MinIO客户端删除 {fingerprint}/tiles/ 目录下的所有文件

	// 3. 删除数据库记录
	if err := s.repo.Delete(qv.ID); err != nil {
		return fmt.Errorf("failed to delete quick view: %w", err)
	}

	logger.L().Info("Quick view cleared",
		"engine_id", engineID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"fingerprint", qv.Fingerprint)

	return nil
}

// IncrementCachedTiles 增加缓存瓦片数（用于按需生成时更新统计）
func (s *QuickViewService) IncrementCachedTiles(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
	avgTimeMs, avgSizeKB float64,
) error {
	return s.repo.IncrementCachedTiles(tenantID, engineID, schema, table, avgTimeMs, avgSizeKB)
}

// calculateFingerprint 计算表的指纹（用于 MinIO 路径）
// 使用 common 模块的统一算法：SHA256(engineID:schema.table)
func calculateFingerprint(engineID uint, schema, table string) string {
	return commonModels.GenerateTableFingerprint(engineID, schema, table)
}
