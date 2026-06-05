package scanruntime

import (
	"context"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
)

func (s *FilesystemCatalogRuntime) ScanRefGroups(
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (int, int, scanflow.ExtractionCounts, error) {
	metaenrich.RegisterItemResolvers()
	_ = force
	scanDepth = scanflow.ScanDepthOrDefault(scanDepth, "deep")
	return scanadapter.ScanFileRefGroups(context.Background(), s, resource, tenantID, groups, scanDepth, reporter)
}

func (s *ObjectStorageCatalogRuntime) ScanRefGroups(
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (ObjectCatalogScanResult, error) {
	metaenrich.RegisterItemResolvers()
	_ = force
	scanDepth = scanflow.ScanDepthOrDefault(scanDepth, "deep")
	result, err := scanadapter.ScanObjectRefGroups(context.Background(), s, s.repo, resource, tenantID, groups, scanDepth, reporter)
	if err != nil {
		return ObjectCatalogScanResult{}, err
	}
	return ObjectCatalogScanResult{CatalogNodes: result.CatalogNodes, Items: result.Items, Extraction: result.Extraction}, nil
}
