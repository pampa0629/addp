package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
)

type scanDispatchMode string

const (
	scanDispatchManual scanDispatchMode = "manual"
	scanDispatchAuto   scanDispatchMode = "auto"
)

type scanDispatchRequest struct {
	Resource     *commonModels.Engine
	TenantID     uint
	CatalogPaths []string
	ScanDepth    string
	Force        bool
	ScanLogID    uint
	Reporter     ScanProgressReporter
	Mode         scanDispatchMode
}

type scanDispatchResult struct {
	CatalogNodes int
	Items        int
	Fields       int
	Extraction   scantask.ExtractionCounts
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

	strategy, ok := catalogScanStrategyForPlugin(enginePlugin)
	if !ok {
		return scanDispatchResult{}, fmt.Errorf("plugin does not expose a supported catalog scan strategy")
	}
	dispatch, ok := s.scanDispatchers()[strategy]
	if !ok {
		return scanDispatchResult{}, fmt.Errorf("plugin does not support metadata query")
	}
	return dispatch(context.Background(), enginePlugin, req)
}

func (s *ScanService) scanDispatchers() map[catalogScanStrategy]scanDispatchFunc {
	return map[catalogScanStrategy]scanDispatchFunc{
		catalogScanTabular:        s.dispatchTabularScan,
		catalogScanNamespaceItems: s.dispatchNamespaceItemScan,
		catalogScanObject:         s.dispatchObjectCatalogScan,
		catalogScanFile:           s.dispatchFileCatalogScan,
	}
}

func (s *ScanService) dispatchNamespaceItemScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanDispatchRequest) (scanDispatchResult, error) {
	namespaceNames := topCatalogTargets(req.CatalogPaths)
	if req.Mode == scanDispatchAuto && len(namespaceNames) == 0 {
		catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
		if !ok {
			return scanDispatchResult{}, fmt.Errorf("engine %s does not implement CatalogProvider", req.Resource.EngineType)
		}
		databasesInfo, err := metacatalog.NamespaceDatabaseInfos(ctx, req.Resource, catalogProvider)
		if err != nil {
			return scanDispatchResult{}, fmt.Errorf("failed to list namespaces: %w", err)
		}
		namespaceNames = make([]string, 0, len(databasesInfo))
		for _, info := range databasesInfo {
			namespaceNames = append(namespaceNames, info.Name)
		}
	}

	namespaces, items, fields, err := s.scanNamespaceItemsWithReporter(
		enginePlugin,
		req.Resource,
		req.TenantID,
		namespaceNames,
		req.ScanDepth,
		req.Force,
		req.Reporter,
	)
	return scanDispatchResult{CatalogNodes: namespaces, Items: items, Fields: fields}, err
}

func (s *ScanService) dispatchObjectCatalogScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanDispatchRequest) (scanDispatchResult, error) {
	_ = ctx
	_ = enginePlugin
	return s.scanObjectStorageCatalogResourceResultWithReporter(
		req.Resource,
		req.TenantID,
		req.CatalogPaths,
		req.ScanDepth,
		req.Force,
		req.Reporter,
	)
}

func (s *ScanService) dispatchFileCatalogScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanDispatchRequest) (scanDispatchResult, error) {
	paths := req.CatalogPaths
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

	return s.scanFilesystemCatalogResourceResultWithReporter(
		req.Resource,
		req.TenantID,
		paths,
		req.ScanDepth,
		req.Force,
		req.Reporter,
	)
}

func (s *ScanService) dispatchTabularScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanDispatchRequest) (scanDispatchResult, error) {
	if req.Mode == scanDispatchManual {
		namespaces, items, fields, err := s.scanResourceNamespacesWithReporter(
			req.Resource,
			req.TenantID,
			topCatalogTargets(req.CatalogPaths),
			req.ScanLogID,
			req.ScanDepth,
			req.Force,
			req.Reporter,
		)
		return scanDispatchResult{CatalogNodes: namespaces, Items: items, Fields: fields}, err
	}

	namespaceInfos, err := metacatalog.NamespaceInfos(ctx, req.Resource, enginePlugin)
	if err != nil {
		return scanDispatchResult{}, fmt.Errorf("failed to list namespaces: %w", err)
	}

	namespaceTerm := namespaceTermForPlugin(enginePlugin)
	s.log.Info("数据库资源扫描开始", "namespace_total", len(namespaceInfos), "namespace_term", namespaceTerm)
	scannedNamespaces := make(map[string]bool)
	result := scanDispatchResult{}

	for _, namespaceInfo := range namespaceInfos {
		scannedNamespaces[namespaceInfo.Name] = true

		var node models.MetaNode
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND name = ?",
			req.TenantID, req.Resource.ID, namespaceTerm, namespaceInfo.Name).First(&node).Error
		if err != gorm.ErrRecordNotFound && err != nil {
			s.log.Warn("查询 namespace 节点失败",
				"engine_id", req.Resource.ID,
				"tenant_id", req.TenantID,
				"namespace", namespaceInfo.Name,
				"error", err,
			)
			continue
		}

		namespaces, items, fields, err := s.dbScanService.ScanNamespace(ctx, req.Resource, req.TenantID, req.Resource.ID, namespaceInfo.Name, req.ScanDepth, req.Force)
		if err != nil {
			s.log.Warn("namespace 扫描失败",
				"engine_id", req.Resource.ID,
				"tenant_id", req.TenantID,
				"namespace", namespaceInfo.Name,
				"error", err,
			)
			continue
		}
		result.CatalogNodes += namespaces
		result.Items += items
		result.Fields += fields
	}

	s.softDeleteMissingTabularNamespaces(req.Resource, req.TenantID, namespaceTerm, scannedNamespaces)
	return result, nil
}

func (s *ScanService) softDeleteMissingTabularNamespaces(resource *commonModels.Engine, tenantID uint, namespaceTerm string, scannedNamespaces map[string]bool) {
	var existingNamespaces []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ?",
		tenantID, resource.ID, namespaceTerm).Find(&existingNamespaces).Error; err != nil {
		s.log.Warn("查询已存在 namespace 节点失败", "namespace_term", namespaceTerm, "error", err)
		return
	}

	s.log.Info("开始检查需要清理的 namespace",
		"engine_id", resource.ID,
		"namespace_term", namespaceTerm,
		"existing_count", len(existingNamespaces),
		"scanned_count", len(scannedNamespaces),
	)
	for _, namespaceNode := range existingNamespaces {
		if scannedNamespaces[namespaceNode.Name] {
			continue
		}
		s.log.Info("namespace 已不存在，标记删除",
			"engine_id", resource.ID,
			"namespace", namespaceNode.Name,
		)
		if err := s.db.Delete(&namespaceNode).Error; err != nil {
			s.log.Warn("软删除 namespace 节点失败",
				"namespace", namespaceNode.Name,
				"error", err,
			)
			continue
		}
		if err := s.db.Where("node_id = ?", namespaceNode.ID).Delete(&models.MetaItem{}).Error; err != nil {
			s.log.Warn("软删除 namespace 下的 item 失败",
				"namespace", namespaceNode.Name,
				"error", err,
			)
		}
	}
}
