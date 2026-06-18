package service

import (
	"context"
	"testing"

	"github.com/addp/common/events"
	"github.com/addp/service/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestServiceCleanupScanFindsEngineBoundServices(t *testing.T) {
	t.Parallel()

	db := newServiceCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	engineID := uint(12)
	otherEngineID := uint(13)
	createServiceCleanupQueryService(t, db, 7, "query-match", &engineID)
	createServiceCleanupQueryService(t, db, 7, "query-duckdb", nil)
	createServiceCleanupGraphService(t, db, 7, "graph-match", engineID)
	createServiceCleanupGraphService(t, db, 7, "graph-other", otherEngineID)
	tile := createServiceCleanupTileService(t, db, 7, "tile-match")
	createServiceCleanupTileLayer(t, db, tile.ID, "roads", engineID)
	createServiceCleanupTileLayer(t, db, tile.ID, "buildings", otherEngineID)

	stats, err := svc.ScanReclaimCandidates(context.Background(), 7, map[string]interface{}{"engine_id": engineID})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if stats.QueryServices != 1 || stats.GraphQueryServices != 1 || stats.TileServices != 1 || stats.TileLayers != 1 {
		t.Fatalf("stats = %#v, want one query, graph, tile service and tile layer", stats)
	}

	stats, err = svc.ScanReclaimCandidates(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() without context error = %v", err)
	}
	if stats.QueryServices != 0 || stats.GraphQueryServices != 0 || stats.TileServices != 0 || stats.TileLayers != 0 {
		t.Fatalf("stats without lifecycle context = %#v, want empty", stats)
	}
}

func TestServiceCleanupLogicalDisablesEngineBoundServices(t *testing.T) {
	t.Parallel()

	db := newServiceCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	engineID := uint(12)
	query := createServiceCleanupQueryService(t, db, 7, "query-match", &engineID)
	graph := createServiceCleanupGraphService(t, db, 7, "graph-match", engineID)
	tile := createServiceCleanupTileService(t, db, 7, "tile-match")
	layer := createServiceCleanupTileLayer(t, db, tile.ID, "roads", engineID)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"engine_id": engineID})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DisabledServiceRecords != 3 || stats.DisabledTileLayers != 1 {
		t.Fatalf("stats = %#v, want disabled service records and layer", stats)
	}

	var updatedQuery models.QueryService
	if err := db.First(&updatedQuery, query.ID).Error; err != nil {
		t.Fatalf("load query service: %v", err)
	}
	if updatedQuery.Status != "inactive" {
		t.Fatalf("query status = %q, want inactive", updatedQuery.Status)
	}
	var updatedGraph models.GraphQueryService
	if err := db.First(&updatedGraph, graph.ID).Error; err != nil {
		t.Fatalf("load graph service: %v", err)
	}
	if updatedGraph.Status != "inactive" {
		t.Fatalf("graph status = %q, want inactive", updatedGraph.Status)
	}
	var updatedTile models.TileService
	if err := db.First(&updatedTile, tile.ID).Error; err != nil {
		t.Fatalf("load tile service: %v", err)
	}
	if updatedTile.Status != "inactive" {
		t.Fatalf("tile status = %q, want inactive", updatedTile.Status)
	}
	var updatedLayer models.TileServiceLayer
	if err := db.First(&updatedLayer, layer.ID).Error; err != nil {
		t.Fatalf("load tile layer: %v", err)
	}
	if updatedLayer.Enabled {
		t.Fatal("tile layer should be disabled")
	}
}

func TestServiceCleanupPhysicalDeletesEngineBoundServices(t *testing.T) {
	t.Parallel()

	db := newServiceCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	engineID := uint(12)
	query := createServiceCleanupQueryService(t, db, 7, "query-match", &engineID)
	otherEngineID := uint(13)
	other := createServiceCleanupQueryService(t, db, 7, "query-other", &otherEngineID)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"engine_id": engineID})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DeletedServiceRecords != 1 {
		t.Fatalf("DeletedServiceRecords = %d, want 1", stats.DeletedServiceRecords)
	}
	if err := db.First(&models.QueryService{}, query.ID).Error; err == nil {
		t.Fatal("engine-bound query service should be deleted")
	}
	if err := db.First(&models.QueryService{}, other.ID).Error; err != nil {
		t.Fatalf("other query service should remain: %v", err)
	}
}

