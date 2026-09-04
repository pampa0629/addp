package scanadapter

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func (d *EngineCatalogScanDispatcher) clearLock(ctx context.Context, acquired bool, lockKey string, msg string, fields ...any) {
	if !acquired || lockKey == "" || d.locker == nil {
		return
	}
	if err := d.locker.ClearTask(ctx, lockKey); err != nil {
		d.log.Warn(msg, append(fields, "error", err)...)
	}
}

func (d *EngineCatalogScanDispatcher) finalizeEngineCatalogRootAfterScan(resource *commonModels.Engine, tenantID uint, items int, scanDepth string) error {
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		d.log.Warn("获取插件失败，跳过 root 扫描状态更新", "engine_type", resource.EngineType, "error", err)
		return fmt.Errorf("resolve plugin for catalog root: %w", err)
	}
	rootNode, err := metaRepo.EnsureEngineCatalogRootNode(d.repo, tenantID, resource, enginePlugin)
	if err != nil {
		d.log.Warn("同步 root 节点失败，跳过 root 扫描状态更新", "engine_id", resource.ID, "error", err)
		return fmt.Errorf("ensure catalog root: %w", err)
	}
	if err := d.repo.FinalizeNodeStateWithDepth(rootNode, "completed", items, 0, "", scanDepth); err != nil {
		d.log.Warn("更新 root 扫描状态失败", "engine_id", resource.ID, "node_id", rootNode.ID, "error", err)
		return fmt.Errorf("finalize catalog root: %w", err)
	}
	return nil
}
