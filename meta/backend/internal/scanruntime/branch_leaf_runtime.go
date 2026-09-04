package scanruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

// BranchLeafRuntime 扫描 root branch -> catalog leaf，并把 leaf 投影为 Meta item。
// 动态 schema 与图型引擎共享这一层级，但 leaf 事实仍由插件和 catalog model 决定。
type BranchLeafRuntime struct {
	db   *gorm.DB
	log  *slog.Logger
	repo *metaRepo.ScanRepository // 数据访问层
}

type branchLeafScanCatalog struct {
	model            plugin.EngineCatalogModelSpec
	catalogProvider  plugin.EngineCatalogProvider
	factsProvider    plugin.EngineCatalogFactsProvider
	samplingProvider plugin.DynamicSchemaSamplingProvider
	connInfo         plugin.ConnectionInfo
	branchTerm       string
}

func NewBranchLeafRuntime(db *gorm.DB, log *slog.Logger, repo *metaRepo.ScanRepository) *BranchLeafRuntime {
	return &BranchLeafRuntime{
		db:   db,
		log:  log,
		repo: repo,
	}
}

// ScanBranch 扫描 root branch 及其所有 catalog leaf。
// EngineCatalogProvider 负责列出真实数据库、集合或 graph leaf；DynamicSchemaSamplingProvider 用于动态 schema 深度推断。
func (s *BranchLeafRuntime) ScanBranch(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	branchName string,
	scanDepth string,
	force bool,
) (int, int, int, error) {

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	catalogProvider, ok := enginePlugin.(plugin.EngineCatalogProvider)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement EngineCatalogProvider", resource.EngineType)
	}
	samplingProvider, _ := enginePlugin.(plugin.DynamicSchemaSamplingProvider)
	catalogFactsProvider, _ := enginePlugin.(plugin.EngineCatalogFactsProvider)
	model := scanflow.EngineCatalogModelForPlugin(enginePlugin)
	if model == nil {
		return 0, 0, 0, fmt.Errorf("engine %s has no catalog model", resource.EngineType)
	}
	scanCatalog := branchLeafScanCatalog{
		model:            *model,
		catalogProvider:  catalogProvider,
		factsProvider:    catalogFactsProvider,
		samplingProvider: samplingProvider,
		connInfo:         connInfo,
		branchTerm:       scanflow.FirstBusinessBranchTermForPlugin(enginePlugin),
	}

	// 1. 创建/更新 root branch 节点
	rootNode, err := metaRepo.EnsureEngineCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		return 0, 0, 0, err
	}
	branchNode, err := s.repo.UpsertNode(tenantID, resource.ID, rootNode, scanCatalog.branchTerm, branchName, nil, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create catalog branch node: %w", err)
	}

	if err := s.repo.ResetNodeState(branchNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	var totalObjects, totalFields int

	totalObjects, totalFields, err = s.scanCatalogLeaves(ctx, scanCatalog, resource, tenantID, branchNode, branchName, scanDepth, force)

	if err != nil {
		_ = s.repo.FinalizeNodeState(branchNode, "failed", totalObjects, 0, err.Error())
		return 0, totalObjects, totalFields, err
	}

	// 3. 完成扫描
	var totalSize int64
	collectionItems, err := s.repo.GetItemsByNode(branchNode.ID)
	if err != nil {
		return 0, totalObjects, totalFields, err
	}
	for _, item := range collectionItems {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeStateWithDepth(branchNode, "completed", totalObjects, totalSize, "", scanDepth); err != nil {
		return 0, totalObjects, totalFields, err
	}

	return 1, totalObjects, totalFields, nil
}
