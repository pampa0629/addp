package preview

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/catalogutil"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
)

type namedPreviewProvider struct {
	name string
}

type previewRuntimeTokenSource struct{}

func (previewRuntimeTokenSource) Token(_ context.Context, tenantID uint) (string, error) {
	if tenantID != 7 {
		return "", fmt.Errorf("unexpected tenant id %d", tenantID)
	}
	return "tenant-runtime-token", nil
}

func (previewRuntimeTokenSource) PlatformToken(context.Context) (string, error) {
	return "platform-runtime-token", nil
}

func (p namedPreviewProvider) Name() string { return p.name }
func (p namedPreviewProvider) Preview(context.Context, *PreviewRequest) (*models.TablePreview, error) {
	return &models.TablePreview{
		Mode:     PreviewModeTable,
		Columns:  []string{"id"},
		Rows:     []map[string]interface{}{{"id": 1}},
		Total:    1,
		Page:     1,
		PageSize: 20,
	}, nil
}

type previewRoutingModelPlugin struct {
	model plugin.CatalogModelSpec
}

func (p *previewRoutingModelPlugin) Type() string         { return "preview-routing-model" }
func (p *previewRoutingModelPlugin) DisplayName() string  { return "preview-routing-model" }
func (p *previewRoutingModelPlugin) EngineOrigin() string { return "general" }
func (p *previewRoutingModelPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *previewRoutingModelPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *previewRoutingModelPlugin) DefaultPort() int          { return 0 }
func (p *previewRoutingModelPlugin) RequiredFields() []string  { return nil }
func (p *previewRoutingModelPlugin) SensitiveFields() []string { return nil }
func (p *previewRoutingModelPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{SchemaVersion: plugin.CapabilitiesSchemaVersion, EngineType: p.Type()}
}
func (p *previewRoutingModelPlugin) CatalogModel() plugin.CatalogModelSpec {
	return p.model
}

func registerPreviewRoutingModelPlugin(t *testing.T, model plugin.CatalogModelSpec) {
	t.Helper()
	const engineType = "preview-routing-model"
	previous, err := plugin.Get(engineType)
	plugin.Register(&previewRoutingModelPlugin{model: model})
	t.Cleanup(func() {
		if err == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(engineType)
	})
}

func TestLoadPreviewPluginsRegistersBuiltinDefaultsWithoutFiles(t *testing.T) {
	registry := NewPreviewRegistry()
	repo := repository.NewMetadataRepository(nil)
	LoadPreviewPlugins(registry, repo, nil, objectcontent.NewObjectContentRegistry(), "")

	for _, name := range []string{
		"builtin:database-table",
		"builtin:dynamic-schema-collection",
		"builtin:graph",
		"builtin:event-stream-topic",
		"builtin:scope-table",
		"builtin:container-child",
		"builtin:ref-file",
		"builtin:file-table",
		"builtin:object-catalog",
		"builtin:file-catalog",
		"builtin:schema-node",
	} {
		if _, err := registry.GetByName(name); err != nil {
			t.Fatalf("expected default provider %s: %v", name, err)
		}
	}
}

func TestProviderNamesForMetaRoutesTopicToEventStreamProvider(t *testing.T) {
	req := &PreviewResolverRequest{
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{"layout": "single", "data_type": "unknown"},
		}},
		ItemType: "topic",
	}
	names := providerNamesForMeta(req, &PreviewRequest{ItemType: "topic"})
	if len(names) != 1 || names[0] != "builtin:event-stream-topic" {
		t.Fatalf("provider names = %#v, want event stream topic provider", names)
	}
	if !isPreviewItemType("topic") {
		t.Fatal("topic must be recognized as preview item type")
	}
}

