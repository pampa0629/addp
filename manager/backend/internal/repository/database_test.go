package repository

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDropLegacyQuickViewTablesRemovesLegacyExecutionsAndTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:drop_legacy_quick_view?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`ATTACH DATABASE ':memory:' AS common`).Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`ATTACH DATABASE ':memory:' AS manager`).Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY,
			module TEXT NOT NULL,
			task_type TEXT NOT NULL
		)
	`).Error; err != nil {
		t.Fatalf("create task_executions: %v", err)
	}
	for _, tableName := range []string{
		"quick_view_optimization",
		"vector_quick_view_targets",
		"model_3d_quick_view",
		"gaussian_splat_quick_view",
	} {
		if err := db.Exec(`CREATE TABLE manager."` + tableName + `" (id INTEGER PRIMARY KEY)`).Error; err != nil {
			t.Fatalf("create legacy table %s: %v", tableName, err)
		}
	}
	if err := db.Exec(`
		INSERT INTO common.task_executions (module, task_type) VALUES
			('manager', 'quick_view_optimization'),
			('manager', 'model_3d_quick_view_generation'),
			('manager', 'gaussian_splat_quick_view_generation'),
			('manager', 'vector_quick_view_target_generation'),
			('manager', 'tile_cache_generation'),
			('manager', 'cog_artifact_generation'),
			('manager', 'mvt_generation'),
			('manager', 'model_3d_glb_generation'),
			('monitor', 'model_3d_quick_view_generation')
	`).Error; err != nil {
		t.Fatalf("insert executions: %v", err)
	}

	if err := dropLegacyQuickViewTables(db); err != nil {
		t.Fatalf("dropLegacyQuickViewTables returned error: %v", err)
	}

	var legacyExecutions int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM common.task_executions
		WHERE module = 'manager'
		  AND task_type IN (
			'quick_view_optimization',
			'quick_view_optimization_generation',
			'tile_cache_generation',
			'cog_artifact_generation',
			'mvt_generation',
			'vector_quick_view_target_generation',
			'model_3d_quick_view_generation',
			'gaussian_splat_quick_view_generation'
		  )
	`).Scan(&legacyExecutions).Error; err != nil {
		t.Fatalf("count legacy executions: %v", err)
	}
	if legacyExecutions != 0 {
		t.Fatalf("legacy manager executions = %d, want 0", legacyExecutions)
	}

	var remainingExecutions int64
	if err := db.Raw(`SELECT COUNT(*) FROM common.task_executions`).Scan(&remainingExecutions).Error; err != nil {
		t.Fatalf("count remaining executions: %v", err)
	}
	if remainingExecutions != 2 {
		t.Fatalf("remaining executions = %d, want 2", remainingExecutions)
	}

	for _, tableName := range []string{
		"quick_view_optimization",
		"vector_quick_view_targets",
		"model_3d_quick_view",
		"gaussian_splat_quick_view",
	} {
		var tableCount int64
		if err := db.Raw(`
			SELECT COUNT(*)
			FROM manager.sqlite_master
			WHERE type = 'table' AND name = ?
		`, tableName).Scan(&tableCount).Error; err != nil {
			t.Fatalf("count legacy table %s: %v", tableName, err)
		}
		if tableCount != 0 {
			t.Fatalf("legacy table %s still exists", tableName)
		}
	}
}
