package scanadapter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

func TestEngineCatalogScanDispatcherFinalizesContentCatalogRoot(t *testing.T) {
	tests := []struct {
		name       string
		model      plugin.EngineCatalogModelSpec
		engineType string
		wantItems  int
	}{
		{
			name:       "object catalog",
			model:      plugin.ObjectCatalogModel(),
			engineType: "dispatcher-object-root-test",
			wantItems:  3,
		},
		{
			name:       "file catalog",
			model:      plugin.FileCatalogModel(),
			engineType: "dispatcher-file-root-test",
			wantItems:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openEngineCatalogScanDispatcherTestDB(t)
			repo := metaRepo.NewScanRepository(db)
			enginePlugin := catalogDispatcherTestPlugin{engineType: tt.engineType, model: tt.model}
			plugin.Register(enginePlugin)
			t.Cleanup(func() {
				plugin.Unregister(enginePlugin.Type())
			})

			contentAdapter := catalogDispatcherTestContentAdapter{
				result: scanflow.DispatchResult{Items: tt.wantItems},
			}
			dispatcher := NewEngineCatalogScanDispatcher(
				db,
				repo,
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				nil,
				nil,
				nil,
				NewEngineCatalogContentScanner(contentAdapter, contentAdapter),
			)

			resource := &commonModels.Engine{ID: 31, Name: "Content Catalog", EngineType: tt.engineType}
			result, err := dispatcher.Dispatch(scanflow.DispatchRequest{
				Resource:  resource,
				TenantID:  1,
				ScanDepth: models.ScannedDepthDeep,
				Force:     true,
				Mode:      scanflow.DispatchManual,
			})
			if err != nil {
				t.Fatalf("Dispatch() error = %v", err)
			}
			if result.Items != tt.wantItems {
				t.Fatalf("result.Items = %d, want %d", result.Items, tt.wantItems)
			}

			var root models.MetaNode
			if err := db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL", 1, resource.ID).First(&root).Error; err != nil {
				t.Fatalf("query root node: %v", err)
			}
			if root.ScanStatus != "completed" || root.ScannedDepth != models.ScannedDepthDeep {
				t.Fatalf("root scan status/depth = %q/%q, want completed/deep", root.ScanStatus, root.ScannedDepth)
			}
			if root.ItemCount != tt.wantItems {
				t.Fatalf("root item_count = %d, want %d", root.ItemCount, tt.wantItems)
			}
		})
	}
}