func TestPreviewFromURIWithBasicItemDoesNotSubmitDeepScanRun(t *testing.T) {
	registerPreviewRoutingModelPlugin(t, plugin.TabularCatalogModel(plugin.CatalogTermSchema))
	scanRunCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/engines/26":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":26,"tenant_id":1,"name":"preview","engine_type":"preview-routing-model","connection_info":{},"lifecycle_state":"active"}`))
		case "/api/v1/meta/items/1831":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1831,"tenant_id":1,"engine_id":26,"node_id":7,"item_type":"table","name":"users","full_name":"public.users","scanned_depth":"basic","attributes":{"item":{"data_type":"table","layout":"single"}}}`))
		case "/api/v1/meta/scan/run/manual":
			scanRunCalled = true
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:database-table"})
	resolver := NewPreviewResolver(
		registry,
		client.NewSystemClient(server.URL, client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "addp_at_test_service_token", nil
		})),
		client.NewMetaClient(server.URL, client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "test-token", nil
		})),
	)
	tenantID := uint(1)
	result, err := resolver.PreviewFromURI(
		context.Background(),
		"addp://engine/26/path/public/users?type=table&item_id=1831",
		1,
		20,
		"",
		&tenantID,
	)
	if err != nil {
		t.Fatalf("PreviewFromURI() error = %v", err)
	}
	if scanRunCalled {
		t.Fatal("preview should not submit Meta deep scan run")
	}
	preview, ok := result.Data.(*models.TablePreview)
	if !ok {
		t.Fatalf("result.Data = %T, want *models.TablePreview", result.Data)
	}
	if len(preview.Advisories) != 1 || preview.Advisories[0].Code != "item_refresh_recommended" {
		t.Fatalf("Advisories = %#v, want item refresh recommendation", preview.Advisories)
	}
	if result.Metadata == nil || result.Metadata.ScannedDepth != "basic" {
		t.Fatalf("metadata scanned depth = %#v, want basic", result.Metadata)
	}
}

func TestPreviewResolverScopesRuntimeDescriptorClientByTenant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tenant-runtime-token" {
			t.Fatalf("runtime descriptor authorization = %q, want tenant token", got)
		}
		if r.URL.Path != "/api/v1/system/runtime/engine-descriptors/9" {
			t.Fatalf("runtime descriptor path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9,"lifecycle_state":"active"}`))
	}))
	defer server.Close()

	runtime := client.NewSystemServiceClient(server.URL, previewRuntimeTokenSource{}, server.Client())
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil, runtime)
	tenantID := uint(7)
	scoped := resolver.runtimeDescriptorClientForTenant(&tenantID)
	if scoped == nil {
		t.Fatal("scoped runtime descriptor client is nil")
	}
	if _, err := scoped.GetEngineRuntimeDescriptor(context.Background(), 9); err != nil {
		t.Fatalf("scoped runtime descriptor request failed: %v", err)
	}
}

func TestLoadPreviewPluginsCanDisableDefaultProvider(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "preview.json")
	config := []byte(`{"providers":[{"name":"builtin:file-catalog","type":"builtin","builtin":"file-catalog","enabled":false}]}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write provider config: %v", err)
	}

	registry := NewPreviewRegistry()
	repo := repository.NewMetadataRepository(nil)
	LoadPreviewPlugins(registry, repo, nil, objectcontent.NewObjectContentRegistry(), dir)

	if _, err := registry.GetByName("builtin:file-catalog"); err == nil {
		t.Fatal("expected builtin:file-catalog to be disabled")
	}
	if _, err := registry.GetByName("builtin:file-table"); err != nil {
		t.Fatalf("expected other default providers to remain registered: %v", err)
	}
}

func TestLoadPreviewPluginsUsesFallbackDefaultsWithPreviewConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "preview.json")
	config := []byte(`{"providers":[]}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write provider config: %v", err)
	}

	registry := NewPreviewRegistry()
	repo := repository.NewMetadataRepository(nil)
	LoadPreviewPlugins(registry, repo, nil, objectcontent.NewObjectContentRegistry(), dir)

	if _, err := registry.GetByName("builtin:file-table"); err != nil {
		t.Fatalf("expected fallback builtin:file-table: %v", err)
	}
	if _, err := registry.GetByName("builtin:object-catalog"); err != nil {
		t.Fatalf("expected fallback builtin:object-catalog: %v", err)
	}
}

