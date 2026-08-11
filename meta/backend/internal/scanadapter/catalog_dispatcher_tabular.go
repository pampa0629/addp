package scanadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func (d *CatalogDispatcher) dispatchTabularScan(ctx context.Context, enginePlugin plugin.EnginePlugin, plan scanflow.CatalogScanPlan, req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if d.namespaceScan == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("namespace scanner is nil")
	}

	namespaces := scanflow.TopCatalogTargets(req.CatalogPaths)
	fullEngineScan := len(namespaces) == 0
	visibleNamespaces := make(map[string]bool)
	if fullEngineScan {
		if req.Reporter != nil {
			req.Reporter.Message("未指定命名空间，正在获取完整列表")
		}
		rootBranchEntries, err := metacatalog.RootBranchEntries(ctx, req.Resource, enginePlugin)
		if err != nil {
			return scanflow.DispatchResult{}, fmt.Errorf("failed to list root branch entries: %w", err)
		}
		for _, entry := range rootBranchEntries {
			namespaces = append(namespaces, entry.Name)
			visibleNamespaces[entry.Name] = true
		}
		if req.Reporter != nil {
			req.Reporter.Message(fmt.Sprintf("已过滤系统命名空间，待扫描 %d 个用户命名空间", len(namespaces)))
		}
	}

	catalogNodes, items, fields, err := d.scanResourceNamespaces(ctx, enginePlugin, req.Resource, req.TenantID, namespaces, req.ScanLogID, req.ScanDepth, req.Force, req.Mode, req.Reporter)
	result := scanflow.DispatchResult{CatalogNodes: catalogNodes, Items: items, Fields: fields}
	if err != nil {
		return result, err
	}
	if fullEngineScan {
		d.softDeleteMissingTabularNamespaces(req.Resource, req.TenantID, plan.BranchTerm, visibleNamespaces)
		d.finalizeCatalogRootAfterScan(req.Resource, req.TenantID, result.Items, req.ScanDepth)
	}
	return result, nil
}

func (d *CatalogDispatcher) scanResourceNamespaces(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	namespaces []string,
	scanLogID uint,
	scanDepth string,
	force bool,
	mode scanflow.DispatchMode,
	reporter scanflow.ProgressReporter,
) (int, int, int, error) {
	resourceID := resource.ID

	startFields := []any{
		"engine_id", resource.ID,
		"tenant_id", tenantID,
		"resource_name", resource.Name,
		"resource_type", resource.EngineType,
		"scan_log_id", scanLogID,
		"mode", string(mode),
	}
	if len(namespaces) > 0 {
		startFields = append(startFields, "target_namespaces", namespaces)
	}
	d.log.Info("开始扫描指定命名空间列表", startFields...)

	totalNamespaces := 0
	totalTables := 0
	totalFields := 0
	total := len(namespaces)
	var scanErrors []error
	if reporter != nil {
		reporter.SetTotal(total)
	}
	completed := 0

	for _, namespaceName := range namespaces {
		func(namespace string) {
			if reporter != nil {
				reporter.Message(fmt.Sprintf("开始扫描命名空间 %s", namespace))
			}

			var namespaceLock string
			lockAcquired := false
			if d.locker != nil {
				namespaceLock = d.locker.GenerateNamespaceLockKey(tenantID, resourceID, namespace)
				acquired, err := d.locker.TryAcquireLock(ctx, namespaceLock, 2*time.Hour)
				if err != nil {
					d.log.Warn("加命名空间级锁失败", "namespace", namespace, "error", err)
				} else if !acquired {
					d.log.Info("命名空间正在扫描中，跳过", "engine_id", resourceID, "namespace", namespace)
					if reporter != nil {
						reporter.Message(fmt.Sprintf("命名空间 %s 正在扫描中，跳过", namespace))
					}
					completed++
					return
				} else {
					lockAcquired = true
				}

				defer d.clearLock(ctx, lockAcquired, namespaceLock, "清除命名空间级锁失败", "namespace", namespace)
			}

			schemas, tables, fields, err := d.namespaceScan.ScanNamespace(ctx, enginePlugin, resource, tenantID, resourceID, namespace, scanDepth, force)
			if err != nil {
				d.log.Warn("命名空间扫描失败",
					"engine_id", resourceID,
					"tenant_id", tenantID,
					"namespace", namespace,
					"error", err,
				)
				if reporter != nil {
					reporter.Message(fmt.Sprintf("命名空间 %s 扫描失败: %v", namespace, err))
				}
				scanErrors = append(scanErrors, fmt.Errorf("%s: %w", namespace, err))
				return
			}
			totalNamespaces += schemas
			totalTables += tables
			totalFields += fields

			completed++
			if reporter != nil {
				reporter.Advance(namespace, completed, total, map[string]interface{}{
					"tables": tables,
					"fields": fields,
				})
			}
		}(namespaceName)
	}

	d.log.Info("指定命名空间扫描完成", append(startFields,
		"catalog_nodes_scanned", totalNamespaces,
		"items_scanned", totalTables,
		"fields_scanned", totalFields,
	)...)

	if len(scanErrors) > 0 {
		return totalNamespaces, totalTables, totalFields, fmt.Errorf("failed to scan %d namespace(s): %v", len(scanErrors), scanErrors[0])
	}

	return totalNamespaces, totalTables, totalFields, nil
}

func (d *CatalogDispatcher) softDeleteMissingTabularNamespaces(resource *commonModels.Engine, tenantID uint, namespaceTerm string, scannedNamespaces map[string]bool) {
	var existingNamespaces []models.MetaNode
	if err := d.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ?",
		tenantID, resource.ID, namespaceTerm).Find(&existingNamespaces).Error; err != nil {
		d.log.Warn("查询已存在 namespace 节点失败", "namespace_term", namespaceTerm, "error", err)
		return
	}

	d.log.Info("开始检查需要清理的 namespace",
		"engine_id", resource.ID,
		"namespace_term", namespaceTerm,
		"existing_count", len(existingNamespaces),
		"scanned_count", len(scannedNamespaces),
	)
	for _, namespaceNode := range existingNamespaces {
		if scannedNamespaces[namespaceNode.Name] {
			continue
		}
		d.log.Info("namespace 已不存在，标记删除", "engine_id", resource.ID, "namespace", namespaceNode.Name)
		if err := d.db.Delete(&namespaceNode).Error; err != nil {
			d.log.Warn("软删除 namespace 节点失败", "namespace", namespaceNode.Name, "error", err)
			continue
		}
		if err := d.db.Where("node_id = ?", namespaceNode.ID).Delete(&models.MetaItem{}).Error; err != nil {
			d.log.Warn("软删除 namespace 下的 item 失败", "namespace", namespaceNode.Name, "error", err)
		}
	}
}
