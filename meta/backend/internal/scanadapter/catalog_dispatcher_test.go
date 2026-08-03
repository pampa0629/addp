package scanadapter

import (
	"context"
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

func TestCatalogDispatcherFinalizesContentCatalogRoot(t *testing.T) {
	tests := []struct {
		name       string
		model      plugin.CatalogModelSpec
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
			db := openCatalogDispatcherTestDB(t)
			repo := metaRepo.NewScanRepository(db)
			enginePlugin := catalogDispatcherTestPlugin{engineType: tt.engineType, model: tt.model}
			plugin.Register(enginePlugin)
			t.Cleanup(func() {
				plugin.Unregister(enginePlugin.Type())
			})

			contentAdapter := catalogDispatcherTestContentAdapter{
				result: scanflow.DispatchResult{Items: tt.wantItems},
			}
			dispatcher := NewCatalogDispatcher(
				db,
				repo,
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				nil,
				nil,
				nil,
				NewContentCatalogScanner(contentAdapter, contentAdapter),
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

func TestCatalogDispatcherRoutesDirectLeaves(t *testing.T) {
	db := openCatalogDispatcherTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	enginePlugin := catalogDispatcherTestPlugin{
		engineType: "dispatcher-direct-leaf-test",
		model: plugin.CatalogModelSpec{
			PathVersion: plugin.CatalogPathVersion,
			RootTerm:    plugin.CatalogTermService,
			Levels: []plugin.CatalogLevelSpec{
				{Term: "topic", Kinds: []string{"topic"}, Role: plugin.CatalogRoleLeaf},
			},
		},
	}
	plugin.Register(enginePlugin)
	t.Cleanup(func() {
		plugin.Unregister(enginePlugin.Type())
	})

	directScanner := &catalogDispatcherTestDirectScanner{items: 2}
	dispatcher := NewCatalogDispatcher(
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

func (a catalogDispatcherTestContentAdapter) ScanPaths(*commonModels.Engine, uint, []string, string, bool, scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.result, nil
}

func (a catalogDispatcherTestContentAdapter) ScanRefGroups(*commonModels.Engine, uint, []models.ScanRefGroup, string, bool, scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	return a.result, nil
}

type catalogDispatcherTestPlugin struct {
	engineType string
	model      plugin.CatalogModelSpec
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
func (p catalogDispatcherTestPlugin) CatalogModel() plugin.CatalogModelSpec {
	return p.model
}

func openCatalogDispatcherTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return metatest.OpenMetadataDB(t, metatest.WithoutMetaItemTable())
}
