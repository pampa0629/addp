package scanadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/scanflow"
)

func (d *EngineCatalogScanDispatcher) dispatchDirectLeafScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if len(req.CatalogPaths) > 0 || len(req.RefGroups) > 0 {
		return scanflow.DispatchResult{}, fmt.Errorf("direct leaf scan requires a typed locator target")
	}
	if d.directLeafScan == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("direct leaf catalog scanner is nil")
	}
	if d.locker != nil {
		key := d.locker.GenerateBranchLockKey(req.TenantID, req.Resource.ID, "")
		acquired, err := d.locker.TryAcquireLock(ctx, key, 2*time.Hour)
		if err != nil {
			return scanflow.DispatchResult{}, err
		}
		if !acquired {
			return scanflow.DispatchResult{}, fmt.Errorf("scan target range is already running")
		}
		defer d.clearLock(ctx, true, key, "清除扫描目标锁失败", "target", req.Resource.Name)
	}
	if req.Reporter != nil {
		req.Reporter.Message("正在扫描 catalog root 下的业务项")
	}
	items, err := d.directLeafScan.ScanRoot(ctx, enginePlugin, req.Resource, req.TenantID, req.ScanDepth, req.Force)
	if err != nil {
		return scanflow.DispatchResult{Items: items}, err
	}
	if req.Reporter != nil {
		req.Reporter.SetTotal(items)
		if items > 0 {
			req.Reporter.Advance(req.Resource.Name, items, items, map[string]interface{}{"items": items})
		} else {
			req.Reporter.Message("catalog root 下未发现业务项")
		}
	}
	return scanflow.DispatchResult{Items: items}, nil
}
