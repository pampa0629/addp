package service

import (
	"context"
	"fmt"

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
		namespaceEntries, err := metacatalog.NamespaceEntries(ctx, req.Resource, catalogProvider)
		if err != nil {
			return scanDispatchResult{}, fmt.Errorf("failed to list namespaces: %w", err)
		}
		namespaceNames = make([]string, 0, len(namespaceEntries))
		for _, entry := range namespaceEntries {
			namespaceNames = append(namespaceNames, entry.Name)
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
	if err == nil {
		s.finalizeCatalogRootAfterScan(req.Resource, req.TenantID, items, req.ScanDepth)
	}
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
	_ = ctx
	paths := req.CatalogPaths
	if req.Mode == scanDispatchAuto && len(paths) == 0 {
		paths = []string{""}
		s.log.Info("文件 catalog 资源从结构 root 开始扫描")
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

	namespaceEntries, err := metacatalog.NamespaceEntriesForPlugin(ctx, req.Resource, enginePlugin)
	if err != nil {
		return scanDispatchResult{}, fmt.Errorf("failed to list namespaces: %w", err)
	}

	namespaceTerm := namespaceTermForPlugin(enginePlugin)
	s.log.Info("数据库资源扫描开始", "namespace_total", len(namespaceEntries), "namespace_term", namespaceTerm)
	scannedNamespaces := make(map[string]bool)
	result := scanDispatchResult{}

	for _, namespaceEntry := range namespaceEntries {
		scannedNamespaces[namespaceEntry.Name] = true

		var node models.MetaNode
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND name = ?",
			req.TenantID, req.Resource.ID, namespaceTerm, namespaceEntry.Name).First(&node).Error
		if err != gorm.ErrRecordNotFound && err != nil {
			s.log.Warn("查询 namespace 节点失败",
				"engine_id", req.Resource.ID,
				"tenant_id", req.TenantID,
				"namespace", namespaceEntry.Name,
				"error", err,
			)
			continue
		}

		namespaces, items, fields, err := s.dbScanService.ScanNamespace(ctx, req.Resource, req.TenantID, req.Resource.ID, namespaceEntry.Name, req.ScanDepth, req.Force)
		if err != nil {
			s.log.Warn("namespace 扫描失败",
				"engine_id", req.Resource.ID,
				"tenant_id", req.TenantID,
				"namespace", namespaceEntry.Name,
				"error", err,
			)
			continue
		}
		result.CatalogNodes += namespaces
		result.Items += items
		result.Fields += fields
	}

	s.softDeleteMissingTabularNamespaces(req.Resource, req.TenantID, namespaceTerm, scannedNamespaces)
	s.finalizeCatalogRootAfterScan(req.Resource, req.TenantID, result.Items, req.ScanDepth)
	return result, nil
}

func (s *ScanService) finalizeCatalogRootAfterScan(resource *commonModels.Engine, tenantID uint, items int, scanDepth string) {
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		s.log.Warn("获取插件失败，跳过 root 扫描状态更新", "engine_type", resource.EngineType, "error", err)
		return
	}
	rootNode, err := ensureCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		s.log.Warn("同步 root 节点失败，跳过 root 扫描状态更新", "engine_id", resource.ID, "error", err)
		return
	}
	if err := s.repo.FinalizeNodeStateWithDepth(rootNode, "completed", items, 0, "", scanDepth); err != nil {
		s.log.Warn("更新 root 扫描状态失败", "engine_id", resource.ID, "node_id", rootNode.ID, "error", err)
	}
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