func TestLoadPreviewPluginsRejectsLegacyDefaultProvidersField(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "preview.json")
	config := []byte(`{"default_providers":[{"name":"builtin:file-table","type":"builtin","builtin":"file-table"}]}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write provider config: %v", err)
	}

	registry := NewPreviewRegistry()
	repo := repository.NewMetadataRepository(nil)
	LoadPreviewPlugins(registry, repo, nil, objectcontent.NewObjectContentRegistry(), dir)

	if _, err := registry.GetByName("builtin:file-table"); err == nil {
		t.Fatal("legacy default_providers config should not load fallback or requested provider")
	}
}

func TestLoadPreviewPluginsRejectsContentPluginField(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "preview.json")
	config := []byte(`{"content_plugins":[]}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write provider config: %v", err)
	}

	registry := NewPreviewRegistry()
	repo := repository.NewMetadataRepository(nil)
	LoadPreviewPlugins(registry, repo, nil, objectcontent.NewObjectContentRegistry(), dir)

	if _, err := registry.GetByName("builtin:file-table"); err == nil {
		t.Fatal("preview config with content_plugins should not load fallback providers")
	}
}

func TestResolveProviderByMetaUsesWholeTableLayout(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:scope-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "parquet",
				"layout":    "whole",
			},
		}},
		ItemType: "table",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "dataset"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:scope-table" {
		t.Fatalf("provider = %q, want builtin:scope-table", provider.Name())
	}
}

func TestResolveProviderByMetaDoesNotRouteWholeTableWithoutScopeProvider(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:scope-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "orc",
				"layout":    "whole",
			},
		}},
		ItemType: "table",
	}
	if _, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "dataset"}); err == nil {
		t.Fatal("expected no provider when format has no scope table implementation")
	}
}

func TestResolveProviderByMetaUsesItemDataTypeAndFormat(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "json",
			},
		}},
		ItemType: "object",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "roads.geojson"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:file-table" {
		t.Fatalf("provider = %q, want builtin:file-table", provider.Name())
	}
}

func TestResolveProviderByMetaUsesFileTableForFileCatalogTableFormat(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	registry.Register(namedPreviewProvider{name: "builtin:file-catalog"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "nfs"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "csv",
				"layout":    "single",
			},
		}},
		ItemType: "file",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "nfs"}, Table: "gis-data/sample.csv"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:file-table" {
		t.Fatalf("provider = %q, want builtin:file-table", provider.Name())
	}
}

func TestResolveProviderByMetaUsesDynamicSchemaCollectionForCollectionItem(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:dynamic-schema-collection"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "mongodb"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"layout":    "single",
			},
		}},
		ItemType: "collection",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "mongodb"}, Schema: "business", Table: "orders"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:dynamic-schema-collection" {
		t.Fatalf("provider = %q, want builtin:dynamic-schema-collection", provider.Name())
	}
}

func TestResolveProviderByMetaUsesContainerChildForExcelChild(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:container-child"})
	registry.Register(namedPreviewProvider{name: "builtin:object-catalog"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "container",
				"format":    "excel",
				"layout":    "single",
			},
		}},
		ItemType:  "object",
		ChildName: "Cities",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "test.xlsx", ChildName: "Cities"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:container-child" {
		t.Fatalf("provider = %q, want builtin:container-child", provider.Name())
	}
}

func TestResolveProviderByMetaUsesContainerChildForSQLiteChild(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:container-child"})
	registry.Register(namedPreviewProvider{name: "builtin:object-catalog"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "container",
				"format":    "sqlite",
				"layout":    "single",
			},
		}},
		ItemType:  "object",
		ChildName: "cities",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "sample.db", ChildName: "cities"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:container-child" {
		t.Fatalf("provider = %q, want builtin:container-child", provider.Name())
	}
}

