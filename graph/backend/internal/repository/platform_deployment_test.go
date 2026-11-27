package repository

import (
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/graph/internal/models"
	"github.com/google/uuid"
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

	t.Run("DeleteOnlyAfterBuildFinishes", func(t *testing.T) {
		task := models.BuildTask{TenantID: target.TenantID, GraphID: target.ID, Name: "构建订单图谱"}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		source := strconv.FormatUint(uint64(task.ID), 10)
		run := execution.TaskExecution{TenantID: int(target.TenantID), ExecutionID: uuid.NewString(),
			Module: execution.ModuleGraph, Source: execution.ModuleGraph, SourceTaskID: &source,
			TaskType: execution.TaskTypeKGBuild, Status: execution.ExecutionStatusRunning, TriggerType: execution.TriggerTypeManual}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		if err := graphRepo.DeleteDefinition(t.Context(), target.ID, target.TenantID, target.Version); !errors.Is(err, ErrGraphReconciliationIncomplete) {
			t.Fatalf("delete during active build: %v", err)
		}
		if err := db.Model(&run).Update("status", execution.ExecutionStatusSuccess).Error; err != nil {
			t.Fatal(err)
		}
		if err := graphRepo.DeleteDefinition(t.Context(), target.ID, target.TenantID, target.Version); err != nil {
			t.Fatalf("delete after build finished: %v", err)
		}
		if _, err := graphRepo.GetByID(target.ID, target.TenantID); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("deleted graph lookup: %v", err)
		}
		if err := db.First(&execution.TaskExecution{}, run.ID).Error; err != nil {
			t.Fatalf("execution history after graph deletion: %v", err)
		}
	})
}
