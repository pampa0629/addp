package service

import (
	"log/slog"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

// CatalogRootReconciler 负责维护 Meta catalog root 节点。
type CatalogRootReconciler struct {
	db  *gorm.DB
	log *slog.Logger
}

func NewCatalogRootReconciler(db *gorm.DB) *CatalogRootReconciler {
	return &CatalogRootReconciler{
		db:  db,
		log: logger.With("component", "catalog_root_reconciler"),
	}
}

func (r *CatalogRootReconciler) Reconcile(resource *commonModels.Engine) bool {
	if r == nil || r.db == nil || resource == nil || !resource.IsActive || resource.TenantID == nil {
		return false
	}
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		r.log.Debug("跳过 catalog root reconcile，插件不存在", "engine_id", resource.ID, "engine_type", resource.EngineType, "error", err)
		return false
	}
	if scanflow.CatalogModelForPlugin(enginePlugin) == nil {
		return false
	}
	if _, err := metaRepo.EnsureCatalogRootNode(metaRepo.NewScanRepository(r.db), *resource.TenantID, resource, enginePlugin); err != nil {
		r.log.Warn("同步 catalog root 失败", "engine_id", resource.ID, "engine_type", resource.EngineType, "error", err)
		return false
	}
	return true
}
