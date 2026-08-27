package scanadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

func (d *EngineCatalogScanDispatcher) dispatchBranchLeafScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	catalogNodes, items, fields, err := d.scanBranchLeaves(ctx, enginePlugin, req.Resource, req.TenantID, scanflow.TopCatalogTargets(req.CatalogPaths), req.ScanDepth, req.Force, req.Mode, req.Reporter)
	if err == nil {
		d.finalizeEngineCatalogRootAfterScan(req.Resource, req.TenantID, items, req.ScanDepth)
	}
	return scanflow.DispatchResult{CatalogNodes: catalogNodes, Items: items, Fields: fields}, err
}

func (d *EngineCatalogScanDispatcher) scanBranchLeaves(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	branchNames []string,
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
		"mode", string(mode),
		"scan_depth", scanDepth,
	}

	if len(branchNames) == 0 {
		if reporter != nil {
			reporter.Message("正在列出 catalog 分支")
		}

		rootBranchEntries, err := scanresource.RootBranchEntries(ctx, resource, enginePlugin)
		if err != nil {
			return 0, 0, 0, err
		}

		for _, entry := range rootBranchEntries {
			branchNames = append(branchNames, entry.Name)
		}

		if reporter != nil {
			reporter.Message(fmt.Sprintf("已过滤系统分支，待扫描 %d 个 catalog 分支", len(branchNames)))
		}
	}

	totalCatalogNodes := 0
	totalItems := 0
	totalFields := 0
	total := len(branchNames)
	if reporter != nil {
		reporter.SetTotal(total)
	}
	completed := 0

	for _, branchName := range branchNames {
		func(branch string) {
			if reporter != nil {
				reporter.Message(fmt.Sprintf("开始扫描 catalog 分支 %s", branch))
			}

			var branchLock string
			lockAcquired := false
			if d.locker != nil {
				branchLock = d.locker.GenerateBranchLockKey(tenantID, resourceID, branch)
				acquired, err := d.locker.TryAcquireLock(ctx, branchLock, 2*time.Hour)
				if err != nil {
					d.log.Warn("加 catalog 分支级锁失败", "branch", branch, "error", err)
				} else if !acquired {
					d.log.Info("catalog 分支正在扫描中，跳过", "engine_id", resourceID, "branch", branch)
					if reporter != nil {
						reporter.Message(fmt.Sprintf("catalog 分支 %s 正在扫描中，跳过", branch))
					}
					completed++
					return
				} else {
					lockAcquired = true
				}

				defer d.clearLock(ctx, lockAcquired, branchLock, "清除 catalog 分支级锁失败", "branch", branch)
			}

			catalogNodes, items, fields, err := d.branchScan.ScanBranch(ctx, enginePlugin, resource, tenantID, branch, scanDepth, force)
			if err != nil {
				d.log.Warn("catalog 分支扫描失败",
					"engine_id", resourceID,
					"tenant_id", tenantID,
					"branch", branch,
					"error", err,
				)
				if reporter != nil {
					reporter.Message(fmt.Sprintf("catalog 分支 %s 扫描失败: %v", branch, err))
				}
				return
			}
			totalCatalogNodes += catalogNodes
			totalItems += items
			totalFields += fields

			completed++
			if reporter != nil {
				reporter.Advance(branch, completed, total, map[string]interface{}{
					"items":  items,
					"fields": fields,
				})
			}
		}(branchName)
	}

	d.log.Info("指定 catalog 分支扫描完成", append(startFields,
		"catalog_nodes_scanned", totalCatalogNodes,
		"items_scanned", totalItems,
		"fields_scanned", totalFields,
	)...)

	return totalCatalogNodes, totalItems, totalFields, nil
}
