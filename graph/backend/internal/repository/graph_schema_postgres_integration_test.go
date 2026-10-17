package repository

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/graph/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresGraphTargetContract(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard PostgreSQL T2 gate required")
	}
	db, err := gorm.Open(postgres.Open(graphRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	name := fmt.Sprintf("graph_contract_%d", time.Now().UnixNano())
	if err = db.Exec("CREATE SCHEMA " + name).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + name + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	if err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO " + name + ",public").Error; err != nil {
			return err
		}
		// Empty old tables have their obsolete columns removed, never retained as a second contract.
		if err := tx.Exec("CREATE TABLE knowledge_graphs(id bigserial PRIMARY KEY,database text NOT NULL)").Error; err != nil {
			return err
		}
		if err := MigrateGraphTables(tx); err != nil {
			return err
		}
		if tx.Migrator().HasColumn("knowledge_graphs", "database") || tx.Migrator().HasColumn("review_items", "neo4j_id") {
			return fmt.Errorf("obsolete graph columns retained")
		}
		path := plugin.EngineCatalogRootPath(plugin.EngineCatalogModelSpec{RootTerm: "server"}, 13)
		path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "graph", Kind: "graph", Name: "中文.a/b"})
		ontology := models.Ontology{TenantID: 7, Name: "contract"}
		if err := tx.Create(&ontology).Error; err != nil {
			return err
		}
		kg := models.KnowledgeGraph{TenantID: 7, OntologyID: ontology.ID, EngineID: 13, CatalogPath: path, IndexManagement: models.GraphIndexesExternal, Name: "contract"}
		if err := tx.Create(&kg).Error; err != nil {
			return err
		}
		loaded, err := NewKnowledgeGraphRepository(tx).GetByID(kg.ID, 7)
		if err != nil {
			return err
		}
		if loaded.CatalogPath.Segments[1].Name != "中文.a/b" || loaded.IndexManagement != models.GraphIndexesExternal {
			return fmt.Errorf("target roundtrip changed: %+v", loaded)
		}
		// The database itself arbitrates concurrent owners of one physical target.
		if err := tx.SavePoint("duplicate").Error; err != nil {
			return err
		}
		kg.ID = 0
		if err := tx.Create(&kg).Error; err == nil {
			return fmt.Errorf("duplicate physical graph target accepted")
		}
		if err := tx.RollbackTo("duplicate").Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// An incompatible populated old binding must fail atomically without guessing its ownership.
	if err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO " + name + ",public").Error; err != nil {
			return err
		}
		if err := tx.Exec("DROP TABLE knowledge_graphs CASCADE").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TABLE knowledge_graphs(id bigserial PRIMARY KEY,tenant_id bigint NOT NULL,ontology_id bigint NOT NULL,engine_id bigint NOT NULL,name text NOT NULL,database text NOT NULL); INSERT INTO knowledge_graphs(tenant_id,ontology_id,engine_id,name,database)VALUES(7,1,13,'old','user-data')").Error; err != nil {
			return err
		}
		if err := MigrateGraphTables(tx); err == nil {
			return fmt.Errorf("populated incompatible binding silently migrated")
		}
		var value string
		if err := tx.Raw("SELECT database FROM knowledge_graphs").Scan(&value).Error; err != nil {
			return err
		}
		if !strings.EqualFold(value, "user-data") {
			return fmt.Errorf("original binding was modified")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
