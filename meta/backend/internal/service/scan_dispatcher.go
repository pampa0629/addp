package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type scanDispatchMode string

const (
	scanDispatchManual scanDispatchMode = "manual"
	scanDispatchAuto   scanDispatchMode = "auto"
)

type scanDispatchRequest struct {
	Resource    *commonModels.Engine
	TenantID    uint
	Namespaces  []string
	ObjectPaths []string
	ScanDepth   string
	Force       bool
	ScanLogID   uint
	Reporter    ScanProgressReporter
	Mode        scanDispatchMode
}

type scanDispatchResult struct {
	Namespaces int
	Items      int
	Fields     int
}

type scanDispatchFunc func(context.Context, plugin.EnginePlugin, scanDispatchRequest) (scanDispatchResult, error)

func (s *ScanService) dispatchScan(req scanDispatchRequest) (scanDispatchResult, error) {
	if req.Resource == nil {
		return scanDispatchResult{}, fmt.Errorf("scan resource is nil")
	}
	enginePlugin, err := plugin.Get(req.Resource.EngineType)
	if err != nil {
		return scanDispatchResult{}, fmt.Errorf("unsupported engine type: %s", req.Resource.EngineType)
	}

	dispatch, ok := s.scanDispatchers()[storageFamily(enginePlugin)]
	if !ok {
		return scanDispatchResult{}, fmt.Errorf("plugin does not support metadata query")
	}
	return dispatch(context.Background(), enginePlugin, req)
}

func storageFamily(p plugin.EnginePlugin) string {
	if p == nil {
		return ""
	}
	caps := p.Capabilities()
	if caps.Storage == nil {
		return ""
	}
	switch caps.EngineFamily {
	case "object", "file", "tabular", "document", "graph":
		return caps.EngineFamily
	}
	return ""
}

func (s *ScanService) scanDispatchers() map[string]scanDispatchFunc {
	return map[string]scanDispatchFunc{
		"tabular":  s.dispatchTabularScan,
		"document": s.dispatchNoSQLScan,
		"graph":    s.dispatchNoSQLScan,
		"object":   s.dispatchObjectCatalogScan,
		"file":     s.dispatchFileCatalogScan,
	}
}

func (s *ScanService) dispatchNoSQLScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanDispatchRequest) (scanDispatchResult, error) {
	databaseNames := req.Namespaces
	if req.Mode == scanDispatchAuto && len(databaseNames) == 0 {
		catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
		if !ok {
			return scanDispatchResult{}, fmt.Errorf("engine %s does not implement CatalogProvider", req.Resource.EngineType)
		}
		databasesInfo, err := metacatalog.NoSQLDatabases(ctx, req.Resource, catalogProvider)
		if err != nil {
			return scanDispatchResult{}, fmt.Errorf("failed to list namespaces: %w", err)
		}
		databaseNames = make([]string, 0, len(databasesInfo))
		for _, info := range databasesInfo {
			databaseNames = append(databaseNames, info.Name)
		}
	}

	namespaces, items, fields, err := s.scanNoSQLResourceWithReporter(
		enginePlugin,
		req.Resource,
		req.TenantID,
		databaseNames,
		req.ScanDepth,
		req.Force,
		req.Reporter,
	)
	return scanDispatchResult{Namespaces: namespaces, Items: items, Fields: fields}, err
}

func (s *ScanService) dispatchObjectCatalogScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanDispatchRequest) (scanDispatchResult, error) {
	_ = ctx
	_ = enginePlugin
	namespaces, items, fields, err := s.scanObjectCatalogResourceWithReporter(
		req.Resource,
		req.TenantID,
		req.ObjectPaths,
		req.ScanDepth,
		req.Force,
		req.Reporter,
	)
	return scanDispatchResult{Namespaces: namespaces, Items: items, Fields: fields}, err
}

