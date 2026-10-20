package service

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type depthRefreshPlugin struct {
	refreshContentReader
	model       plugin.EngineCatalogModelSpec
	entry       plugin.EngineCatalogEntry
	resolveMode string
	calls       []string
}

func (p *depthRefreshPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec { return p.model }
func (p *depthRefreshPlugin) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.calls = append(p.calls, "list")
	return nil, errors.New("unexpected sibling enumeration")
}
func (p *depthRefreshPlugin) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	p.calls = append(p.calls, "resolve:"+path.StringPath())
	if p.resolveMode == "error" {
		return nil, errors.New("source unavailable")
	}
	if p.resolveMode == "missing" {
		return nil, nil
	}
	entry := p.entry
	if p.resolveMode == "wrong-path" {
		entry.Path = plugin.TabularItemPath(path.EngineID, "schema", "other", "sibling")
	}
	if p.resolveMode == "wrong-role" {
		entry.Role = plugin.EngineCatalogRoleBranch
	}
	if p.resolveMode == "wrong-name" {
		entry.Name = "sibling"
	}
	return &entry, nil
}
func (p *depthRefreshPlugin) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.calls = append(p.calls, "describe")
	return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Name: "source", Fields: []datatype.FieldInfo{{Name: "fresh", Type: datatype.FieldTypeString}}}}, nil
}
func (p *depthRefreshPlugin) SampleDynamicSchema(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.calls = append(p.calls, "sample")
	return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Name: "source", Fields: []datatype.FieldInfo{{Name: "sampled", Type: datatype.FieldTypeString}}}}, nil
}
func (p *depthRefreshPlugin) OpenContent(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	p.calls = append(p.calls, "content")
	return p.refreshContentReader.OpenContent(ctx, conn, path, opts)
}
func (p *depthRefreshPlugin) OpenRange(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	p.calls = append(p.calls, "range")
	return nil, errors.New("unexpected content range")
}

func TestKnownItemSelectorHonorsBasicAndDeep(t *testing.T) {
	for _, kind := range []string{"table", "collection", "topic", "object", "file"} {
		for _, existingDeep := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/new", true: "/after-deep"}[existingDeep], func(t *testing.T) {
				svc, p, item := setupDepthRefresh(t, kind, existingDeep)
				result, err := svc.ScanEngineWithOptions(scanflow.Options{EngineID: item.EngineID, TenantID: item.TenantID, ItemID: item.ID, ScanDepth: "basic", Force: true})
				if err != nil {
					t.Fatal(err)
				}
				if result.ItemsScanned != 1 || result.FieldsScanned != 0 {
					t.Fatalf("basic counts=%+v", result)
				}
				want := []string{}
				if kind != "object" && kind != "file" {
					want = []string{"resolve:" + p.entry.Path.StringPath()}
				}
				if !reflect.DeepEqual(p.calls, want) {
					t.Fatalf("basic source IO=%v want %v", p.calls, want)
				}
				var actual models.MetaItem
				if err := svc.db.First(&actual, item.ID).Error; err != nil {
					t.Fatal(err)
				}
				wantDepth := "basic"
				if existingDeep {
					wantDepth = "deep"
				}
				if actual.ID != item.ID || actual.Fingerprint != item.Fingerprint || actual.NodeID != item.NodeID || actual.ScannedDepth != wantDepth {
					t.Fatalf("identity/depth changed: %+v", actual)
				}
				fields := commonJSON.InterfaceSlice(commonJSON.Section(actual.Attributes, "type_info.table")["fields"])
				if existingDeep && (len(fields) != 1 || commonJSON.InterfaceString(commonJSON.InterfaceMap(fields[0])["name"]) != "preserved") {
					t.Fatalf("deep fields lost: %#v", fields)
				}
				if !existingDeep && len(fields) != 0 {
					t.Fatalf("basic promoted entry fields: %#v", fields)
				}
				if commonJSON.String(actual.Attributes, "storage", "content_hash") != "existing-hash" {
					t.Fatal("basic changed content hash")
				}
				var count int64
				svc.db.Model(&models.MetaItem{}).Count(&count)
				if count != 1 {
					t.Fatalf("extra items: %d", count)
				}
			})
		}
	}
	for _, kind := range []string{"table", "collection"} {
		t.Run(kind+"/deep", func(t *testing.T) {
			svc, p, item := setupDepthRefresh(t, kind, false)
			result, err := svc.ScanEngineWithOptions(scanflow.Options{EngineID: item.EngineID, TenantID: item.TenantID, ItemID: item.ID, ScanDepth: "deep", Force: true})
			if err != nil {
				t.Fatal(err)
			}
			want := "describe"
			if kind == "collection" {
				want = "sample"
			}
			if !reflect.DeepEqual(p.calls, []string{want}) || result.FieldsScanned != 1 {
				t.Fatalf("deep IO/count=%v/%+v", p.calls, result)
			}
		})
	}
}

