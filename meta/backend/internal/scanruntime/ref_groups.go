package scanruntime

import (
	"context"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func (s *FilesystemCatalogRuntime) ScanRefGroups(
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	metaenrich.RegisterItemResolvers()
	_ = force
	scanDepth = scanflow.ScanDepthOrDefault(scanDepth, "deep")
	return scanFileRefGroups(context.Background(), s, resource, tenantID, groups, scanDepth, reporter)
}

func (s *ObjectStorageCatalogRuntime) ScanRefGroups(
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	metaenrich.RegisterItemResolvers()
	_ = force
	scanDepth = scanflow.ScanDepthOrDefault(scanDepth, "deep")
	result, err := scanObjectRefGroups(context.Background(), s, s.repo, resource, tenantID, groups, scanDepth, reporter)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	return result, nil
}
