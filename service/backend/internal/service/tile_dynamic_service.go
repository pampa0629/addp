package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/spatial"
	"github.com/addp/service/internal/models"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

// DynamicTileService 动态瓦片生成服务
type DynamicTileService struct {
	systemClient   *client.SystemClient
	dbPools        sync.Map // map[engineID]*gorm.DB
	sf             singleflight.Group
	protectionGate interface {
		BeginCatalogPath(context.Context, uint, plugin.EnginePlugin, plugin.EngineCatalogPath) (func(), error)
	}
}

func (s *DynamicTileService) SetProtectionGate(gate interface {
	BeginCatalogPath(context.Context, uint, plugin.EnginePlugin, plugin.EngineCatalogPath) (func(), error)
}) {
	s.protectionGate = gate
}

func (s *DynamicTileService) BeginProtectedRead(ctx context.Context, tenantID uint, layer *models.TileServiceLayer) (func(), error) {
	if s == nil || s.protectionGate == nil || s.systemClient == nil || tenantID == 0 || layer == nil {
		return nil, fmt.Errorf("Service 动态瓦片保护门禁未配置")
	}
	source := commonJSON.InterfaceMap(layer.LayerConfig["source"])
	if source == nil {
		return nil, fmt.Errorf("invalid source config")
	}
	engineID := uint(commonJSON.InterfaceInt64(source["engine_id"]))
	schema := commonJSON.InterfaceString(source["schema"])
	table := commonJSON.InterfaceString(source["table"])
	if engineID == 0 || schema == "" || table == "" {
		return nil, fmt.Errorf("dynamic tile source identity is incomplete")
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, tenantID, engineID)
	if err != nil {
		return nil, err
	}
	enginePlugin, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	modelProvider, ok := enginePlugin.(plugin.EngineCatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("dynamic tile source provider has no catalog model")
	}
	locator := &resourcetree.ResourceLocator{EngineID: engineID, Type: resourcetree.TypeTable, Path: []string{schema, table}}
	path, err := resourcetree.EngineCatalogPathFromLocator(modelProvider.EngineCatalogModel(), locator)
	if err != nil {
		return nil, err
	}
	return s.protectionGate.BeginCatalogPath(ctx, tenantID, enginePlugin, path)
}

// NewDynamicTileService 创建动态瓦片服务
func NewDynamicTileService(systemClient *client.SystemClient) *DynamicTileService {
	return &DynamicTileService{
		systemClient: systemClient,
	}
}

// GetDynamicTile 生成动态 MVT 瓦片
func (s *DynamicTileService) GetDynamicTile(
	ctx context.Context,
	tenantID uint,
	layer *models.TileServiceLayer,
	z, x, y int,
) ([]byte, error) {
	// 1. 解析图层配置
	config := layer.LayerConfig

	source := commonJSON.InterfaceMap(config["source"])
	if source == nil {
		return nil, fmt.Errorf("invalid source config")
	}

	engineID := uint(commonJSON.InterfaceInt64(source["engine_id"]))
	schema := commonJSON.InterfaceString(source["schema"])
	table := commonJSON.InterfaceString(source["table"])
	geomCol := commonJSON.InterfaceString(source["geometry_column"])
	srid := int(commonJSON.InterfaceInt64(source["srid"]))
	if engineID == 0 || schema == "" || table == "" || geomCol == "" || srid == 0 {
		return nil, fmt.Errorf("dynamic tile source config is incomplete")
	}

	// 2. 解析 MVT 配置
	extent := 4096
	buffer := 256
	if mvtConfig, ok := config["mvt"].(map[string]interface{}); ok {
		if e, ok := mvtConfig["extent"].(float64); ok {
			extent = int(e)
		}
		if b, ok := mvtConfig["buffer"].(float64); ok {
			buffer = int(b)
		}
	}

	// 3. Singleflight 防缓存击穿
	sfKey := fmt.Sprintf("%d:%s:%s:%d:%d:%d", engineID, schema, table, z, x, y)
	v, err, _ := s.sf.Do(sfKey, func() (interface{}, error) {
		return s.generateTile(ctx, tenantID, engineID, schema, table, geomCol, srid, z, x, y, extent, buffer, layer.LayerName)
	})

	if err != nil {
		return nil, err
	}

	return v.([]byte), nil
}

// generateTile 内部生成瓦片（复用 common/spatial）
func (s *DynamicTileService) generateTile(
	ctx context.Context,
	tenantID uint,
	engineID uint,
	schema, table, geomCol string,
	srid, z, x, y, extent, buffer int,
	layerName string,
) ([]byte, error) {
	// 1. 获取数据库连接池
	db, err := s.getOrCreateDBPool(ctx, tenantID, engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get db pool: %w", err)
	}

	// 2. ✅ 复用 common/spatial 工具生成 MVT
	tileData, err := spatial.GenerateMVTTileWithConfig(
		db,
		schema,
		table,
		geomCol,
		srid,
		z, x, y,
		extent,
		buffer,
		nil, // columns: 默认全部
		layerName,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to generate MVT: %w", err)
	}

	logger.L().Debug("✅ 动态瓦片生成成功",
		"engine_id", engineID,
		"schema", schema,
		"table", table,
		"z", z, "x", x, "y", y,
		"size", len(tileData))

	return tileData, nil
}

// getOrCreateDBPool 获取或创建数据库连接池
func (s *DynamicTileService) getOrCreateDBPool(ctx context.Context, tenantID, engineID uint) (*gorm.DB, error) {
	// 1. 尝试从缓存获取
	if db, ok := s.dbPools.Load(engineID); ok {
		return db.(*gorm.DB), nil
	}

	// 2. 获取引擎配置
	engine, err := s.systemClient.GetEngineForTenant(ctx, tenantID, engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine config: %w", err)
	}

	// 3. 转换为 common models
	commonEngine := &commonModels.Engine{
		ID:             engine.ID,
		EngineType:     engine.EngineType,
		ConnectionInfo: commonModels.ConnectionInfo(engine.ConnectionInfo),
	}

	// 4. 使用 dbbridge 获取或创建连接池（复用 Service 查询服务的模式）
	db, err := dbbridge.GetOrCreatePool(commonEngine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}

	// 5. 缓存连接池
	s.dbPools.Store(engineID, db)

	logger.L().Info("数据库连接池已创建",
		"engine_id", engineID,
		"engine_name", engine.Name)

	return db, nil
}

// Close 关闭所有数据库连接
func (s *DynamicTileService) Close() error {
	var errs []error
	s.dbPools.Range(func(key, value interface{}) bool {
		db := value.(*gorm.DB)
		sqlDB, err := db.DB()
		if err != nil {
			errs = append(errs, err)
			return true
		}
		if err := sqlDB.Close(); err != nil {
			errs = append(errs, err)
		}
		return true
	})

	if len(errs) > 0 {
		return fmt.Errorf("failed to close some db connections: %v", errs)
	}

	return nil
}