// Preserve the actual optional capabilities: a catalog-only engine must not
// acquire ContentReadable/Describe/Sampling merely because it can run basic.
type catalogOnlyDepthPlugin struct {
	plugin.EnginePlugin
	catalog plugin.EngineCatalogProvider
	model   plugin.EngineCatalogModelSpec
}

func (p catalogOnlyDepthPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec { return p.model }
func (p catalogOnlyDepthPlugin) ResolvePath(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return p.catalog.ResolvePath(ctx, conn, path)
}
func (p catalogOnlyDepthPlugin) ListChildren(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return p.catalog.ListChildren(ctx, conn, path, opts)
}
func TestKnownBasicNeedsNoDeepProviderCapabilities(t *testing.T) {
	for _, kind := range []string{"table", "collection", "topic", "file", "object"} {
		t.Run(kind, func(t *testing.T) {
			svc, p, item := setupDepthRefresh(t, kind, false)
			plugin.Register(catalogOnlyDepthPlugin{EnginePlugin: p, catalog: p, model: p.model})
			opts := scanflow.Options{EngineID: item.EngineID, TenantID: item.TenantID, ItemID: item.ID, ScanDepth: "basic"}
			if _, err := svc.ScanEngineWithOptions(opts); err != nil {
				t.Fatalf("basic required a deep capability: %v", err)
			}
			before := len(p.calls)
			opts.ScanDepth = "deep"
			if _, err := svc.ScanEngineWithOptions(opts); err == nil {
				t.Fatal("unsupported deep silently accepted")
			}
			if len(p.calls) != before {
				t.Fatalf("unsupported deep performed source IO: %v", p.calls)
			}
		})
	}
}

func TestBasicKnownItemRejectsUnresolvedOrDifferentCatalogLeaf(t *testing.T) {
	for _, mode := range []string{"error", "missing", "wrong-path", "wrong-name", "wrong-role"} {
		t.Run(mode, func(t *testing.T) {
			svc, p, item := setupDepthRefresh(t, "table", true)
			p.resolveMode = mode
			_, err := svc.ScanEngineWithOptions(scanflow.Options{EngineID: item.EngineID, TenantID: item.TenantID, ItemID: item.ID, ScanDepth: "basic", Force: true})
			if err == nil {
				t.Fatal("unresolved or changed source accepted")
			}
			var actual models.MetaItem
			if err := svc.db.First(&actual, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if actual.ScannedAt != nil || !reflect.DeepEqual(actual.Attributes, item.Attributes) {
				t.Fatal("failed basic changed persistent facts")
			}
			if !reflect.DeepEqual(p.calls, []string{"resolve:" + p.entry.Path.StringPath()}) {
				t.Fatalf("unexpected IO: %v", p.calls)
			}
		})
	}
}

func TestKnownItemRefreshRejectsCrossEngineParentBeforeSourceIO(t *testing.T) {
	for _, depth := range []string{"basic", "deep"} {
		t.Run(depth, func(t *testing.T) {
			svc, p, item := setupDepthRefresh(t, "table", false)
			if err := svc.db.Model(&models.MetaNode{}).Where("id = ?", item.NodeID).Update("engine_id", item.EngineID+1).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ScanEngineWithOptions(scanflow.Options{EngineID: item.EngineID, TenantID: item.TenantID, ItemID: item.ID, ScanDepth: depth}); err == nil {
				t.Fatal("cross-engine parent accepted")
			}
			if len(p.calls) != 0 {
				t.Fatalf("source IO before parent binding check: %v", p.calls)
			}
		})
	}
}

func TestBasicKnownItemPreservesLiteralNamespaceNames(t *testing.T) {
	for _, kind := range []string{"table", "collection"} {
		t.Run(kind, func(t *testing.T) {
			svc, p, item := setupDepthRefresh(t, kind, false)
			namespace := " department.v2 "
			if err := svc.db.Model(&models.MetaNode{}).Where("id = ?", item.NodeID).Updates(map[string]interface{}{"name": namespace, "full_name": "display-only"}).Error; err != nil {
				t.Fatal(err)
			}
			p.entry.Path.Segments[1].Name = namespace
			if _, err := svc.ScanEngineWithOptions(scanflow.Options{EngineID: item.EngineID, TenantID: item.TenantID, ItemID: item.ID, ScanDepth: "basic"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.calls, []string{"resolve:" + p.entry.Path.StringPath()}) {
				t.Fatalf("literal namespace changed: %v", p.calls)
			}
		})
	}
}

func setupDepthRefresh(t *testing.T, kind string, deep bool) (*ScanService, *depthRefreshPlugin, models.MetaItem) {
	t.Helper()
	db := openObjectCatalogScanTestDB(t)
	tenantID, engineID := uint(1), uint(187)
	name, parentName, parentType := "source", "public", "schema"
	model := plugin.TabularCatalogModel("schema")
	path := plugin.TabularItemPath(engineID, "schema", parentName, name)
	switch kind {
	case "collection":
		model = plugin.DynamicSchemaCatalogModel()
		parentType = "database"
		path = plugin.EngineCatalogBranchLeafPath(model, engineID, "database", parentName, "collection", "collection", name)
	case "topic":
		model = plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: plugin.EngineCatalogTermService, Levels: []plugin.EngineCatalogLevelSpec{{Role: plugin.EngineCatalogRoleLeaf, Term: "topic", Kinds: []string{"topic"}}}}
		parentType = plugin.EngineCatalogTermService
		parentName = ""
		path = plugin.EngineCatalogRootPath(model, engineID)
		path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "topic", Kind: "topic", Name: name})
	case "object":
		name = "source.pdf"
		parentType = "bucket"
		parentName = "docs"
		model = plugin.ObjectCatalogModel()
		path = plugin.ObjectItemPath(engineID, "docs", name)
	case "file":
		name = "source.pdf"
		parentType = "directory"
		parentName = "docs"
		model = plugin.FileCatalogModel()
		path = plugin.FileItemPath(engineID, "docs/"+name)
	}
	n := int64(21)
	size := int64(123)
	p := &depthRefreshPlugin{refreshContentReader: refreshContentReader{engineType: "depth-" + t.Name(), content: map[string][]byte{}}, model: model, calls: []string{}, entry: plugin.EngineCatalogEntry{Name: name, Path: path, Role: plugin.EngineCatalogRoleLeaf, Kind: kind, Table: &datatype.TableInfo{RowCount: &n, SizeBytes: &size, Fields: []datatype.FieldInfo{{Name: "forbidden_entry_field", Type: datatype.FieldTypeString}}}}}
	plugin.Register(p)
	resource := &commonModels.Engine{ID: engineID, TenantID: &tenantID, EngineType: p.Type(), LifecycleState: "active"}
	engineSvc := NewEngineService(db, nil)
	engineSvc.engineCache[engineCacheKey{tenantID: tenantID, engineID: engineID}] = &engineCacheEntry{resource: resource, expiresAt: time.Now().Add(time.Hour)}
	svc := NewScanService(db, engineSvc)
	parent := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: parentType, Name: parentName, FullName: parentName, Attributes: models.JSONMap{}}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	dataType, formatName := "table", ""
	if kind == "file" || kind == "object" {
		dataType = "document"
		formatName = "pdf"
	}
	attrs := models.JSONMap{"item": map[string]interface{}{"layout": "single", "data_type": dataType, "format": formatName}, "storage": map[string]interface{}{"schema_name": parentName, "path": "docs/", "name": name, "content_hash": "existing-hash"}}
	if kind == "object" {
		attrs["storage"].(map[string]interface{})["bucket"] = "docs"
	}
	if deep {
		attrs["type_info"] = map[string]interface{}{"table": map[string]interface{}{"fields": []interface{}{map[string]interface{}{"name": "preserved", "type": "string"}}}}
	}
	fullName := parentName + "." + name
	if kind == "file" || kind == "object" {
		fullName = "docs/" + name
	}
	item := models.MetaItem{TenantID: tenantID, EngineID: engineID, NodeID: parent.ID, ItemType: kind, Name: name, FullName: fullName, Fingerprint: "depth-item", Attributes: attrs, SizeBytes: &size, ScannedDepth: "none"}
	if deep {
		item.ScannedDepth = "deep"
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	// Compare through the same JSON persistence representation (numeric values,
	// optional fields), not the Go types used to construct the fixture.
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	return svc, p, item
}
