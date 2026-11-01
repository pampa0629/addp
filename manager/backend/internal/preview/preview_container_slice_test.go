package preview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/netcdf"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

func TestGridSliceDeniesProtectionBeforeRuntimeDiscovery(t *testing.T) {
	tenant, item := uint(7), uint(30)
	denied := errors.New("raw data protection denied")
	calls := 0
	req := &PreviewResolverRequest{Locator: &resourcetree.ResourceLocator{EngineID: 14, Type: "file", Path: []string{"grid.nc"}}, Engine: &commonModels.Engine{ID: 14, EngineType: "nfs"}, TenantID: &tenant, MetaItemID: &item, ItemFullName: "grid.nc", ItemFingerprint: commonModels.GenerateItemFingerprint(14, "grid.nc"), GridSlice: &format.GridSliceSelection{Variable: "/temperature", Indices: []int64{0}, Width: 4, Height: 3}, CheckContainerSliceRead: func(_ context.Context, path plugin.EngineCatalogPath) error {
		calls++
		if path.StringPath() != "grid.nc" {
			t.Fatal("different primary reached check")
		}
		return denied
	}}
	source := &PreviewRequest{ItemType: "file", ProviderPath: plugin.FileItemPath(14, "grid.nc"), Attributes: map[string]interface{}{"item": map[string]interface{}{"format": "netcdf", "layout": "single", "data_type": "container"}, "format_info": map[string]interface{}{"netcdf": map[string]interface{}{"source_version": "sha256:" + strings.Repeat("a", 64)}}}}
	r := &PreviewResolver{}
	if got, err := r.previewContainerSlice(context.Background(), req, source); got != nil || !errors.Is(err, denied) || calls != 1 {
		t.Fatal(got, err, calls)
	}
	req.ItemFingerprint = commonModels.GenerateItemFingerprint(14, "other.nc")
	if got, err := r.previewContainerSlice(context.Background(), req, source); got != nil || !errors.Is(err, format.ErrGridSliceSelection) || calls != 1 {
		t.Fatal("identity mismatch reached content checker", got, err, calls)
	}
	req.ItemFingerprint = commonModels.GenerateItemFingerprint(14, "grid.nc")
	source.RefPath = "external"
	if got, err := r.previewContainerSlice(context.Background(), req, source); got != nil || !errors.Is(err, format.ErrGridSliceSelection) || calls != 1 {
		t.Fatal("non-primary reference reached runtime", got, err, calls)
	}
}
