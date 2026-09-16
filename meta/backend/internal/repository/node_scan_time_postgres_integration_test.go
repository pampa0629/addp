package repository

import (
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/addp/meta/internal/models"
	metaMigrations "github.com/addp/meta/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNodeScanTimeMigrationAgainstPostgres(t *testing.T) {
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
	if err := tx.AutoMigrate(&models.MetaNode{}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, status := range []string{"completed", "failed", "running", "pending"} {
		node := models.MetaNode{ID: uint(i + 1), TenantID: 1, EngineID: 9, NodeType: "bucket", Name: status, ScanStatus: status, ScannedDepth: "basic"}
		if status != "pending" {
			node.ScannedAt = &at
		}
		if err := tx.Create(&node).Error; err != nil {
			t.Fatal(err)
		}
	}
	const name = "025_clear_unsuccessful_node_scan_times.sql"
	migration, err := metaMigrations.FS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	fs := fstest.MapFS{name: &fstest.MapFile{Data: migration}}
	if err := runSQLMigrations(tx, fs, "."); err != nil {
		t.Fatal(err)
	}
	var nodes []models.MetaNode
	if err := tx.Order("id").Find(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if (node.ScannedAt != nil) != (node.ScanStatus == "completed") || node.ScannedDepth != "basic" {
			t.Fatalf("unexpected migration facts: %#v", node)
		}
	}
	// A later successful scan followed by failure must survive future restarts.
	repo := NewScanRepository(tx)
	if err := repo.FinalizeNodeStateWithDepth(&nodes[1], "completed", "", "deep"); err != nil {
		t.Fatal(err)
	}
	if err := tx.First(&nodes[1], nodes[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	lastSuccess := *nodes[1].ScannedAt
	if err := repo.ResetNodeState(&nodes[1], "running"); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinalizeNodeState(&nodes[1], "failed", "failed again"); err != nil {
		t.Fatal(err)
	}
	if err := runSQLMigrations(tx, fs, "."); err != nil {
		t.Fatal(err)
	}
	var after models.MetaNode
	if err := tx.First(&after, nodes[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.ScannedAt == nil || !after.ScannedAt.Equal(lastSuccess) || after.ScannedDepth != "deep" {
		t.Fatalf("restart erased valid successful facts: %#v", after)
	}
}
