package repository

import (
	"os"
	"testing"
	"time"

	"github.com/addp/meta/internal/models"
	metaMigrations "github.com/addp/meta/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNodeStatisticsMigrationAgainstPostgres(t *testing.T) {
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
	if err := tx.Exec("DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("ALTER TABLE meta.meta_node ADD COLUMN item_count INTEGER DEFAULT 999, ADD COLUMN total_size_bytes BIGINT DEFAULT 999").Error; err != nil {
		t.Fatal(err)
	}
	migration, err := metaMigrations.FS.ReadFile("024_drop_node_scan_statistics.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := tx.Exec(string(migration)).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.AutoMigrate(&models.MetaNode{}); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"item_count", "total_size_bytes"} {
		if tx.Migrator().HasColumn(&models.MetaNode{}, column) {
			t.Fatalf("redundant column recreated: %s", column)
		}
	}
	for _, index := range []string{"idx_meta_node_active_parent", "idx_meta_item_active_node"} {
		var count int64
		if err := tx.Raw("SELECT count(*) FROM pg_indexes WHERE schemaname = 'meta' AND indexname = ?", index).Scan(&count).Error; err != nil || count != 1 {
			t.Fatalf("index %s: %d, %v", index, count, err)
		}
	}
	assertNodeStatisticsLifecycle(t, tx)
	assertNodeStatisticsScale(t, tx)
}

func assertNodeStatisticsScale(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Keep all synthetic data within this gate's rollback transaction.
	for _, sql := range []string{
		"SET LOCAL statement_timeout = '10s'",
		`INSERT INTO meta.meta_node(id, tenant_id, engine_id, node_type, name, depth) VALUES (100, 7, 20, 'bucket', 'scale', 0)`,
		`INSERT INTO meta.meta_node(id, tenant_id, engine_id, parent_node_id, node_type, name, depth)
		 SELECT n, 7, 20, 100, 'prefix', 'dir-' || n, 1 FROM generate_series(1000, 2999) n`,
		`INSERT INTO meta.meta_item(id, tenant_id, engine_id, node_id, item_type, name, fingerprint, size_bytes)
		 SELECT n, 7, 20, 1000 + (n % 2000), 'object', 'item-' || n, 'scale-' || n, 10
		 FROM generate_series(1000, 100999) n`,
		"ANALYZE meta.meta_node",
		"ANALYZE meta.meta_item",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	ids := []uint{100}
	for id := uint(1000); id < 1020; id++ {
		ids = append(ids, id)
	}
	start := time.Now()
	stats, err := QueryNodeStatistics(db, 7, 20, ids)
	if err != nil || len(stats) != len(ids) {
		t.Fatalf("scale query: %d rows, %v", len(stats), err)
	}
	for _, stat := range stats {
		count := 50
		if stat.NodeID == 100 {
			count = 100000
		}
		if stat.ItemCount != count || stat.TotalSizeBytes != int64(count*10) {
			t.Fatalf("scale statistics: %#v", stat)
		}
	}
	t.Logf("batch subtree statistics: 2001 nodes, 100000 items, 21 requested roots, %s", time.Since(start))
	var plan []string
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+nodeStatisticsSQL, 7, 20, ids, 7, 20, 7, 20).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, line := range plan {
		t.Log(line)
	}
}