func TestServiceCleanupTenantDeletedContextScopesTenantOwnedServices(t *testing.T) {
	t.Parallel()

	db := newServiceCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	engineID := uint(12)
	query := createServiceCleanupQueryService(t, db, 7, "tenant-query", &engineID)
	otherTenantQuery := createServiceCleanupQueryService(t, db, 8, "other-query", &engineID)
	graph := createServiceCleanupGraphService(t, db, 7, "tenant-graph", engineID)
	tile := createServiceCleanupTileService(t, db, 7, "tenant-tile")
	layer := createServiceCleanupTileLayer(t, db, tile.ID, "roads", engineID)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DeletedServiceRecords != 3 || stats.DeletedTileLayers != 1 {
		t.Fatalf("stats = %#v, want tenant services and layers deleted", stats)
	}
	for name, id := range map[string]uint{
		"query": query.ID,
		"graph": graph.ID,
		"tile":  tile.ID,
	} {
		var count int64
		var model interface{}
		switch name {
		case "query":
			model = &models.QueryService{}
		case "graph":
			model = &models.GraphQueryService{}
		case "tile":
			model = &models.TileService{}
		}
		if err := db.Model(model).Where("id = ?", id).Count(&count).Error; err != nil {
			t.Fatalf("count %s service: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("%s service should be deleted", name)
		}
	}
	var layerCount int64
	if err := db.Model(&models.TileServiceLayer{}).Where("id = ?", layer.ID).Count(&layerCount).Error; err != nil {
		t.Fatalf("count tile layer: %v", err)
	}
	if layerCount != 0 {
		t.Fatal("tenant tile layer should be deleted")
	}
	if err := db.First(&models.QueryService{}, otherTenantQuery.ID).Error; err != nil {
		t.Fatalf("other tenant query service should remain: %v", err)
	}
}

func newServiceCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS service").Error; err != nil {
		t.Fatalf("attach service schema: %v", err)
	}
	statements := []string{
		`CREATE TABLE service.query_services (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			service_name TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT,
			keywords TEXT,
			config_type TEXT NOT NULL,
			engine_id INTEGER,
			schema_name TEXT,
			table_name TEXT,
			sql_query TEXT,
			data_config JSON,
			protocols JSON,
			public_access BOOLEAN,
			max_features INTEGER,
			status TEXT,
			error_message TEXT,
			created_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE service.graph_query_services (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			service_name TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT,
			keywords TEXT,
			engine_id INTEGER NOT NULL,
			database_name TEXT,
			config_type TEXT,
			node_shape TEXT,
			node_labels TEXT,
			cypher_query TEXT,
			data_config JSON,
			public_access BOOLEAN,
			max_records INTEGER,
			status TEXT,
			error_message TEXT,
			created_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE service.tile_services (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			service_name TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT,
			keywords TEXT,
			default_srid INTEGER,
			extent JSON,
			protocols JSON,
			public_access BOOLEAN,
			status TEXT,
			error_message TEXT,
			created_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE service.tile_service_layers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			service_id INTEGER NOT NULL,
			layer_name TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT,
			layer_type TEXT NOT NULL,
			layer_config JSON,
			display_order INTEGER,
			enabled BOOLEAN,
			created_at DATETIME,
			updated_at DATETIME
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}

func createServiceCleanupQueryService(t *testing.T, db *gorm.DB, tenantID uint, name string, engineID *uint) models.QueryService {
	t.Helper()
	item := models.QueryService{
		TenantID:    tenantID,
		ServiceName: name,
		Title:       name,
		ConfigType:  "table",
		EngineID:    engineID,
		Status:      "active",
		CreatedBy:   1,
		DataConfig:  models.JSONB{},
		Protocols:   models.JSONB{},
	}
	if engineID == nil {
		item.ConfigType = "sql"
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create query service: %v", err)
	}
	return item
}

func createServiceCleanupGraphService(t *testing.T, db *gorm.DB, tenantID uint, name string, engineID uint) models.GraphQueryService {
	t.Helper()
	item := models.GraphQueryService{
		TenantID:     tenantID,
		ServiceName:  name,
		Title:        name,
		EngineID:     engineID,
		DatabaseName: "neo4j",
		ConfigType:   "cypher",
		DataConfig:   models.JSONB{},
		Status:       "active",
		CreatedBy:    1,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create graph query service: %v", err)
	}
	return item
}

func createServiceCleanupTileService(t *testing.T, db *gorm.DB, tenantID uint, name string) models.TileService {
	t.Helper()
	item := models.TileService{
		TenantID:     tenantID,
		ServiceName:  name,
		Title:        name,
		DefaultSRID:  3857,
		Protocols:    models.JSONB{},
		Status:       "active",
		CreatedBy:    1,
		PublicAccess: false,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create tile service: %v", err)
	}
	return item
}

func createServiceCleanupTileLayer(t *testing.T, db *gorm.DB, serviceID uint, name string, engineID uint) models.TileServiceLayer {
	t.Helper()
	item := models.TileServiceLayer{
		ServiceID: serviceID,
		LayerName: name,
		Title:     name,
		LayerType: "dynamic",
		LayerConfig: models.JSONB{
			"source": map[string]interface{}{"engine_id": float64(engineID)},
		},
		Enabled: true,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create tile layer: %v", err)
	}
	return item
}
