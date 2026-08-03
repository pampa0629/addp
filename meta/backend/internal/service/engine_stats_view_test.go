package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func TestBuildResourceWithStatsProjectsScanStats(t *testing.T) {
	lastScan := time.Date(2026, 6, 6, 8, 30, 0, 0, time.UTC)
	lastCheck := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	tenantID := uint(1)
	resource := &commonModels.Engine{
		ID:               9,
		TenantID:         &tenantID,
		Name:             "Business MinIO",
		EngineType:       "s3",
		ConnectionStatus: "healthy",
		LastCheckAt:      &lastCheck,
		CheckMessage:     "ok",
	}
	stats := &engineScanStats{
		totalCount:   map[uint]int64{9: 12},
		scannedCount: map[uint]int64{9: 7},
		lastScanByID: map[uint]*time.Time{9: &lastScan},
	}

	view := buildResourceWithStats(resource, stats)
	if view.EngineID != resource.ID || view.ResourceName != resource.Name || view.ResourceType != resource.EngineType {
		t.Fatalf("identity fields = %#v", view)
	}
	if view.TotalCatalogNodes != 12 || view.ScannedCatalogNodes != 7 || view.UnscannedCatalogNodes != 5 {
		t.Fatalf("scan counts = total:%d scanned:%d unscanned:%d", view.TotalCatalogNodes, view.ScannedCatalogNodes, view.UnscannedCatalogNodes)
	}
	if view.ScannedAt != "2026-06-06 08:30:00" {
		t.Fatalf("ScannedAt = %q", view.ScannedAt)
	}
	if view.LastCheckAt != "2026-06-06 09:00:00" || view.ConnectionStatus != "healthy" || view.CheckMessage != "ok" {
		t.Fatalf("connection fields = %#v", view)
	}
	if view.EngineFamily == "" || view.CatalogRootTerm == "" || view.CatalogLeafTerm == "" {
		t.Fatalf("catalog terms not projected: %#v", view)
	}
}

func TestLoadEngineScanStatsUsesCatalogModelTopLevel(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	directPlugin := engineStatsTestPlugin{
		engineType: "engine-stats-direct-leaf-test",
		model: plugin.CatalogModelSpec{
			PathVersion: plugin.CatalogPathVersion,
			RootTerm:    plugin.CatalogTermService,
			Levels: []plugin.CatalogLevelSpec{
				{Term: "topic", Kinds: []string{"topic"}, Role: plugin.CatalogRoleLeaf},
			},
		},
	}
	branchPlugin := engineStatsTestPlugin{
		engineType: "engine-stats-branch-test",
		model:      plugin.TabularCatalogModel(plugin.CatalogTermSchema),
	}
	plugin.Register(directPlugin)
	plugin.Register(branchPlugin)
	t.Cleanup(func() {
		plugin.Unregister(directPlugin.Type())
		plugin.Unregister(branchPlugin.Type())
	})

	directResource := &commonModels.Engine{ID: 41, Name: "Kafka", EngineType: directPlugin.Type()}
	directRoot, err := metaRepo.EnsureCatalogRootNode(repo, 1, directResource, directPlugin)
	if err != nil {
		t.Fatalf("ensure direct root: %v", err)
	}
	if _, err := repo.UpsertItemWithDepth(1, 41, directRoot, "topic", "orders", "orders", models.JSONMap{}, nil, nil, nil, models.ScannedDepthDeep); err != nil {
		t.Fatalf("create direct item: %v", err)
	}

	branchResource := &commonModels.Engine{ID: 42, Name: "PostgreSQL", EngineType: branchPlugin.Type()}
	branchRoot, err := metaRepo.EnsureCatalogRootNode(repo, 1, branchResource, branchPlugin)
	if err != nil {
		t.Fatalf("ensure branch root: %v", err)
	}
	branchNode, err := repo.UpsertNode(1, 42, branchRoot, plugin.CatalogTermSchema, "public", nil, nil)
	if err != nil {
		t.Fatalf("create branch node: %v", err)
	}
	if err := db.Model(branchNode).Update("scan_status", "completed").Error; err != nil {
		t.Fatalf("mark branch node completed: %v", err)
	}

	stats, err := loadEngineScanStats(db, []*commonModels.Engine{directResource, branchResource})
	if err != nil {
		t.Fatalf("loadEngineScanStats() error = %v", err)
	}
	if stats.totalCount[41] != 1 || stats.scannedCount[41] != 1 {
		t.Fatalf("direct leaf counts = %d/%d, want 1/1", stats.totalCount[41], stats.scannedCount[41])
	}
	if stats.totalCount[42] != 1 || stats.scannedCount[42] != 1 {
		t.Fatalf("branch counts = %d/%d, want 1/1", stats.totalCount[42], stats.scannedCount[42])
	}

	view := buildResourceWithStats(directResource, stats)
	if view.CatalogTopTerm != "topic" || view.CatalogLeafTerm != "topic" {
		t.Fatalf("direct leaf top/leaf terms = %q/%q, want topic/topic", view.CatalogTopTerm, view.CatalogLeafTerm)
	}
}

type engineStatsTestPlugin struct {
	engineType string
	model      plugin.CatalogModelSpec
}

func (p engineStatsTestPlugin) Type() string         { return p.engineType }
func (p engineStatsTestPlugin) DisplayName() string  { return p.engineType }
func (p engineStatsTestPlugin) EngineOrigin() string { return "general" }
func (p engineStatsTestPlugin) DefaultPort() int     { return 0 }
func (p engineStatsTestPlugin) RequiredFields() []string {
	return nil
}
func (p engineStatsTestPlugin) SensitiveFields() []string {
	return nil
}
func (p engineStatsTestPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p engineStatsTestPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p engineStatsTestPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p engineStatsTestPlugin) CatalogModel() plugin.CatalogModelSpec {
	return p.model
}

var _ plugin.CatalogModelProvider = engineStatsTestPlugin{}
