package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/addp/common/logger"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/worker"
	"gorm.io/gorm"
)

// QuickViewService 快显服务层
type QuickViewService struct {
	repo         *repository.QuickViewRepository
	taskQueue    *worker.TaskQueue
	systemClient *commonClient.SystemClient
}

// NewQuickViewService 创建快显服务
func NewQuickViewService(
	db *gorm.DB,
	taskQueue *worker.TaskQueue,
	systemClient *commonClient.SystemClient,
) *QuickViewService {
	return &QuickViewService{
		repo:         repository.NewQuickViewRepository(db),
		taskQueue:    taskQueue,
		systemClient: systemClient,
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

	// 3. 计算fingerprint（使用 resource_id + schema + table 的组合）
	fingerprint := calculateFingerprint(params.ResourceID, params.SchemaName, params.TableName)

	// 5. 自动计算MinZoom（如果未指定）
	minZoom := 6 // 默认值
	if params.MinZoom != nil {
		minZoom = *params.MinZoom
	} else if len(spatialMeta.Extent) == 4 {
		// 使用统一的计算函数（支持不同坐标系）
		minZoom = spatial.CalculateMinZoomFromExtent(spatialMeta.Extent, spatialMeta.SRID)
	}

	// 6. 设置默认值
	if params.MaxZoom == 0 {
		params.MaxZoom = 18
	}
	if params.Concurrency == 0 {
		params.Concurrency = 20  // 提高默认并发数（原来是10）
	}

	// 7. 创建或更新快显记录
	qv := models.QuickView{
		TenantID:            params.TenantID,
		ResourceID:          params.ResourceID,
		SchemaName:          params.SchemaName,
		Table:               params.TableName,
		Status:              "generating",
		MinZoom:             &minZoom,
		MaxZoom:             params.MaxZoom,
		Fingerprint:         fingerprint,
		Extent:              spatialMeta.Extent,
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
		ResourceID:      params.ResourceID,
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
		"resource_id", params.ResourceID,
		"table", fmt.Sprintf("%s.%s", params.SchemaName, params.TableName),
		"min_zoom", minZoom,
		"max_zoom", params.MaxZoom)

	return nil
}

// SpatialMetadataResult 空间元数据结果
type SpatialMetadataResult struct {
	GeomColumn string
	SRID       int
	PrimaryKey string
	Extent     []float64
}

// getSpatialMetadataFromMeta 从Meta模块获取空间元数据
func (s *QuickViewService) getSpatialMetadataFromMeta(
	ctx context.Context,
	tenantID, resourceID uint,
	schema, table string,
) (*SpatialMetadataResult, error) {
	// 使用原生 SQL 查询 meta_item 表获取空间元数据
	query := `
		SELECT
			COALESCE(attributes->'spatial_metadata'->>'geometry_column', '') AS geometry_column,
			COALESCE((attributes->'spatial_metadata'->>'srid')::int, 0) AS srid,
			COALESCE((attributes->'spatial_metadata'->'extent')::text, '[]') AS extent,
			COALESCE(attributes->'table_metadata'->>'primary_key', '') AS primary_key,
			COALESCE((attributes->'fields')::text, '[]') AS fields_json
		FROM metadata.meta_item
		WHERE res_id = $1
			AND item_type = 'table'
			AND attributes->>'schema' = $2
			AND name = $3
			AND tenant_id = $4
			AND deleted_at IS NULL
		LIMIT 1
	`

	type QueryResult struct {
		GeometryColumn string
		SRID           int
		ExtentJSON     string
		PrimaryKey     string
		FieldsJSON     string
	}

	var result QueryResult
	// 通过 repo 访问数据库
	db := s.repo.GetDB()

	// 执行查询并手动扫描结果
	row := db.Raw(query, resourceID, schema, table, tenantID).Row()
	err := row.Scan(&result.GeometryColumn, &result.SRID, &result.ExtentJSON, &result.PrimaryKey, &result.FieldsJSON)
	if err != nil {
		logger.L().Warn("Failed to query spatial metadata from Meta, using defaults",
			"resource_id", resourceID,
			"table", fmt.Sprintf("%s.%s", schema, table),
			"error", err)

		// 降级：返回默认值
		return &SpatialMetadataResult{
			GeomColumn: "geom",
			SRID:       4326,
			PrimaryKey: "id",
			Extent:     []float64{},
		}, nil
	}

	// 调试：输出原始查询结果
	logger.L().Debug("Raw query result from Meta",
		"resource_id", resourceID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"geom_col", result.GeometryColumn,
		"srid", result.SRID,
		"primary_key", result.PrimaryKey,
		"fields_json_len", len(result.FieldsJSON))

	// 检查是否查询到结果
	if result.GeometryColumn == "" {
		logger.L().Warn("No spatial metadata found in Meta, using defaults",
			"resource_id", resourceID,
			"table", fmt.Sprintf("%s.%s", schema, table))

		return &SpatialMetadataResult{
			GeomColumn: "geom",
			SRID:       4326,
			PrimaryKey: "id",
			Extent:     []float64{},
		}, nil
	}

	// 解析 extent JSON
	var extent []float64
	if result.ExtentJSON != "" {
		if err := json.Unmarshal([]byte(result.ExtentJSON), &extent); err != nil {
			logger.L().Warn("Failed to parse extent JSON", "extent", result.ExtentJSON, "error", err)
			extent = []float64{}
		}
	}

	// 使用查询到的主键，如果为空则从字段列表中查找
	primaryKey := result.PrimaryKey
	if primaryKey == "" && result.FieldsJSON != "" {
		// 从字段列表中查找标记为主键的字段
		var fields []map[string]interface{}
		if err := json.Unmarshal([]byte(result.FieldsJSON), &fields); err == nil {
			for _, field := range fields {
				if isPK, ok := field["is_primary_key"].(bool); ok && isPK {
					if name, ok := field["name"].(string); ok {
						primaryKey = name
						break
					}
				}
			}
		}
	}

	// 如果还是没找到主键，使用默认值
	if primaryKey == "" {
		primaryKey = "id" // 默认主键名
		logger.L().Warn("No primary key found in Meta, using default",
			"resource_id", resourceID,
			"table", fmt.Sprintf("%s.%s", schema, table),
			"default_pk", primaryKey)
	}

	logger.L().Info("✅ 已从 Meta 获取空间元数据",
		"resource_id", resourceID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"geom_col", result.GeometryColumn,
		"srid", result.SRID,
		"primary_key", primaryKey,
		"extent", extent)

	return &SpatialMetadataResult{
		GeomColumn: result.GeometryColumn,
		SRID:       result.SRID,
		PrimaryKey: primaryKey,
		Extent:     extent,
	}, nil
}

// GetStatus 获取快显状态
func (s *QuickViewService) GetStatus(
	ctx context.Context,
	tenantID, resourceID uint,
	schema, table string,
) (*models.QuickView, error) {
	qv, err := s.repo.GetByTable(tenantID, resourceID, schema, table)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 返回默认状态
			fingerprint := calculateFingerprint(resourceID, schema, table)
			return &models.QuickView{
				TenantID:    tenantID,
				ResourceID:  resourceID,
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
	tenantID, resourceID uint,
	schema, table string,
) error {
	// 1. 获取快显记录
	qv, err := s.repo.GetByTable(tenantID, resourceID, schema, table)
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
		"resource_id", resourceID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"fingerprint", qv.Fingerprint)

	return nil
}

// IncrementCachedTiles 增加缓存瓦片数（用于按需生成时更新统计）
func (s *QuickViewService) IncrementCachedTiles(
	ctx context.Context,
	tenantID, resourceID uint,
	schema, table string,
	avgTimeMs, avgSizeKB float64,
) error {
	return s.repo.IncrementCachedTiles(tenantID, resourceID, schema, table, avgTimeMs, avgSizeKB)
}

// calculateFingerprint 计算表的指纹（用于 MinIO 路径）
// 使用 common 模块的统一算法：SHA256(resourceID:schema.table)
func calculateFingerprint(resourceID uint, schema, table string) string {
	return commonModels.GenerateTableFingerprint(resourceID, schema, table)
}
