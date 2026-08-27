package service

import (
	"log/slog"

	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

// EngineCatalogRootReconciler 负责维护 Meta catalog root 节点。
type EngineCatalogRootReconciler struct {
	db  *gorm.DB
	log *slog.Logger
}

func NewEngineCatalogRootReconciler(db *gorm.DB) *EngineCatalogRootReconciler {
	return &EngineCatalogRootReconciler{
		db:  db,
		log: logger.With("component", "catalog_root_reconciler"),
	}
}

func (r *EngineCatalogRootReconciler) Reconcile(resource *commonModels.Engine) bool {
	if r == nil || r.db == nil || !engineselection.IsSelectionOption(resource) || resource.TenantID == nil {
		return false
	}
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		r.log.Debug("跳过 catalog root reconcile，插件不存在", "engine_id", resource.ID, "engine_type", resource.EngineType, "error", err)
		return false
	}
	if scanflow.EngineCatalogModelForPlugin(enginePlugin) == nil {
		return false
	}
	if _, err := metaRepo.EnsureEngineCatalogRootNode(metaRepo.NewScanRepository(r.db), *resource.TenantID, resource, enginePlugin); err != nil {
		r.log.Warn("同步 catalog root 失败", "engine_id", resource.ID, "engine_type", resource.EngineType, "error", err)
		return false
	}
	return true
}
