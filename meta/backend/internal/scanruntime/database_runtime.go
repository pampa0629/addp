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

// DatabaseRuntime 数据库扫描运行时。
// 职责：扫描关系型数据库的 namespace、table 和 field。
type DatabaseRuntime struct {
	db           *gorm.DB
	log          *slog.Logger
	repo         *metaRepo.ScanRepository // 数据访问层
	tableIndexer TableAssetIndexer        // 索引能力
}

type databaseScanCatalog struct {
	catalogProvider plugin.CatalogProvider
	factsProvider   plugin.CatalogFactsProvider
	namespaceTerm   string
	itemTerm        string
}

// NewDatabaseRuntime 创建数据库扫描运行时。
func NewDatabaseRuntime(db *gorm.DB, log *slog.Logger, repo *metaRepo.ScanRepository, tableIndexer TableAssetIndexer) *DatabaseRuntime {
	return &DatabaseRuntime{
		db:           db,
		log:          log,
		repo:         repo,
		tableIndexer: tableIndexer,
	}
}

// ScanNamespace 扫描数据库命名空间及其所有表
//
// 职责划分：
// 1. Schema节点管理：创建/更新Schema节点，管理扫描状态
// 2. 表迭代处理：扫描所有表，判断是否需要更新
// 3. 字段扫描：深度扫描时获取表字段信息
// 4. 空间事实：消费 engine CatalogFacts 中的 spatial facts
// 5. 搜索索引：将表资产信息同步到Meilisearch
// 6. 软删除处理：清理已删除的表
//
// 参数：
//   - ctx: 上下文
//   - resource: 数据源引擎配置
//   - tenantID: 租户ID
//   - engineID: 引擎ID
//   - namespaceName: 命名空间名称
//   - scanDepth: 扫描深度 ("quick"快速扫描 | "deep"深度扫描)
//
// 返回：(schema数量, 表数量, 字段数量, error)
func (s *DatabaseRuntime) ScanNamespace(ctx context.Context, resource *commonModels.Engine, tenantID, engineID uint, namespaceName string, scanDepth string, force bool) (int, int, int, error) {
	// 1. 获取插件
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	factsProvider, ok := p.(plugin.CatalogFactsProvider)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement CatalogFactsProvider", resource.EngineType)
	}
	scanCatalog := databaseScanCatalog{
		catalogProvider: catalogProvider,
		factsProvider:   factsProvider,
		namespaceTerm:   scanflow.NamespaceTermForPlugin(p),
		itemTerm:        scanflow.CatalogLeafTermForPlugin(p, plugin.CatalogTermTable),
	}

	// 2. 创建/更新 Schema/Database 节点
	rootNode, err := metaRepo.EnsureCatalogRootNode(s.repo, tenantID, resource, p)
	if err != nil {
		return 0, 0, 0, err
	}

	schemaNode, err := s.repo.UpsertNode(tenantID, engineID, rootNode, scanCatalog.namespaceTerm, namespaceName, nil, nil)
	if err != nil {
		return 0, 0, 0, err
	}

	if err := s.repo.ResetNodeState(schemaNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	// 3. 扫描表
	tables, fields, err := s.scanTables(ctx, resource, scanCatalog, tenantID, engineID, schemaNode, namespaceName, scanDepth, force)
	if err != nil {
		s.repo.FinalizeNodeState(schemaNode, "pending", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	// 6. 完成扫描
	var totalSize int64
	tableItems, err := s.repo.GetItemsByNodeAndType(tenantID, engineID, schemaNode.ID, scanCatalog.itemTerm)
	if err != nil {
		return 0, tables, fields, err
	}
	for _, item := range tableItems {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeStateWithDepth(schemaNode, "completed", tables, totalSize, "", scanDepth); err != nil {
		return 0, tables, fields, err
	}

	return 1, tables, fields, nil
}
