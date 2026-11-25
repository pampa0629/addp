package repository

import (
	"errors"
	"os"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

func TestInstalledGraphOntology(t *testing.T) {
	if os.Getenv("ADDP_TEST_GRAPH_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Graph deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "graph"}, "graph", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewOntologyRepository(db)
	ontology := models.Ontology{TenantID: 7, Name: "订单本体"}
	if err := repo.Create(&ontology); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.GetDetail(ontology.ID, 7)
	if err != nil || detail.Name != ontology.Name {
		t.Fatalf("ontology details: %v %v", detail, err)
	}
	if _, err := repo.GetByID(ontology.ID, 8); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("other tenant read: %v", err)
	}
	graphRepo := NewKnowledgeGraphRepository(db)
	target := models.KnowledgeGraph{TenantID: 7, OntologyID: ontology.ID, EngineID: 42, Name: "订单图谱", IndexManagement: models.GraphIndexesExternal, CatalogPath: plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 42, Segments: []plugin.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "graph", Kind: "graph", Name: "orders"}}}}
	if err := graphRepo.Create(&target); err != nil {
		t.Fatal(err)
	}
	copy := target
	copy.ID = 0
	copy.Name = "同一目标"
	if err := graphRepo.Create(&copy); !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("duplicate target: %v", err)
	}
	stored, err := graphRepo.GetByID(target.ID, 7)
	if err != nil || stored.OntologyID != ontology.ID {
		t.Fatalf("graph target: %v %v", stored, err)
	}
	t.Logf("ontology %d graph %d: same-tenant read, other-tenant denial and unique graph target", ontology.ID, target.ID)
}
