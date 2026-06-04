package service

import (
	"fmt"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

type contentCatalogAdapter interface {
	LeafTerm() string
	ScanPaths(resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter ScanProgressReporter) (scanDispatchResult, error)
	ScanRefGroups(resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter ScanProgressReporter) (scanDispatchResult, error)
}

type ContentCatalogScanner struct {
	objectAdapter contentCatalogAdapter
	fileAdapter   contentCatalogAdapter
}

func NewContentCatalogScanner(objectService *ObjectStorageCatalogScanService, fileService *FilesystemCatalogScanService) *ContentCatalogScanner {
	return &ContentCatalogScanner{
		objectAdapter: objectCatalogAdapter{service: objectService},
		fileAdapter:   fileCatalogAdapter{service: fileService},
	}
}

func (s *ScanService) ensureContentCatalogScanner() *ContentCatalogScanner {
	s.contentCatalogScanner = NewContentCatalogScanner(s.objectStorageCatalogScanService, s.filesystemCatalogScanService)
	return s.contentCatalogScanner
}

func (s *ContentCatalogScanner) ScanObjectCatalog(req scanDispatchRequest) (scanDispatchResult, error) {
	return s.scan(s.objectAdapter, req)
}

func (s *ContentCatalogScanner) ScanFileCatalog(req scanDispatchRequest) (scanDispatchResult, error) {
	return s.scan(s.fileAdapter, req)
}

func (s *ContentCatalogScanner) scan(adapter contentCatalogAdapter, req scanDispatchRequest) (scanDispatchResult, error) {
	if adapter == nil {
		return scanDispatchResult{}, fmt.Errorf("content catalog adapter is nil")
	}
	if len(req.RefGroups) > 0 {
		return adapter.ScanRefGroups(req.Resource, req.TenantID, req.RefGroups, req.ScanDepth, req.Force, req.Reporter)
	}
	return adapter.ScanPaths(req.Resource, req.TenantID, req.CatalogPaths, req.ScanDepth, req.Force, req.Reporter)
}

type objectCatalogAdapter struct {
	service *ObjectStorageCatalogScanService
}

func (a objectCatalogAdapter) LeafTerm() string {
	return "object"
}

func (a objectCatalogAdapter) ScanPaths(resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter ScanProgressReporter) (scanDispatchResult, error) {
	result, err := a.service.ScanPaths(resource, tenantID, paths, nil, scanDepth, force, reporter)
	if err != nil {
		return scanDispatchResult{}, err
	}
	return scanDispatchResult{CatalogNodes: result.CatalogNodes, Items: result.Items, Extraction: result.Extraction}, nil
}

func (a objectCatalogAdapter) ScanRefGroups(resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter ScanProgressReporter) (scanDispatchResult, error) {
	result, err := a.service.ScanRefGroups(resource, tenantID, groups, scanDepth, force, reporter)
	if err != nil {
		return scanDispatchResult{}, err
	}
	return scanDispatchResult{CatalogNodes: result.CatalogNodes, Items: result.Items, Extraction: result.Extraction}, nil
}

type fileCatalogAdapter struct {
	service *FilesystemCatalogScanService
}

func (a fileCatalogAdapter) LeafTerm() string {
	return "file"
}

func (a fileCatalogAdapter) ScanPaths(resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter ScanProgressReporter) (scanDispatchResult, error) {
	roots, items, extraction, err := a.service.ScanPaths(resource, tenantID, paths, scanDepth, force, reporter)
	if err != nil {
		return scanDispatchResult{}, err
	}
	return scanDispatchResult{CatalogNodes: roots, Items: items, Extraction: extraction}, nil
}

func (a fileCatalogAdapter) ScanRefGroups(resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter ScanProgressReporter) (scanDispatchResult, error) {
	roots, items, extraction, err := a.service.ScanRefGroups(resource, tenantID, groups, scanDepth, force, reporter)
	if err != nil {
		return scanDispatchResult{}, err
	}
	return scanDispatchResult{CatalogNodes: roots, Items: items, Extraction: extraction}, nil
}