func (s *ScanService) dispatchFileCatalogScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanDispatchRequest) (scanDispatchResult, error) {
	paths := req.ObjectPaths
	if req.Mode == scanDispatchAuto && len(paths) == 0 {
		var err error
		paths, err = metacatalog.FileCatalogRootPaths(ctx, req.Resource, enginePlugin)
		if err != nil {
			return scanDispatchResult{}, fmt.Errorf("failed to list roots: %w", err)
		}
		if len(paths) == 0 {
			s.log.Info("文件 catalog 资源无可扫描根节点，跳过扫描")
			return scanDispatchResult{}, nil
		}
		sort.Strings(paths)
		s.log.Info("文件 catalog 资源扫描开始", "root_count", len(paths), "roots", paths)
	}

	roots, items, fields, err := s.scanFileCatalogResourceWithReporter(
		req.Resource,
		req.TenantID,
		paths,
		req.ScanDepth,
		req.Force,
		req.Reporter,
	)
	return scanDispatchResult{Namespaces: roots, Items: items, Fields: fields}, err
}

func (s *ScanService) dispatchTabularScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanDispatchRequest) (scanDispatchResult, error) {
	if req.Mode == scanDispatchManual {
		namespaces, items, fields, err := s.scanResourceSchemasWithReporter(
			req.Resource,
			req.TenantID,
			req.Namespaces,
			req.ScanLogID,
			req.ScanDepth,
			req.Force,
			req.Reporter,
		)
		return scanDispatchResult{Namespaces: namespaces, Items: items, Fields: fields}, err
	}

	schemasInfo, err := metacatalog.SchemaInfos(ctx, req.Resource, enginePlugin)
	if err != nil {
		return scanDispatchResult{}, fmt.Errorf("failed to list schemas: %w", err)
	}

	s.log.Info("数据库资源扫描开始", "schema_total", len(schemasInfo))
	scannedSchemas := make(map[string]bool)
	result := scanDispatchResult{}

	for _, schemaInfo := range schemasInfo {
		scannedSchemas[schemaInfo.Name] = true

		var node models.MetaNode
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND name = ?",
			req.TenantID, req.Resource.ID, "schema", schemaInfo.Name).First(&node).Error
		if err != gorm.ErrRecordNotFound && err != nil {
			s.log.Warn("查询 Schema 节点失败",
				"engine_id", req.Resource.ID,
				"tenant_id", req.TenantID,
				"schema", schemaInfo.Name,
				"error", err,
			)
			continue
		}

		namespaces, items, fields, err := s.dbScanService.ScanSchema(ctx, req.Resource, req.TenantID, req.Resource.ID, schemaInfo.Name, req.ScanDepth, req.Force)
		if err != nil {
			s.log.Warn("Schema 扫描失败",
				"engine_id", req.Resource.ID,
				"tenant_id", req.TenantID,
				"schema", schemaInfo.Name,
				"error", err,
			)
			continue
		}
		result.Namespaces += namespaces
		result.Items += items
		result.Fields += fields
	}

	s.softDeleteMissingTabularNamespaces(req.Resource, req.TenantID, scannedSchemas)
	return result, nil
}

func (s *ScanService) softDeleteMissingTabularNamespaces(resource *commonModels.Engine, tenantID uint, scannedSchemas map[string]bool) {
	var existingSchemas []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ?",
		tenantID, resource.ID, "schema").Find(&existingSchemas).Error; err != nil {
		s.log.Warn("查询已存在 schema 节点失败", "error", err)
		return
	}

	s.log.Info("开始检查需要清理的 schema",
		"engine_id", resource.ID,
		"existing_count", len(existingSchemas),
		"scanned_count", len(scannedSchemas),
	)
	for _, schemaNode := range existingSchemas {
		if scannedSchemas[schemaNode.Name] {
			continue
		}
		s.log.Info("Schema 已不存在，标记删除",
			"engine_id", resource.ID,
			"schema", schemaNode.Name,
		)
		if err := s.db.Delete(&schemaNode).Error; err != nil {
			s.log.Warn("软删除 schema 节点失败",
				"schema", schemaNode.Name,
				"error", err,
			)
			continue
		}
		if err := s.db.Where("node_id = ?", schemaNode.ID).Delete(&models.MetaItem{}).Error; err != nil {
			s.log.Warn("软删除 schema 下的表失败",
				"schema", schemaNode.Name,
				"error", err,
			)
		}
	}
}
