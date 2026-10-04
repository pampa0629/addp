package scanadapter

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type EngineCatalogContentAdapter interface {
	ScanLeaf(ctx context.Context, resource *commonModels.Engine, tenantID uint, entry plugin.EngineCatalogEntry, scanDepth string, force bool) (scanflow.DispatchResult, error)
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

func (s *EngineCatalogContentScanner) ScanObjectCatalogLeaf(req scanflow.DispatchRequest, entry plugin.EngineCatalogEntry) (scanflow.DispatchResult, error) {
	return s.scanLeaf(s.objectAdapter, req, entry)
}

func (s *EngineCatalogContentScanner) ScanFileCatalogLeaf(req scanflow.DispatchRequest, entry plugin.EngineCatalogEntry) (scanflow.DispatchResult, error) {
	return s.scanLeaf(s.fileAdapter, req, entry)
}

func (s *EngineCatalogContentScanner) scanLeaf(adapter EngineCatalogContentAdapter, req scanflow.DispatchRequest, entry plugin.EngineCatalogEntry) (scanflow.DispatchResult, error) {
	if adapter == nil {
		return scanflow.DispatchResult{}, fmt.Errorf("content catalog adapter is nil")
	}
	return adapter.ScanLeaf(req.Context, req.Resource, req.TenantID, entry, req.ScanDepth, req.Force)
}
