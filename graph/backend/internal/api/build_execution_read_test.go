package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/graph/internal/config"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBuildExecutionReadExcludesCleanupAndPrivateAuthority(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	auth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer reader": {"graph.build_task.read"}})
	defer auth.Close()
	cfg := &config.Config{}
	cfg.SystemServiceURL = auth.URL
	h := NewTaskProviderHandler(nil, execution.NewTaskExecutionRepository(db))
	router := SetupRouter(NewCleanupResultHandler(nil), cfg, nil, nil, nil, nil, h, nil, nil, modulelifecycle.NewStandalone("graph"))
	taskID, token, by := "17", "private-lease-token", 7
	base := execution.TaskExecution{TenantID: 7, Module: "graph", Source: "graph", Status: "pending", TriggerType: "manual", TriggeredBy: &by,
		SourceTaskID: &taskID, LeaseToken: &token, ExecutionConfig: commonmodels.JSONMap{"private_snapshot": "must-not-leak"}, Metadata: commonmodels.JSONMap{"cleanup_result": map[string]any{"statistics": "private-owner-details"}}}
	for _, test := range []struct {
		name, taskType string
		tenant         int
		adhoc          bool
		want           int
	}{
		{"cleanup", "cleanup_executor", 7, false, http.StatusNotFound},
		{"other_tenant", "kg_build", 8, false, http.StatusNotFound},
		{"build", "kg_build", 7, false, http.StatusOK},
		{"other_actor_adhoc", "kg_build", 7, true, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := base
			e.ExecutionID = uuid.NewString()
			e.TaskType = test.taskType
			e.TenantID = test.tenant
			if test.adhoc {
				other := 8
				e.SourceTaskID = nil
				e.TriggeredBy = &other
			}
			if err := db.Create(&e).Error; err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/graph/executions/"+e.ExecutionID, nil)
			r.Header.Set("Authorization", "Bearer reader")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, test.want, w.Body.String())
			}
			for _, private := range []string{"lease_token", "execution_config", "cleanup_result", "private-owner-details", "must-not-leak"} {
				if strings.Contains(w.Body.String(), private) {
					t.Fatalf("private field %s leaked", private)
				}
			}
		})
	}
}
