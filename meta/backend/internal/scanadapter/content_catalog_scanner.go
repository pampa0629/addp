package scanadapter

import (
	"fmt"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type ContentCatalogAdapter interface {
	ScanPaths(resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error)
	ScanRefGroups(resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error)
}

type ContentCatalogScanner struct {
	objectAdapter ContentCatalogAdapter
	fileAdapter   ContentCatalogAdapter
}

func NewContentCatalogScanner(objectAdapter, fileAdapter ContentCatalogAdapter) *ContentCatalogScanner {
	return &ContentCatalogScanner{
		objectAdapter: objectAdapter,
		fileAdapter:   fileAdapter,
	}
}

func (s *ContentCatalogScanner) ScanObjectCatalog(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	return s.scan(s.objectAdapter, req)
}

func (s *ContentCatalogScanner) ScanFileCatalog(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	return s.scan(s.fileAdapter, req)
}

func (s *ContentCatalogScanner) scan(adapter ContentCatalogAdapter, req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if adapter == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("content catalog adapter is nil")
	}
	if len(req.RefGroups) > 0 {
		return adapter.ScanRefGroups(req.Resource, req.TenantID, req.RefGroups, req.ScanDepth, req.Force, req.Reporter)
	}
	return adapter.ScanPaths(req.Resource, req.TenantID, req.CatalogPaths, req.ScanDepth, req.Force, req.Reporter)
}
