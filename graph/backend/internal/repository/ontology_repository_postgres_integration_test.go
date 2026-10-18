package repository

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/graph/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresGraphOntologyDeletion(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard PostgreSQL T2 gate required")
	}
	db, err := gorm.Open(postgres.Open(graphRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	name := fmt.Sprintf("graph_delete_%d", time.Now().UnixNano())
	if err := db.Exec("CREATE SCHEMA " + name).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + name + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO " + name + ",public").Error; err != nil {
			return err
		}
		if err := MigrateGraphTables(tx); err != nil {
			return err
		}
		ontology := models.Ontology{TenantID: 7, Name: "delete aggregate"}
		control := models.Ontology{TenantID: 8, Name: "foreign aggregate"}
		sibling := models.Ontology{TenantID: 7, Name: "same tenant aggregate"}
		for _, row := range []*models.Ontology{&ontology, &control, &sibling} {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		parent := models.EntityType{TenantID: 7, OntologyID: ontology.ID, Name: "Person"}
		if err := tx.Create(&parent).Error; err != nil {
			return err
		}
		child := models.EntityType{TenantID: 7, OntologyID: ontology.ID, Name: "Employee", ParentID: &parent.ID}
		foreign := models.EntityType{TenantID: 8, OntologyID: control.ID, Name: "Unchanged"}
		neighbor := models.EntityType{TenantID: 7, OntologyID: sibling.ID, Name: "Unchanged sibling"}
		for _, row := range []*models.EntityType{&child, &foreign, &neighbor} {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		relation := models.RelationType{TenantID: 7, OntologyID: ontology.ID, Name: "Link", SourceTypeID: &parent.ID, TargetTypeID: &child.ID}
		version := models.OntologyVersion{TenantID: 7, OntologyID: ontology.ID, Version: "1", Snapshot: []byte(`{}`)}
		for _, row := range []any{&relation, &version} {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		repo := NewOntologyRepository(tx)
		check := func(deleted bool) error {
			for _, item := range []struct {
				model any
				where string
				id    uint
				count int64
			}{
				{&models.Ontology{}, "id = ?", ontology.ID, 1},
				{&models.EntityType{}, "ontology_id = ?", ontology.ID, 2},
				{&models.RelationType{}, "ontology_id = ?", ontology.ID, 1},
				{&models.OntologyVersion{}, "ontology_id = ?", ontology.ID, 1},
			} {
				want := item.count
				if deleted {
					want = 0
				}
				var count int64
				if err := tx.Model(item.model).Where(item.where, item.id).Count(&count).Error; err != nil {
					return err
				}
				if count != want {
					return fmt.Errorf("aggregate %T count=%d, want %d", item.model, count, want)
				}
			}
			for _, untouched := range []models.EntityType{foreign, neighbor} {
				var count int64
				if err := tx.Model(&models.EntityType{}).Where("id = ? AND tenant_id = ?", untouched.ID, untouched.TenantID).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					return fmt.Errorf("another ontology definition was changed")
				}
			}
			return nil
		}
		if err := repo.Delete(ontology.ID, 8); err != nil {
			return err
		}
		if err := check(false); err != nil {
			return err
		}
		path := plugin.EngineCatalogRootPath(plugin.EngineCatalogModelSpec{RootTerm: "server"}, 13)
		path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "graph", Kind: "graph", Name: "delete-control"})
		binding := models.KnowledgeGraph{TenantID: 7, OntologyID: ontology.ID, EngineID: 13, CatalogPath: path, IndexManagement: models.GraphIndexesExternal, Name: "keep bound graph"}
		if err := tx.Create(&binding).Error; err != nil {
			return err
		}
		if err := repo.Delete(ontology.ID, 7); err == nil {
			return fmt.Errorf("bound graph no longer protects its ontology")
		}
		if err := check(false); err != nil {
			return fmt.Errorf("failed deletion did not roll back definitions and version: %w", err)
		}
		if err := tx.Delete(&binding).Error; err != nil {
			return err
		}
		if err := repo.Delete(ontology.ID, 7); err != nil {
			return err
		}
		if err := check(true); err != nil {
			return err
		}
		return repo.Delete(ontology.ID, 7)
	}); err != nil {
		t.Fatal(err)
	}
}
