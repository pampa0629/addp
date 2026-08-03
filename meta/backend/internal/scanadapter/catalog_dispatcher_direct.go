package scanadapter

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/scanflow"
)

func (d *CatalogDispatcher) dispatchDirectLeafScan(ctx context.Context, enginePlugin plugin.EnginePlugin, req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if d.directLeafScan == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("direct leaf catalog scanner is nil")
	}
	if req.Reporter != nil {
		req.Reporter.Message("正在扫描 catalog root 下的业务项")
	}
	items, err := d.directLeafScan.ScanRoot(ctx, enginePlugin, req.Resource, req.TenantID, req.ScanDepth, req.Force)
	if err != nil {
		return scanflow.DispatchResult{}, err
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