func TestResolveProviderByMetaUsesContainerChildForNestedContainerChild(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:container-child"})
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	registry.Register(namedPreviewProvider{name: "builtin:file-catalog"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "nfs"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "container",
				"format":    "zip",
				"layout":    "single",
			},
		}},
		ItemType:        "file",
		ChildName:       "inner.zip",
		NestedChildPath: "roads.shp",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{
		Engine:          &models.Engine{EngineType: "nfs"},
		Table:           "outer.zip",
		ChildName:       "inner.zip",
		NestedChildPath: "roads.shp",
	})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:container-child" {
		t.Fatalf("provider = %q, want builtin:container-child", provider.Name())
	}
}

func TestResolveProviderByMetaPrefersPartitionedItemAttributes(t *testing.T) {
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:file-table"})
	resolver := NewPreviewResolver(registry, nil, nil)

	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{},
		Engine:  &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "json",
			},
		}},
		ItemType: "object",
	}
	provider, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "roads.geojson"})
	if err != nil {
		t.Fatalf("resolveProviderByMeta() error = %v", err)
	}
	if provider.Name() != "builtin:file-table" {
		t.Fatalf("provider = %q, want builtin:file-table", provider.Name())
	}
}

func TestStringAttributeReadsPartitionedStorageOnly(t *testing.T) {
	attrs := map[string]interface{}{
		"physical_path": "legacy/path.geojson",
		"storage": map[string]interface{}{
			"physical_path": "bucket/path.geojson",
		},
	}

	if got := catalogutil.StringAttribute(attrs, "physical_path"); got != "bucket/path.geojson" {
		t.Fatalf("stringAttribute() = %q, want bucket/path.geojson", got)
	}

	if got := catalogutil.StringAttribute(map[string]interface{}{"physical_path": "legacy/path.geojson"}, "physical_path"); got != "" {
		t.Fatalf("stringAttribute() legacy flat = %q, want empty", got)
	}
}

func TestAttributeHelpersReadPartitionedSlicesAndNumbers(t *testing.T) {
	attrs := map[string]interface{}{
		"refs": []interface{}{
			map[string]interface{}{"path": "legacy/a.shp"},
		},
		"item": map[string]interface{}{
			"refs": []interface{}{
				map[string]interface{}{"path": "bucket/roads/roads.shp"},
				map[string]interface{}{"path": "bucket/roads/roads.dbf"},
			},
		},
		"storage": map[string]interface{}{
			"total_size": float64(42),
		},
	}

	refs := refRefsFromMetaAttributes(attrs)
	if len(refs) != 2 || refs[0].Ref.Path != "bucket/roads/roads.shp" {
		t.Fatalf("refs = %#v, want partitioned refs", refs)
	}
	if got := catalogutil.Int64Attribute(attrs, "total_size"); got != 42 {
		t.Fatalf("total_size = %d, want 42", got)
	}
	if got := refRefsFromMetaAttributes(map[string]interface{}{"refs": []interface{}{map[string]interface{}{"path": "legacy/a.shp"}}}); len(got) != 0 {
		t.Fatalf("legacy flat refs = %#v, want empty", got)
	}
}

func TestBuildProviderRequestUsesPartitionedPhysicalPath(t *testing.T) {
	registerPreviewRoutingModelPlugin(t, plugin.ObjectCatalogModel())
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil)
	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{
			EngineID: 1,
			Path:     []string{"bucket", "table.parquet"},
			Type:     resourcetree.TypeObject,
		},
		Engine:       &commonModels.Engine{ID: 1, EngineType: "preview-routing-model"},
		Metadata:     &commonModels.MetaNode{Attributes: map[string]interface{}{}},
		ItemType:     "table",
		PhysicalPath: "bucket/table.parquet",
	}

	providerReq, err := resolver.buildProviderRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("buildProviderRequest() error = %v", err)
	}
	if providerReq.PhysicalPath != "bucket/table.parquet" {
		t.Fatalf("PhysicalPath = %q, want bucket/table.parquet", providerReq.PhysicalPath)
	}
	if !plugin.IsCatalogRootSegment(providerReq.ProviderPath.Segments[0]) {
		t.Fatalf("ProviderPath = %#v, want explicit root", providerReq.ProviderPath)
	}
}

