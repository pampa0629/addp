package scanadapter

import (
	"context"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type fakeContentAdapter struct {
	pathsCalled  bool
	groupsCalled bool
}

func (a *fakeContentAdapter) ScanPaths(context.Context, *commonModels.Engine, uint, []string, string, bool, scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	a.pathsCalled = true
	return scanflow.DispatchResult{Items: 1}, nil
}

func (a *fakeContentAdapter) ScanRefGroups(context.Context, *commonModels.Engine, uint, []models.ScanRefGroup, string, bool, scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	a.groupsCalled = true
	return scanflow.DispatchResult{Items: 2}, nil
}

func TestContentCatalogScannerChoosesRefGroups(t *testing.T) {
	adapter := &fakeContentAdapter{}
	scanner := NewEngineCatalogContentScanner(adapter, nil)

	result, err := scanner.ScanObjectCatalog(scanflow.DispatchRequest{
		RefGroups: []models.ScanRefGroup{{Primary: "roads.shp"}},
	})
	if err != nil {
		t.Fatalf("scan object catalog: %v", err)
	}
	if !adapter.groupsCalled || adapter.pathsCalled {
		t.Fatalf("pathsCalled=%v groupsCalled=%v", adapter.pathsCalled, adapter.groupsCalled)
	}
	if result.Items != 2 {
		t.Fatalf("items = %d", result.Items)
	}
}
