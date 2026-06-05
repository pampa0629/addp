package scanruntime

import (
	"context"
	"log/slog"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaenrich"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
	"gorm.io/gorm"
)

type ObjectCatalogScanResult struct {
	CatalogNodes int
	Items        int
	Extraction   scanflow.ExtractionCounts
}

// ObjectStorageCatalogRuntime 对象存储 catalog 扫描运行时。
// 职责：按插件 catalog model 扫描 bucket/prefix/object 层级。
type ObjectStorageCatalogRuntime struct {
	db      *gorm.DB
	log     *slog.Logger
	repo    *metaRepo.ScanRepository   // 数据访问层
	indexer scanprocessor.AssetIndexer // 索引服务
}

// NewObjectStorageCatalogRuntime 创建对象存储 catalog 扫描运行时。
func NewObjectStorageCatalogRuntime(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	indexer scanprocessor.AssetIndexer,
) *ObjectStorageCatalogRuntime {
	runtime := &ObjectStorageCatalogRuntime{
		db:      db,
		log:     log,
		repo:    repo,
		indexer: indexer,
	}
	return runtime
}

// ============================================================================
// 公共接口方法
// ============================================================================

// ScanPaths 扫描对象存储 catalog 路径。
func (s *ObjectStorageCatalogRuntime) ScanPaths(
	resource *commonModels.Engine,
	tenantID uint,
	catalogPaths, fallback []string,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (ObjectCatalogScanResult, error) {
	metaenrich.RegisterItemResolvers()
	result, err := scanadapter.ScanObjectPaths(context.Background(), s, s.repo, resource, tenantID, catalogPaths, fallback, scanDepth, force, reporter)
	if err != nil {
		return ObjectCatalogScanResult{}, err
	}
	return ObjectCatalogScanResult{CatalogNodes: result.CatalogNodes, Items: result.Items, Extraction: result.Extraction}, nil
}
