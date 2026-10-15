package scanadapter

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type fakeContentAdapter struct {
	pathsCalled  bool
	groupsCalled bool
	paths        []string
	depth        string
	force        bool
	reporter     scanflow.ProgressReporter
}

func (a *fakeContentAdapter) ScanPaths(_ context.Context, _ *commonModels.Engine, _ uint, paths []string, depth string, force bool, reporter scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	a.pathsCalled = true
	a.reporter = reporter
	a.paths, a.depth, a.force = paths, depth, force
	return scanflow.DispatchResult{Items: 1}, nil
}

func TestNFSNodeLocatorDispatchesExactDirectoryScope(t *testing.T) {
	for _, businessPath := range [][]string{{"shp"}, {"shp", "子目录"}} {
		nodeID := uint(71)
		loc := &resourcetree.ResourceLocator{EngineID: 14, NodeID: &nodeID, Type: resourcetree.TypeDir, Path: businessPath}
		t.Run(loc.ToURI(), func(t *testing.T) {
			adapter := &fakeContentAdapter{}
			reporter := &targetProgressRecorder{}
			d := NewEngineCatalogScanDispatcher(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil, NewEngineCatalogContentScanner(nil, adapter))
			result, err := d.Dispatch(scanflow.DispatchRequest{
				Context: context.Background(), Resource: &commonModels.Engine{ID: 14, EngineType: "nfs"},
				EnginePlugin: &nfs.NFSPlugin{}, TenantID: 1, Targets: []string{loc.ToURI()}, ScanDepth: "deep", Force: true, Reporter: reporter,
			})
			if adapter.reporter != reporter {
				t.Fatal("precise directory dropped inner progress reporter")
			}
			if err != nil || result.Items != 1 {
				t.Fatalf("NFS directory dispatch: %+v %v", result, err)
			}
			path, err := resourcetree.EngineCatalogPathFromLocator(plugin.FileCatalogModel(), loc)
			if err != nil || !reflect.DeepEqual(adapter.paths, []string{path.StringPath()}) || adapter.depth != "deep" || !adapter.force || adapter.groupsCalled {
				t.Fatalf("directory scan scope changed: %+v %v", adapter, err)
			}
		})
	}
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

func (a *fakeContentAdapter) ScanLeaf(context.Context, *commonModels.Engine, uint, plugin.EngineCatalogEntry, string, bool) (scanflow.DispatchResult, error) {
	return scanflow.DispatchResult{Items: 1}, nil
}

type targetProgressRecorder struct{}

func (*targetProgressRecorder) SetTotal(int)                                     {}
func (*targetProgressRecorder) Advance(string, int, int, map[string]interface{}) {}
func (*targetProgressRecorder) Message(string)                                   {}
func (*targetProgressRecorder) Report(models.ScanProgress)                       {}
