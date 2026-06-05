package scanruntime

import (
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
)

func NewRuntimeContentCatalogScanner(objectRuntime *ObjectStorageCatalogRuntime, fileRuntime *FilesystemCatalogRuntime) *scanadapter.ContentCatalogScanner {
	return scanadapter.NewContentCatalogScanner(
		objectCatalogAdapter{runtime: objectRuntime},
		fileCatalogAdapter{runtime: fileRuntime},
	)
}

type objectCatalogAdapter struct {
	runtime *ObjectStorageCatalogRuntime
}

var _ scanadapter.ContentCatalogAdapter = objectCatalogAdapter{}

func (a objectCatalogAdapter) ScanPaths(resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.runtime.ScanPaths(resource, tenantID, paths, nil, scanDepth, force, reporter)
}

func (a objectCatalogAdapter) ScanRefGroups(resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.runtime.ScanRefGroups(resource, tenantID, groups, scanDepth, force, reporter)
}

type fileCatalogAdapter struct {
	runtime *FilesystemCatalogRuntime
}

var _ scanadapter.ContentCatalogAdapter = fileCatalogAdapter{}

func (a fileCatalogAdapter) ScanPaths(resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.runtime.ScanPaths(resource, tenantID, paths, scanDepth, force, reporter)
}

func (a fileCatalogAdapter) ScanRefGroups(resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.runtime.ScanRefGroups(resource, tenantID, groups, scanDepth, force, reporter)
}
