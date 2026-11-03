package repository

import (
	"os"
	"testing"
	"testing/fstest"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/schema"
	"github.com/addp/meta/internal/models"
	metamigrations "github.com/addp/meta/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestWholeScopeMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard Meta PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
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
	repo := NewScanRepository(tx)
	node := func(tenant, engine uint, parent *models.MetaNode, kind, name, full string) *models.MetaNode {
		t.Helper()
		n, err := repo.UpsertNode(tenant, engine, parent, kind, name, &full, models.JSONMap{})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	root := node(7, 99, nil, "service", "Object store", "")
	bucket := node(7, 99, root, "bucket", "业务", "业务")
	parent := node(7, 99, bucket, "prefix", "results", "业务/results")
	makeItem := func(owner *models.MetaNode, name string) *models.MetaItem {
		t.Helper()
		full := "业务/results/" + name
		attrs := models.JSONMap{"item": map[string]any{"layout": "whole", "data_type": "table", "format": "parquet", "scope_exclusive": true, "claim_policy": "whole_scope"}, "storage": map[string]any{"physical_path": full}, "type_info": map[string]any{"table": map[string]any{"row_count": 2}}}
		rows := int64(2)
		i, err := repo.UpsertItemWithDepth(7, 99, owner, "object", name, full, attrs, &rows, nil, nil, "deep")
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	missingScope := makeItem(parent, "统计 %")
	scope := node(7, 99, parent, "prefix", "existing", "业务/results/existing")
	existingScope := makeItem(parent, "existing")
	canonicalScope := node(7, 99, parent, "prefix", "canonical", "业务/results/canonical")
	canonical := makeItem(canonicalScope, "canonical")
	internal := node(7, 99, scope, "prefix", "leaf", "业务/results/existing/leaf")
	if _, err := repo.UpsertItemWithDepth(7, 99, internal, "object", "fragment", "业务/results/existing/leaf/fragment", models.JSONMap{}, nil, nil, nil, "basic"); err != nil {
		t.Fatal(err)
	}
	sibling := node(7, 99, parent, "prefix", "existing-other", "业务/results/existing-other")
	if _, err := repo.UpsertItemWithDepth(7, 99, sibling, "object", "keep", "业务/results/existing-other/keep", models.JSONMap{}, nil, nil, nil, "basic"); err != nil {
		t.Fatal(err)
	}
	wrongTenant := node(8, 99, nil, "service", "Other tenant", "")
	broken := makeItem(wrongTenant, "broken")
	badHash := makeItem(parent, "bad-hash")
	if err := tx.Model(badHash).Update("fingerprint", "invalid").Error; err != nil {
		t.Fatal(err)
	}
	wrongPath := makeItem(parent, "wrong-path")
	if err := tx.Model(wrongPath).Update("full_name", "another/place").Error; err != nil {
		t.Fatal(err)
	}
	nestedRoot := node(7, 100, nil, "root", "NFS", "")
	gdb := node(7, 100, nestedRoot, "dir", "地籍.gdb", "地籍.gdb")
	gdbItem := models.MetaItem{TenantID: 7, EngineID: 100, NodeID: gdb.ID, ItemType: "file", Name: gdb.Name, FullName: gdb.FullName, Fingerprint: commonmodels.GenerateItemFingerprint(100, gdb.FullName), Attributes: models.JSONMap{"item": map[string]any{"layout": "whole", "data_type": "container", "format": "filegdb", "scope_exclusive": true, "claim_policy": "whole_scope"}, "storage": map[string]any{"physical_path": gdb.FullName}}}
	if err := tx.Create(&gdbItem).Error; err != nil {
		t.Fatal(err)
	}
	// The old explicit-refs file route also attached whole items to their parent.
	legacyFile := gdbItem
	legacyFile.ID, legacyFile.NodeID = 0, nestedRoot.ID
	legacyFile.Name, legacyFile.FullName = "旧成果.gdb", "旧成果.gdb"
	legacyFile.Fingerprint = commonmodels.GenerateItemFingerprint(100, legacyFile.FullName)
	legacyFile.Attributes = models.JSONMap{"item": map[string]any{"layout": "whole", "data_type": "container", "format": "filegdb", "scope_exclusive": true, "claim_policy": "whole_scope"}, "storage": map[string]any{"physical_path": legacyFile.FullName}}
	if err := tx.Create(&legacyFile).Error; err != nil {
		t.Fatal(err)
	}
	invalidScope := node(7, 99, parent, "prefix", "invalid-scope", "业务/results/invalid-scope")
	invalidWhole := makeItem(invalidScope, "invalid-scope")
	if err := tx.Model(invalidWhole).Update("attributes", models.JSONMap{"item": map[string]any{"layout": "whole"}}).Error; err != nil {
		t.Fatal(err)
	}
	// Exercise the real change feed: migrations must preserve the source identity
	// and publish missing facts for deleted malformed records, not re-create items.
	changeSQL, err := metamigrations.FS.ReadFile("019_add_data_item_changes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(string(changeSQL)).Error; err != nil {
		t.Fatal(err)
	}
	const name = "027_whole_scope_ownership.sql"
	sql, err := metamigrations.FS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	fs := fstest.MapFS{name: &fstest.MapFile{Data: sql}}
	// Production startup skips the entire callback at an unchanged revision.
	// Seed the previous deployed revision so this test also covers publication.
	if err := tx.Exec(`CREATE TABLE meta.startup_schema_revision
		(singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),version BIGINT NOT NULL);
		INSERT INTO meta.startup_schema_revision VALUES (TRUE,4)`).Error; err != nil {
		t.Fatal(err)
	}
	upgrade := func() error {
		return schema.Migrate(tx, "meta", SchemaVersion, func(db *gorm.DB) error {
			return runSQLMigrations(db, fs, ".")
		})
	}
	if err := upgrade(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Require(tx, "meta", SchemaVersion); err != nil {
		t.Fatal("Worker cannot observe completed whole migration", err)
	}
	if err := schema.Require(tx, "meta", 4); err == nil {
		t.Fatal("old Worker accepted the new schema revision")
	}
	for _, before := range []*models.MetaItem{missingScope, existingScope, canonical, &gdbItem, &legacyFile} {
		var after models.MetaItem
		if err := tx.First(&after, before.ID).Error; err != nil {
			t.Fatal(err)
		}
		var owner models.MetaNode
		if err := tx.First(&owner, after.NodeID).Error; err != nil {
			t.Fatal(err)
		}
		if after.ID != before.ID || after.Name != owner.Name || after.FullName != owner.FullName || after.Fingerprint != before.Fingerprint || after.ScannedDepth != before.ScannedDepth {
			t.Fatalf("migration changed logical identity or deep facts: %+v", after)
		}
		if before.ID == existingScope.ID && owner.ID != scope.ID {
			t.Fatal("migration did not reuse existing scope")
		}
	}
	for _, removed := range []*models.MetaItem{broken, badHash, wrongPath, invalidWhole} {
		var count int64
		if err := tx.Unscoped().Model(&models.MetaItem{}).Where("id=?", removed.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("unprovable whole survived: %+v %v", removed, err)
		}
	}
	var count int64
	if err := tx.Model(&models.MetaNode{}).Where("id=?", invalidScope.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("invalid whole became a grantable ordinary directory")
	}
	var operation string
	if err := tx.Raw("SELECT operation FROM meta.data_item_changes WHERE item_id=? ORDER BY id DESC LIMIT 1", broken.ID).Scan(&operation).Error; err != nil || operation != "missing" {
		t.Fatal("deleted whole did not publish missing fact", err)
	}
	if err := tx.Raw("SELECT operation FROM meta.data_item_changes WHERE item_id=? ORDER BY id DESC LIMIT 1", missingScope.ID).Scan(&operation).Error; err != nil || operation != "upsert" {
		t.Fatal("migrated whole lost source identity", err)
	}
	if err := tx.Model(&models.MetaNode{}).Where("id=?", internal.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("internal node survived")
	}
	if err := tx.Model(&models.MetaItem{}).Where("node_id=?", sibling.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("sibling scope changed")
	}
	if err := repo.HardDeleteInvalidEngineGraph(7, 99); err != nil {
		t.Fatal(err)
	}
	if err := tx.First(&models.MetaItem{}, missingScope.ID).Error; err != nil {
		t.Fatal("normal cleanup deleted migrated whole", err)
	}
	if err := tx.Model(&models.MetaItem{}).Where("id=?", missingScope.ID).Update("row_count", 9).Error; err != nil {
		t.Fatal(err)
	}
	if err := upgrade(); err != nil {
		t.Fatal(err)
	}
	var after models.MetaItem
	if err := tx.First(&after, missingScope.ID).Error; err != nil || after.RowCount == nil || *after.RowCount != 9 {
		t.Fatal("restart replayed migration", err)
	}
}
