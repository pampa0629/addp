package service

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIntegrationPostgresGraphCleanupRuntime(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard Graph PostgreSQL gate required")
	}
	testGraphCleanupRuntime(t, true)
}
func cleanupRuntimeDomainFixture(t *testing.T, pg bool) (*gorm.DB, models.BuildTask, *execution.TaskExecution) {
	t.Helper()
	if !pg {
		_, db, task, build := queuedCancelFixture(t)
		if err := db.Exec("ATTACH DATABASE ':memory:' AS graph").Error; err != nil {
			t.Fatal(err)
		}
		return db, task, build
	}
	value := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=graph,public", value("ADDP_TEST_POSTGRES_HOST", "localhost"), os.Getenv("ADDP_TEST_POSTGRES_PORT"), value("ADDP_TEST_POSTGRES_USER", "addp"), os.Getenv("ADDP_TEST_POSTGRES_PASSWORD"), value("ADDP_TEST_POSTGRES_DATABASE", "addp_test"))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open graph fixture failed")
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = pool.Close() })
	if err = db.Exec("CREATE SCHEMA IF NOT EXISTS graph").Error; err != nil {
		t.Fatal(err)
	}
	if err = execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = repository.MigrateGraphTables(db); err != nil {
		t.Fatal(err)
	}
	tenant := uint(760000000 + time.Now().UnixNano()%10000000)
	t.Cleanup(func() {
		if e := db.Table("graph.cleanup_results").Where("tenant_id = ?", tenant).Delete(map[string]interface{}{}).Error; e != nil {
			t.Error(e)
		}
		for _, model := range []interface{}{&models.GraphOperation{}, &models.BuildExecutionControl{}, &models.ReviewItem{}, &models.BuildMaterial{}, &models.BuildTask{}, &models.KnowledgeGraph{}, &models.Ontology{}, &models.TenantAdmissionLock{}, &execution.Event{}, &execution.TaskExecution{}} {
			if e := db.Where("tenant_id = ?", tenant).Delete(model).Error; e != nil {
				t.Error(e)
			}
			var left int64
			if e := db.Model(model).Where("tenant_id = ?", tenant).Count(&left).Error; e != nil || left != 0 {
				t.Errorf("fixture remains: %T count=%d err=%v", model, left, e)
			}
		}
	})
	ontology := models.Ontology{TenantID: tenant, Name: "cleanup-pg", Status: "active", Metadata: []byte(`{}`)}
	if err = db.Create(&ontology).Error; err != nil {
		t.Fatal(err)
	}
	kg := models.KnowledgeGraph{TenantID: tenant, OntologyID: ontology.ID, EngineID: 1, Name: "cleanup-pg", Status: "active", IndexManagement: models.GraphIndexesExternal, Stats: []byte(`{}`), CatalogPath: plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 1, Segments: []plugin.EngineCatalogSegment{{Term: "server", Kind: "server", Name: ""}, {Term: "graph", Kind: "graph", Name: "cleanup-fixture"}}}}
	if err = db.Create(&kg).Error; err != nil {
		t.Fatal(err)
	}
	task := models.BuildTask{TenantID: tenant, GraphID: kg.ID, Name: "cleanup-pg", Status: models.BuildStatusPending, ConfidenceThreshold: 0.7, ChunkSize: 1000, ChunkOverlap: 200, DocContextSize: 200, Stats: []byte(`{}`)}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	mat := models.BuildMaterial{TenantID: tenant, TaskID: task.ID, GraphID: kg.ID, Type: "document", FileName: "source.txt", FilePath: "build/source.txt", Status: models.BuildMaterialStatusPending, Stats: []byte(`{}`)}
	if err = db.Create(&mat).Error; err != nil {
		t.Fatal(err)
	}
	build := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: "graph", TaskType: "kg_build", Source: "graph", Status: "pending", TriggerType: "manual"}
	ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext(strconv.FormatUint(uint64(tenant), 10), "7", nil))
	if _, _, err = repository.NewBuildRepository(db).EnqueueExecution(ctx, task.ID, kg.ID, tenant, build, repository.BuildExecutionClaimRun, []models.FrozenBuildMaterial{{Material: mat, SHA256: strings.Repeat("a", 64), Size: 1}}, 1, kg); err != nil {
		t.Fatal(err)
	}
	return db, task, build
}