func TestBuildProviderRequestKeepsChildName(t *testing.T) {
	registerPreviewRoutingModelPlugin(t, plugin.ObjectCatalogModel())
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil)
	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{EngineID: 1, Path: []string{"bucket", "test.xlsx"}, Type: resourcetree.TypeObject},
		Engine:  &commonModels.Engine{ID: 1, EngineType: "preview-routing-model"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"item": map[string]interface{}{"data_type": "container", "format": "excel"},
		}},
		ItemType:  "object",
		ChildName: "Cities",
	}

	providerReq, err := resolver.buildProviderRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("buildProviderRequest() error = %v", err)
	}
	if providerReq.ChildName != "Cities" {
		t.Fatalf("ChildName = %q, want Cities", providerReq.ChildName)
	}
}

func TestBuildProviderRequestUsesScopePathForWholeScopeTable(t *testing.T) {
	req := &PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{
			Path: []string{"bucket", "dataset"},
		},
		Engine: &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"physical_path": "bucket/dataset",
			},
			"item": map[string]interface{}{
				"layout": "whole",
			},
		}},
		ItemType: "table",
	}

	physicalPath, scopePath := previewResourcePaths(req.Metadata.Attributes)
	if physicalPath != "" {
		t.Fatalf("physicalPath = %q, want empty for whole scope table", physicalPath)
	}
	if scopePath != "bucket/dataset" {
		t.Fatalf("scopePath = %q, want bucket/dataset", scopePath)
	}
}

func TestResolveProviderByMetaRejectsUnmappedMeta(t *testing.T) {
	resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil)
	req := &PreviewResolverRequest{
		Locator:  &resourcetree.ResourceLocator{},
		Engine:   &commonModels.Engine{EngineType: "minio"},
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{}},
		ItemType: "object",
	}

	if _, err := resolver.resolveProviderByMeta(req, &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}, Table: "unknown.bin"}); err == nil {
		t.Fatal("expected error for unmapped meta")
	}
}

func TestBuildMetadataDeclaresOwnerVerifiedQueryFacts(t *testing.T) {
	itemID := uint(7)
	resolver := NewPreviewResolver(nil, nil, nil)
	relational := resolver.buildMetadata(&PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{}, Engine: &commonModels.Engine{Name: "pg", EngineType: "postgresql"},
		Metadata: &commonModels.MetaNode{}, MetaItemID: &itemID, ItemFullName: "analytics.users", ItemScannedDepth: "deep",
	})
	if relational.QueryNames["sql"] != "analytics.users" || relational.QueryNames["federated_sql"] != "pg.analytics.users" || relational.SchemaCoverage != "complete" || relational.EngineType != "postgresql" {
		t.Fatalf("unexpected relational query facts: %#v", relational)
	}

	dynamic := resolver.buildMetadata(&PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{}, Engine: &commonModels.Engine{Name: "mongo", EngineType: "mongodb"},
		Metadata: &commonModels.MetaNode{}, MetaItemID: &itemID, ItemName: "Persons.v2", ItemFullName: "Outdoor.Persons.v2", ItemScannedDepth: "deep",
	})
	if dynamic.QueryNames["mql"] != "Persons.v2" || dynamic.SchemaCoverage != "sampled" {
		t.Fatalf("unexpected dynamic query facts: %#v", dynamic)
	}

	object := resolver.buildMetadata(&PreviewResolverRequest{
		Locator: &resourcetree.ResourceLocator{}, Engine: &commonModels.Engine{Name: "lake source", EngineType: "minio"},
		Metadata: &commonModels.MetaNode{}, MetaItemID: &itemID, ItemName: "events parquet", ItemFullName: "bucket/events.parquet", ItemScannedDepth: "deep",
	})
	if object.QueryNames["federated_sql"] != "lake_source.events_parquet" {
		t.Fatalf("unexpected object query facts: %#v", object)
	}
}
