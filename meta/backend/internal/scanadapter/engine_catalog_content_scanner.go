package scanadapter

import (
	"context"
	"fmt"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type EngineCatalogContentAdapter interface {
	ScanPaths(ctx context.Context, resource *commonModels.Engine, tenantID uint, paths []string, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error)
	ScanRefGroups(ctx context.Context, resource *commonModels.Engine, tenantID uint, groups []models.ScanRefGroup, scanDepth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error)
}

type EngineCatalogContentScanner struct {
	objectAdapter EngineCatalogContentAdapter
	fileAdapter   EngineCatalogContentAdapter
}

func NewEngineCatalogContentScanner(objectAdapter, fileAdapter EngineCatalogContentAdapter) *EngineCatalogContentScanner {
	return &EngineCatalogContentScanner{
		objectAdapter: objectAdapter,
		fileAdapter:   fileAdapter,
	}
}

func (s *EngineCatalogContentScanner) ScanObjectCatalog(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	return s.scan(s.objectAdapter, req)
}

func (s *EngineCatalogContentScanner) ScanFileCatalog(req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	return s.scan(s.fileAdapter, req)
}

func (s *EngineCatalogContentScanner) scan(adapter EngineCatalogContentAdapter, req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	if adapter == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("content catalog adapter is nil")
	}
	if len(req.RefGroups) > 0 {
		return adapter.ScanRefGroups(req.Context, req.Resource, req.TenantID, req.RefGroups, req.ScanDepth, req.Force, req.Reporter)
	}
	return adapter.ScanPaths(req.Context, req.Resource, req.TenantID, req.CatalogPaths, req.ScanDepth, req.Force, req.Reporter)
}
