package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresGraphConcurrentClaimAndStart(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(graphRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS graph").Error; err != nil {
		t.Fatalf("create graph schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.Ontology{}, &models.KnowledgeGraph{}, &models.BuildTask{}, &models.BuildMaterial{}, &models.ReviewItem{}); err != nil {
		t.Fatalf("migrate graph build tables: %v", err)
	}

	tenantID := uint(time.Now().UnixNano()%100000000 + 920000000)
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.ReviewItem{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.BuildMaterial{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.BuildTask{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.KnowledgeGraph{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Ontology{}).Error
	})
	ontology := models.Ontology{TenantID: tenantID, Name: "integration-ontology", Status: "active", Metadata: []byte(`{}`)}
	if err := db.Create(&ontology).Error; err != nil {
		t.Fatalf("create integration ontology: %v", err)
	}
	graph := models.KnowledgeGraph{
		TenantID: tenantID, OntologyID: ontology.ID, EngineID: 1, Database: "neo4j",
		Name: "integration-graph", Status: "active", Stats: []byte(`{}`),
	}
	if err := db.Create(&graph).Error; err != nil {
		t.Fatalf("create integration knowledge graph: %v", err)
	}
	task := models.BuildTask{
		TenantID: tenantID, GraphID: graph.ID, Name: "kg-build", Status: models.BuildStatusPending,
		ConfidenceThreshold: 0.7, ChunkSize: 1000, ChunkOverlap: 200, DocContextSize: 200, Stats: []byte(`{}`),
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create integration build task: %v", err)
	}
	material := models.BuildMaterial{
		TaskID: task.ID, TenantID: tenantID, GraphID: graph.ID, Type: "document",
		FileName: "source.txt", FilePath: "build/source.txt", Status: models.BuildMaterialStatusPending, Stats: []byte(`{}`),
	}
	if err := db.Create(&material).Error; err != nil {
		t.Fatalf("create integration build material: %v", err)
	}
	repo := NewBuildRepository(db)
	createdAt := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("graph-pg-%d-a", tenantID),
		fmt.Sprintf("graph-pg-%d-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-start
			_, _, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, task.GraphID, tenantID,
				newGraphRepositoryTestExecution(executionID, int(tenantID), createdAt), BuildExecutionClaimRun,
			)
			results <- claimErr
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		claimErr := <-results
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Where("tenant_id = ? AND module = ? AND task_type = ?", tenantID, commonExecution.ModuleGraph, commonExecution.TaskTypeKGBuild).
		Find(&executions).Error; err != nil {
		t.Fatalf("load claimed executions: %v", err)
	}
	if len(executions) != 1 || executions[0].Status != commonExecution.ExecutionStatusPending || executions[0].StartedAt != nil {
		t.Fatalf("claimed executions = %#v", executions)
	}
	startedAt := createdAt.Add(time.Second)
	if err := repo.StartExecution(context.Background(), task.ID, tenantID, executions[0].ExecutionID, startedAt); err != nil {
		t.Fatalf("start claimed execution: %v", err)
	}
	var storedTask models.BuildTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load started task: %v", err)
	}
	if storedTask.ExecutionID != executions[0].ExecutionID || storedTask.Status != models.BuildStatusRunning || storedTask.StartedAt == nil {
		t.Fatalf("started graph task = %#v", storedTask)
	}
	completedAt := startedAt.Add(time.Second)
	if err := repo.FinishExecution(context.Background(), task.ID, tenantID, executions[0].ExecutionID,
		map[string]interface{}{"status": models.BuildStatusSuccess, "completed_at": completedAt},
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}); err != nil {
		t.Fatalf("finish initial execution: %v", err)
	}
	if err := db.Model(&models.BuildMaterial{}).Where("id = ?", material.ID).Updates(map[string]interface{}{
		"status": models.BuildMaterialStatusCompleted, "processed_chunks": 3, "total_chunks": 3,
	}).Error; err != nil {
		t.Fatalf("prepare completed material: %v", err)
	}
	review := models.ReviewItem{
		TaskID: task.ID, MaterialID: material.ID, TenantID: tenantID, GraphID: graph.ID,
		ItemType: models.ReviewItemEntity, Content: []byte(`{}`), Confidence: 0.5, Status: models.ReviewStatusPending,
	}
	if err := db.Create(&review).Error; err != nil {
		t.Fatalf("create pending review: %v", err)
	}

	rerunStart := make(chan struct{})
	rerunResults := make(chan error, 2)
	for _, executionID := range []string{
		fmt.Sprintf("graph-pg-%d-rerun-a", tenantID),
		fmt.Sprintf("graph-pg-%d-rerun-b", tenantID),
	} {
		executionID := executionID
		go func() {
			<-rerunStart
			_, _, claimErr := repo.ClaimExecution(
				context.Background(), task.ID, task.GraphID, tenantID,
				newGraphRepositoryTestExecution(executionID, int(tenantID), completedAt.Add(time.Second)), BuildExecutionClaimRerun,
			)
			rerunResults <- claimErr
		}()
	}
	close(rerunStart)
	rerunSuccesses, rerunConflicts := 0, 0
	for range 2 {
		claimErr := <-rerunResults
		switch {
		case claimErr == nil:
			rerunSuccesses++
		case errors.Is(claimErr, commonAPI.ErrConflict):
			rerunConflicts++
		default:
			t.Fatalf("concurrent rerun claim error: %v", claimErr)
		}
	}
	if rerunSuccesses != 1 || rerunConflicts != 1 {
		t.Fatalf("rerun claim results success=%d conflict=%d, want 1/1", rerunSuccesses, rerunConflicts)
	}
	var resetMaterial models.BuildMaterial
	if err := db.First(&resetMaterial, material.ID).Error; err != nil {
		t.Fatalf("load reset material: %v", err)
	}
	if resetMaterial.Status != models.BuildMaterialStatusPending || resetMaterial.ProcessedChunks != 0 || resetMaterial.TotalChunks != 0 {
		t.Fatalf("rerun material was not reset atomically: %#v", resetMaterial)
	}
	var pendingReviewCount int64
	if err := db.Model(&models.ReviewItem{}).Where("id = ?", review.ID).Count(&pendingReviewCount).Error; err != nil {
		t.Fatalf("count pending review: %v", err)
	}
	if pendingReviewCount != 0 {
		t.Fatalf("pending review count = %d, want 0", pendingReviewCount)
	}
}

func graphRepositoryIntegrationDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s search_path=graph,public",
		graphRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		graphRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		graphRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		graphRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		graphRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		graphRepositoryIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
}

func graphRepositoryIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
