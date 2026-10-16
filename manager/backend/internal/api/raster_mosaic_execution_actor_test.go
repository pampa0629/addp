package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
	commonauth "github.com/addp/common/middleware/auth"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func testManagerMosaicExecutionActor(t *testing.T, db *gorm.DB) {
	t.Helper()
	if db.Dialector.Name() == "sqlite" {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
			t.Fatal(err)
		}
		ensureDerivedTaskDefinitionTestTables(t, db)
	} else {
		if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&models.TaskDefinition{}); err != nil {
			t.Fatal(err)
		}
	}
	const tenant = 710000093
	creator := uint(32)
	task := &models.RasterMosaicTask{TenantID: tenant, Name: "mosaic actor provenance", CreatedBy: &creator, Enabled: true, Config: commonmodels.JSONMap{"fixture": true}}
	repo := repository.NewRasterMosaicRepository(db)
	if err := repo.CreateTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "31", []string{"manager.derived_artifact.create"}))
	}))
	defer system.Close()
	executions := execution.NewTaskExecutionRepository(db)
	handler := NewTaskProviderHandler(nil, nil, nil, nil, executions, service.NewRasterMosaicTaskService(repo, executions))
	router := gin.New()
	router.Use(commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: system.URL}), commonauth.MustNewContextGuard("tenant"))
	router.POST("/tasks/:task_type/:id/execute", commonauth.MustNewPermissionGuard("manager.derived_artifact.create"), handler.TaskExecute)
	request := func(body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/tasks/raster_mosaic_generation/"+strconv.FormatUint(uint64(task.ID), 10)+"/execute", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer current-mosaic-user")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("mosaic execute status=%d want=%d body=%s", w.Code, status, w.Body)
		}
		return w
	}
	for _, body := range []string{`{"actor_principal_id":32}`, `{"triggered_by":32}`, `{"source":"orchestrator","parent_execution_id":"11111111-1111-4111-8111-111111111111"}`} {
		request(body, http.StatusBadRequest)
	}
	var accepted TaskExecuteResponse
	if err := json.Unmarshal(request("{}", http.StatusAccepted).Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	stored, err := executions.GetByExecutionID(t.Context(), accepted.ExecutionID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Source != execution.ModuleManager || stored.ParentExecutionID != nil || stored.TriggeredBy == nil || *stored.TriggeredBy != 31 || stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != 31 || stored.ActorTenantMembershipID == nil || *stored.ActorTenantMembershipID <= 0 || stored.IssuedAuthorizationVersion == nil || *stored.IssuedAuthorizationVersion <= 0 || stored.ExecutionAuthorizationID != nil {
		t.Fatal("mosaic execution did not persist the current authenticated actor independently of task creator")
	}
}
