package scanruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

type basicGraphDepthPlugin struct {
	directLeafRuntimeTestPlugin
	model plugin.EngineCatalogModelSpec
	calls int
	fail  bool
}

func (p *basicGraphDepthPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec { return p.model }
func (p *basicGraphDepthPlugin) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.calls++
	if p.fail {
		return nil, errors.New("deep graph source unavailable")
	}
	n := int64(7)
	return &plugin.EngineCatalogFacts{Graph: &datatype.GraphInfo{Model: datatype.GraphModelPropertyGraph, NodeCount: &n, NodeShapes: []datatype.GraphNodeShapeInfo{{Name: "Person", Labels: []string{"Person"}, Properties: []datatype.FieldInfo{{Name: "secret", Type: datatype.FieldTypeString}}}}}}, nil
}

func TestGraphBasicRegistersIdentityWithoutDescribeAndPreservesDeep(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "branch-leaf", true: "direct-leaf"}[direct], func(t *testing.T) {
			db := metatest.OpenMetadataDB(t)
			repo := metaRepo.NewScanRepository(db)
			model := plugin.GraphCatalogModel()
			path := plugin.EngineCatalogBranchLeafPath(model, 228, "database", "business", "graph", "graph", "graph")
			name, fullName := "graph", "business.graph"
			if direct {
				model = plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: plugin.EngineCatalogTermService, Levels: []plugin.EngineCatalogLevelSpec{{Term: "graph", Kinds: []string{"graph"}, Role: plugin.EngineCatalogRoleLeaf}}}
				name = "/graph.opaque/"
				fullName = name
				path = plugin.EngineCatalogRootPath(model, 228)
				path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "graph", Kind: "graph", Name: name})
			}
			entry := plugin.EngineCatalogEntry{Name: name, Term: "graph", Kind: "graph", Path: path, Role: plugin.EngineCatalogRoleLeaf}
			p := &basicGraphDepthPlugin{model: model}
			resource := &commonModels.Engine{ID: 228, Name: "graph depth", EngineType: p.Type()}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			branch := NewBranchLeafRuntime(db, log, repo, nil)
			leaf := NewDirectLeafRuntime(log, repo, nil)
			scan := func(depth string) error {
				if direct {
					_, _, _, err := leaf.ScanLeaf(t.Context(), p, resource, 1, entry, depth, true)
					return err
				}
				_, _, _, err := branch.ScanLeaf(t.Context(), p, resource, 1, entry, depth, true)
				return err
			}
			load := func() *models.MetaItem {
				t.Helper()
				item, found, err := repo.FindItemByFullName(1, resource.ID, fullName)
				if err != nil || !found {
					t.Fatalf("graph identity missing: found=%v err=%v", found, err)
				}
				return item
			}
			// A deep provider that would fail cannot be invoked by a basic request.
			p.fail = true
			if err := scan("basic"); err != nil {
				t.Fatal(err)
			}
			basic := load()
			if p.calls != 0 || basic.ItemType != "graph" || basic.ScannedDepth != "basic" || basic.RowCount != nil || len(commonJSON.Section(basic.Attributes, "type_info.graph")) != 0 || commonJSON.String(basic.Attributes, "item", "data_type") != "graph" {
				t.Fatalf("basic fabricated graph facts or called Describe: calls=%d item=%+v", p.calls, basic)
			}
			p.fail = false
			if err := scan("deep"); err != nil {
				t.Fatal(err)
			}
			deep := load()
			deepFacts := commonJSON.Section(deep.Attributes, "type_info.graph")
			if p.calls != 1 || deep.ScannedDepth != "deep" || len(commonJSON.InterfaceSlice(deepFacts["node_shapes"])) != 1 {
				t.Fatalf("deep did not Describe: calls=%d attrs=%#v", p.calls, deep.Attributes)
			}
			p.fail = true
			if err := scan("basic"); err != nil {
				t.Fatal(err)
			}
			after := load()
			if p.calls != 1 || after.ID != deep.ID || after.Fingerprint != deep.Fingerprint || after.ScannedDepth != "deep" || !reflect.DeepEqual(commonJSON.Section(after.Attributes, "type_info.graph"), deepFacts) {
				t.Fatalf("basic changed deep facts or invoked Describe: calls=%d item=%+v", p.calls, after)
			}
			if err := scan("deep"); err == nil {
				t.Fatal("failed deep was accepted as basic")
			}
			failed := load()
			if p.calls != 2 || !reflect.DeepEqual(failed.Attributes, after.Attributes) {
				t.Fatal("failed deep overwrote committed facts")
			}
		})
	}
}
