package scanruntime

import (
	"context"
	"log/slog"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaenrich"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
	"gorm.io/gorm"
)

// ObjectStorageCatalogRuntime 对象存储 catalog 扫描运行时。
// 职责：按插件 catalog model 扫描 bucket/prefix/object 层级。
type ObjectStorageCatalogRuntime struct {
	db                 *gorm.DB
	log                *slog.Logger
	repo               *metaRepo.ScanRepository   // 数据访问层
	indexer            scanprocessor.AssetIndexer // 索引服务
	containerInspector metaenrich.ContainerInspector
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
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	catalogPaths, fallback []string,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	metaenrich.RegisterItemResolvers()
	result, err := scanObjectPaths(ctx, s, s.repo, resource, tenantID, catalogPaths, fallback, scanDepth, force, reporter)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	return result, nil
}
