package scanruntime

import (
	"context"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
)

func NewRuntimeEngineCatalogContentScanner(objectRuntime *ObjectStorageCatalogRuntime, fileRuntime *FilesystemCatalogRuntime) *scanadapter.EngineCatalogContentScanner {
	return scanadapter.NewEngineCatalogContentScanner(
		objectCatalogAdapter{runtime: objectRuntime},
		fileCatalogAdapter{runtime: fileRuntime},
	)
}

type objectCatalogAdapter struct {
	runtime *ObjectStorageCatalogRuntime
}

var _ scanadapter.EngineCatalogContentAdapter = objectCatalogAdapter{}

func (a objectCatalogAdapter) ScanPaths(ctx context.Context, resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.runtime.ScanPaths(ctx, resource, tenantID, paths, nil, scanDepth, force, reporter)
}

func (a objectCatalogAdapter) ScanRefGroups(ctx context.Context, resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.runtime.ScanRefGroups(ctx, resource, tenantID, groups, scanDepth, force, reporter)
}

type fileCatalogAdapter struct {
	runtime *FilesystemCatalogRuntime
}

var _ scanadapter.EngineCatalogContentAdapter = fileCatalogAdapter{}

func (a fileCatalogAdapter) ScanPaths(ctx context.Context, resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.runtime.ScanPaths(ctx, resource, tenantID, paths, scanDepth, force, reporter)
}

func (a fileCatalogAdapter) ScanRefGroups(ctx context.Context, resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.runtime.ScanRefGroups(ctx, resource, tenantID, groups, scanDepth, force, reporter)
}
