package api

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/graph/internal/config"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBuildCancellationHTTPKeepsAcceptedSeparateFromStopped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	if err = executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err = repository.MigrateGraphTables(db); err != nil {
		t.Fatal(err)
	}
	ontology := models.Ontology{TenantID: 7, Name: "cancel-http"}
	if err = db.Create(&ontology).Error; err != nil {
		t.Fatal(err)
	}
	kg := models.KnowledgeGraph{TenantID: 7, OntologyID: ontology.ID, EngineID: 1, Name: "cancel-http", IndexManagement: models.GraphIndexesExternal}
	if err = db.Create(&kg).Error; err != nil {
		t.Fatal(err)
	}
	task := models.BuildTask{TenantID: 7, GraphID: kg.ID, Name: "cancel-http", Status: models.BuildStatusPending}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	mat := models.BuildMaterial{TenantID: 7, GraphID: kg.ID, TaskID: task.ID, Type: "document", FilePath: "build/input", Status: models.BuildMaterialStatusPending}
	if err = db.Create(&mat).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewBuildRepository(db)
	e := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: execution.ModuleGraph, TaskType: execution.TaskTypeKGBuild, Source: execution.ModuleGraph, Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual}
	ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", nil))
	if _, _, err = repo.EnqueueExecution(ctx, task.ID, kg.ID, 7, e, repository.BuildExecutionClaimRun, []models.FrozenBuildMaterial{{Material: mat, SHA256: strings.Repeat("a", 64), Size: 1}}, 1, kg); err != nil {
		t.Fatal(err)
	}
	_, lease, runtime, _, err := repo.ClaimBuild(ctx, *e, "remote-backend", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewBuildService(repo, nil, nil, nil, nil, nil, nil, nil, "", nil)
	auth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer canceller": {"graph.build_task.cancel"}})
	defer auth.Close()
	router := SetupRouter(&config.Config{SystemServiceURL: auth.URL}, nil, nil, nil, NewBuildHandler(svc), nil, nil, nil, modulelifecycle.NewStandalone("graph"))
	send := func(body string, expected int, state string) {
		t.Helper()
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/graph/graphs/%d/build/tasks/%d/cancel", kg.ID, task.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer canceller")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != expected || state != "" && !strings.Contains(w.Body.String(), `"status":"`+state+`"`) {
			t.Fatalf("cancel got %d %s", w.Code, w.Body.String())
		}
	}
	send(`{}`, 400, "")
	send(fmt.Sprintf(`{"execution_id":%q,"attempt":1,"force":true}`, e.ExecutionID), 400, "")
	send(fmt.Sprintf(`{"execution_id":%q,"attempt":0}`, e.ExecutionID), 409, "")
	body := fmt.Sprintf(`{"execution_id":%q,"attempt":1}`, e.ExecutionID)
	send(body, 202, "running")
	if err = repo.CompleteBuild(execution.ContextWithLease(ctx, *lease), runtime, execution.ExecutionStatusSuccess, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	send(body, 200, "cancelled")
}
