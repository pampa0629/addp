package scanruntime

import (
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
)

func NewContentCatalogScanner(objectRuntime *ObjectStorageCatalogRuntime, fileRuntime *FilesystemCatalogRuntime) *scanadapter.ContentCatalogScanner {
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
	result, err := a.runtime.ScanPaths(resource, tenantID, paths, nil, scanDepth, force, reporter)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	return scanflow.DispatchResult{CatalogNodes: result.CatalogNodes, Items: result.Items, Extraction: result.Extraction}, nil
}

func (a objectCatalogAdapter) ScanRefGroups(resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	result, err := a.runtime.ScanRefGroups(resource, tenantID, groups, scanDepth, force, reporter)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	return scanflow.DispatchResult{CatalogNodes: result.CatalogNodes, Items: result.Items, Extraction: result.Extraction}, nil
}

type fileCatalogAdapter struct {
	runtime *FilesystemCatalogRuntime
}

var _ scanadapter.ContentCatalogAdapter = fileCatalogAdapter{}

func (a fileCatalogAdapter) ScanPaths(resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	roots, items, extraction, err := a.runtime.ScanPaths(resource, tenantID, paths, scanDepth, force, reporter)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	return scanflow.DispatchResult{CatalogNodes: roots, Items: items, Extraction: extraction}, nil
}

func (a fileCatalogAdapter) ScanRefGroups(resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	roots, items, extraction, err := a.runtime.ScanRefGroups(resource, tenantID, groups, scanDepth, force, reporter)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	return scanflow.DispatchResult{CatalogNodes: roots, Items: items, Extraction: extraction}, nil
}
