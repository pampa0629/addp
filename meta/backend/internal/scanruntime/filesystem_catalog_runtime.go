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

// FilesystemCatalogRuntime 文件系统 catalog 扫描运行时。
// 职责：通过 EngineCatalogProvider 扫描文件系统语义存储，并使用 ContentReadableProvider 读取内容识别复合数据项。
type FilesystemCatalogRuntime struct {
	db                 *gorm.DB
	log                *slog.Logger
	repo               *metaRepo.ScanRepository
	indexer            scanprocessor.AssetIndexer
	cadInspector       metaenrich.CADInspector
	containerInspector metaenrich.ContainerInspector
}

// NewFilesystemCatalogRuntime 创建文件系统 catalog 扫描运行时。
func NewFilesystemCatalogRuntime(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	indexer scanprocessor.AssetIndexer,
) *FilesystemCatalogRuntime {
	return &FilesystemCatalogRuntime{
		db:      db,
		log:     log,
		repo:    repo,
		indexer: indexer,
	}
}

// ScanPaths 扫描文件系统 catalog 路径，识别复合数据项。
func (s *FilesystemCatalogRuntime) ScanPaths(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID uint,
	paths []string,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	metaenrich.RegisterItemResolvers()
	result, err := scanFilePaths(ctx, s, s.repo, resource, tenantID, paths, scanDepth, force, reporter)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	return result, nil
}