func TestEngineCatalogScanDispatcherRoutesDirectLeaves(t *testing.T) {
	db := openEngineCatalogScanDispatcherTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	enginePlugin := catalogDispatcherTestPlugin{
		engineType: "dispatcher-direct-leaf-test",
		model: plugin.EngineCatalogModelSpec{
			PathVersion: plugin.EngineCatalogPathVersion,
			RootTerm:    plugin.EngineCatalogTermService,
			Levels: []plugin.EngineCatalogLevelSpec{
				{Term: "topic", Kinds: []string{"topic"}, Role: plugin.EngineCatalogRoleLeaf},
			},
		},
	}
	plugin.Register(enginePlugin)
	t.Cleanup(func() {
		plugin.Unregister(enginePlugin.Type())
	})

	directScanner := &catalogDispatcherTestDirectScanner{items: 2}
	dispatcher := NewEngineCatalogScanDispatcher(
		db,
		repo,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		nil,
		nil,
		directScanner,
		nil,
	)
	resource := &commonModels.Engine{ID: 32, Name: "Kafka", EngineType: enginePlugin.Type()}
	result, err := dispatcher.Dispatch(scanflow.DispatchRequest{
		Resource:  resource,
		TenantID:  1,
		ScanDepth: models.ScannedDepthBasic,
		Force:     true,
		Mode:      scanflow.DispatchManual,
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !directScanner.called || result.CatalogNodes != 0 || result.Items != 2 || result.Fields != 0 {
		t.Fatalf("direct scanner called/result = %v/%#v", directScanner.called, result)
	}
}

func TestEngineCatalogScanDispatcherFullTabularScanReconcilesNamespacesAndRoot(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	enginePlugin := catalogDispatcherTestPlugin{
		engineType: "dispatcher-tabular-reconcile-test",
		model:      plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema),
		entries: []plugin.EngineCatalogEntry{
			{Name: "BUSINESS", Role: plugin.EngineCatalogRoleBranch},
		},
	}
	plugin.Register(enginePlugin)
	t.Cleanup(func() {
		plugin.Unregister(enginePlugin.Type())
	})

	resource := &commonModels.Engine{ID: 33, Name: "Business Oracle", EngineType: enginePlugin.Type()}
	root, err := metaRepo.EnsureEngineCatalogRootNode(repo, 1, resource, enginePlugin)
	if err != nil {
		t.Fatalf("EnsureEngineCatalogRootNode() error = %v", err)
	}
	if _, err := repo.UpsertNode(1, resource.ID, root, plugin.EngineCatalogTermSchema, "BUSINESS", stringPtr("BUSINESS"), models.JSONMap{}); err != nil {
		t.Fatalf("create current namespace: %v", err)
	}
	stale, err := repo.UpsertNode(1, resource.ID, root, plugin.EngineCatalogTermSchema, "PDBADMIN", stringPtr("PDBADMIN"), models.JSONMap{})
	if err != nil {
		t.Fatalf("create stale namespace: %v", err)
	}

	namespaceScanner := &catalogDispatcherTestNamespaceScanner{catalogNodes: 1, items: 4, fields: 12}
	dispatcher := NewEngineCatalogScanDispatcher(
		db,
		repo,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		namespaceScanner,
		nil,
		nil,
		nil,
	)
	result, err := dispatcher.Dispatch(scanflow.DispatchRequest{
		Resource:  resource,
		TenantID:  1,
		ScanDepth: models.ScannedDepthDeep,
		Force:     true,
		Mode:      scanflow.DispatchManual,
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if len(namespaceScanner.namespaces) != 1 || namespaceScanner.namespaces[0] != "BUSINESS" {
		t.Fatalf("scanned namespaces = %#v, want [BUSINESS]", namespaceScanner.namespaces)
	}
	if result.CatalogNodes != 1 || result.Items != 4 || result.Fields != 12 {
		t.Fatalf("Dispatch() result = %#v", result)
	}

	var activeStale models.MetaNode
	if err := db.First(&activeStale, stale.ID).Error; err == nil {
		t.Fatalf("stale namespace still active: %#v", activeStale)
	} else if err != gorm.ErrRecordNotFound {
		t.Fatalf("query stale namespace: %v", err)
	}
	if err := db.First(root, root.ID).Error; err != nil {
		t.Fatalf("reload root: %v", err)
	}
	if root.ScanStatus != "completed" || root.ScannedDepth != models.ScannedDepthDeep || root.ItemCount != 4 {
		t.Fatalf("root status/depth/items = %q/%q/%d, want completed/deep/4", root.ScanStatus, root.ScannedDepth, root.ItemCount)
	}
}

func TestEngineCatalogScanDispatcherFailsExecutionButKeepsSuccessfulNamespaceCounts(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	enginePlugin := catalogDispatcherTestPlugin{
		engineType: "dispatcher-tabular-partial-failure-test",
		model:      plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema),
		entries: []plugin.EngineCatalogEntry{
			{Name: "GOOD", Role: plugin.EngineCatalogRoleBranch},
			{Name: "BROKEN", Role: plugin.EngineCatalogRoleBranch},
		},
	}
	plugin.Register(enginePlugin)
	t.Cleanup(func() { plugin.Unregister(enginePlugin.Type()) })

	namespaceScanner := &catalogDispatcherTestNamespaceScanner{
		catalogNodes: 1,
		items:        2,
		fields:       3,
		errors:       map[string]error{"BROKEN": errors.New("permission denied")},
	}
	dispatcher := NewEngineCatalogScanDispatcher(
		db,
		repo,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		namespaceScanner,
		nil,
		nil,
		nil,
	)
	result, err := dispatcher.Dispatch(scanflow.DispatchRequest{
		Resource:  &commonModels.Engine{ID: 34, Name: "Database", EngineType: enginePlugin.Type()},
		TenantID:  1,
		ScanDepth: models.ScannedDepthDeep,
		Force:     true,
		Mode:      scanflow.DispatchManual,
	})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want failed target error")
	}
	if result.CatalogNodes != 1 || result.Items != 2 || result.Fields != 3 {
		t.Fatalf("successful counts = %#v", result)
	}
	count, samples := scanflow.FailedTargetDetails(err)
	if count != 1 || len(samples) != 1 || samples[0].Target != "BROKEN" {
		t.Fatalf("failed target details = %d/%#v", count, samples)
	}
}

type catalogDispatcherTestNamespaceScanner struct {
	namespaces   []string
	catalogNodes int
	items        int
	fields       int
	errors       map[string]error
}

func (s *catalogDispatcherTestNamespaceScanner) ScanNamespace(_ context.Context, _ plugin.EnginePlugin, _ *commonModels.Engine, _, _ uint, namespaceName, _ string, _ bool) (int, int, int, error) {
	s.namespaces = append(s.namespaces, namespaceName)
	if err := s.errors[namespaceName]; err != nil {
		return 0, 0, 0, err
	}
	return s.catalogNodes, s.items, s.fields, nil
}

func stringPtr(value string) *string {
	return &value
}

type catalogDispatcherTestDirectScanner struct {
	called bool
	items  int
}

func (s *catalogDispatcherTestDirectScanner) ScanRoot(context.Context, plugin.EnginePlugin, *commonModels.Engine, uint, string, bool) (int, error) {
	s.called = true
	return s.items, nil
}

type catalogDispatcherTestContentAdapter struct {
	result scanflow.DispatchResult
}

func (a catalogDispatcherTestContentAdapter) ScanPaths(context.Context, *commonModels.Engine, uint, []string, string, bool, scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.result, nil
}

func (a catalogDispatcherTestContentAdapter) ScanRefGroups(context.Context, *commonModels.Engine, uint, []models.ScanRefGroup, string, bool, scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.result, nil
}

type catalogDispatcherTestPlugin struct {
	engineType string
	model      plugin.EngineCatalogModelSpec
	entries    []plugin.EngineCatalogEntry
}

func (p catalogDispatcherTestPlugin) Type() string         { return p.engineType }
func (p catalogDispatcherTestPlugin) DisplayName() string  { return "catalog dispatcher test" }
func (p catalogDispatcherTestPlugin) EngineOrigin() string { return "general" }
func (p catalogDispatcherTestPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p catalogDispatcherTestPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p catalogDispatcherTestPlugin) DefaultPort() int                                   { return 0 }
func (p catalogDispatcherTestPlugin) RequiredFields() []string                           { return nil }
func (p catalogDispatcherTestPlugin) SensitiveFields() []string                          { return nil }
func (p catalogDispatcherTestPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p catalogDispatcherTestPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return p.model
}
func (p catalogDispatcherTestPlugin) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return append([]plugin.EngineCatalogEntry(nil), p.entries...), nil
}
func (p catalogDispatcherTestPlugin) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return nil, nil
}

func openEngineCatalogScanDispatcherTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return metatest.OpenMetadataDB(t, metatest.WithoutMetaItemTable())
}
