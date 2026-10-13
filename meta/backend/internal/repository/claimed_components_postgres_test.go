package repository

import (
	"fmt"
	"os"
	"testing"

	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	metaMigrations "github.com/addp/meta/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestClaimedComponentsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec(`DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta;
	CREATE TABLE meta.meta_item (
		id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, engine_id BIGINT NOT NULL, node_id BIGINT NOT NULL,
		item_type TEXT NOT NULL, name TEXT NOT NULL, full_name TEXT, fingerprint TEXT NOT NULL UNIQUE,
		row_count BIGINT, size_bytes BIGINT, data_updated_at TIMESTAMPTZ, scanned_at TIMESTAMPTZ,
		scanned_depth TEXT, attributes JSONB, created_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ
	)`).Error; err != nil {
		t.Fatal(err)
	}
	migration, err := metaMigrations.FS.ReadFile("019_add_data_item_changes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewScanRepository(tx)
	node := &models.MetaNode{ID: 3}
	single := models.JSONMap{"item": map[string]interface{}{"layout": "single"}}
	var original *models.MetaItem
	for _, name := range []string{"roads.shp", "roads.shx", "roads.dbf", "roads.prj", "other.txt"} {
		item, err := repo.UpsertItemWithDepth(1, 26, node, "file", name, "gis/"+name, single, nil, nil, nil, "deep")
		if err != nil {
			t.Fatal(err)
		}
		if name == "roads.shp" {
			original = item
		}
	}
	// Same full_name in a different Tenant, Engine or leaf type must survive.
	for i, identity := range [][3]interface{}{{2, 26, "file"}, {1, 27, "file"}, {1, 26, "table"}} {
		if err := tx.Exec(`INSERT INTO meta.meta_item (tenant_id, engine_id, node_id, item_type, name, full_name, fingerprint, attributes)
		VALUES (?, ?, 3, ?, 'roads.dbf', 'gis/roads.dbf', ?, '{"item":{"layout":"single"}}')`, identity[0], identity[1], identity[2], fmt.Sprintf("outside-%d", i)).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A claimed path already representing a composite is not an independent component.
	if _, err := repo.UpsertItemWithDepth(1, 26, node, "file", "nested.tif", "gis/nested.tif", models.JSONMap{"item": map[string]interface{}{"layout": "multi"}}, nil, nil, nil, "deep"); err != nil {
		t.Fatal(err)
	}
	claims := []string{"gis/roads.shp", "gis/roads.shx", "gis/roads.dbf", "gis/nested.tif"}
	multi := models.JSONMap{"item": map[string]interface{}{"layout": "multi", "format": "shapefile"}}
	persist := func() error {
		return repo.Transaction(func(repo *ScanRepository) error {
			item, err := repo.UpsertItemWithDepth(1, 26, node, "file", "roads.shp", "gis/roads.shp", multi, nil, nil, nil, "basic")
			if err != nil {
				return err
			}
			_, err = repo.DeleteClaimedSingleItems(item, claims)
			return err
		})
	}
	if err := tx.Exec(`CREATE FUNCTION meta.reject_component_delete() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN RAISE EXCEPTION 'fixture refuses component deletion'; END $$;
	CREATE TRIGGER reject_component_delete BEFORE DELETE ON meta.meta_item FOR EACH ROW EXECUTE FUNCTION meta.reject_component_delete()`).Error; err != nil {
		t.Fatal(err)
	}
	if err := persist(); err == nil {
		t.Fatal("expected component deletion failure")
	}
	primary, found, err := repo.FindItemByFullName(1, 26, "gis/roads.shp")
	if err != nil || !found {
		t.Fatalf("primary missing after rollback: %v", err)
	}
	if primary.ID != original.ID || primary.ScannedDepth != "deep" || commonJSON.String(primary.Attributes, "item", "layout") != "single" {
		t.Fatal("failed reconciliation did not roll back primary")
	}
	var before int64
	if err := tx.Model(&models.MetaItem{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if before != 9 {
		t.Fatalf("rollback item count = %d, want 9", before)
	}
	if err := tx.Exec("DROP TRIGGER reject_component_delete ON meta.meta_item").Error; err != nil {
		t.Fatal(err)
	}
	if err := persist(); err != nil {
		t.Fatal(err)
	}
	primary, found, err = repo.FindItemByFullName(1, 26, "gis/roads.shp")
	if err != nil || !found {
		t.Fatalf("primary missing: %v", err)
	}
	if primary.ID != original.ID || primary.Fingerprint != original.Fingerprint || primary.ScannedDepth != "basic" {
		t.Fatal("primary identity changed or old deep state retained")
	}
	var after int64
	if err := tx.Model(&models.MetaItem{}).Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != 7 {
		t.Fatalf("remaining item count = %d, want 7", after)
	}
	var missing []string
	if err := tx.Raw(`SELECT source_identity FROM meta.data_item_changes WHERE operation = 'missing' ORDER BY source_identity`).Scan(&missing).Error; err != nil {
		t.Fatal(err)
	}
	if len(missing) != 2 {
		t.Fatalf("missing events = %v, want exactly two claimed components", missing)
	}
}
