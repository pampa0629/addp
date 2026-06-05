package metatest

import (
	"testing"

	"gorm.io/gorm"
)

func TestOpenMetadataDBCreatesNodeAndItemTablesByDefault(t *testing.T) {
	db := OpenMetadataDB(t)

	if !metadataTableExists(t, db, "meta_node") {
		t.Fatal("meta.meta_node table missing")
	}
	if !metadataTableExists(t, db, "meta_item") {
		t.Fatal("meta.meta_item table missing")
	}
}

func TestOpenMetadataDBCanSkipItemTable(t *testing.T) {
	db := OpenMetadataDB(t, WithoutMetaItemTable())

	if !metadataTableExists(t, db, "meta_node") {
		t.Fatal("meta.meta_node table missing")
	}
	if metadataTableExists(t, db, "meta_item") {
		t.Fatal("meta.meta_item table should not exist")
	}
}

func metadataTableExists(t *testing.T, db interface {
	Raw(string, ...interface{}) *gorm.DB
}, table string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT count(*) FROM meta.sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
		t.Fatalf("query metadata sqlite_master: %v", err)
	}
	return count > 0
}
