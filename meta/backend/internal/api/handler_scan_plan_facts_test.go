package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

func TestScanPlanFactsRuntimeGuardAndUnavailableContract(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	run := &execution.TaskExecution{TenantID: 7, ExecutionID: "23dd4f40-95b5-4cc1-a294-ded9f78d10a2", Module: "meta", TaskType: "scan", ExecutionBoundary: "bounded", Status: "pending", TriggerType: "manual", ExecutionConfig: commonmodels.JSONMap{"engine_id": 9, "scan_depth": "deep", "password": "secret"}}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	handler := &Handler{executionService: service.NewScanExecutionService(db, nil, nil, nil)}
	for _, test := range []struct {
		client, tenant, token, permission, suffix string
		want                                      int
	}{
		{"addp-system", "7", "service_access_token", "meta.task_provider.read", "", 409},
		{"addp-system", "8", "service_access_token", "meta.task_provider.read", "", 404},
		{"addp-system", "7", "service_access_token", "meta.task_provider.read", "?engine_id=9", 400},
		{"addp-system", "7", "service_access_token", "", "", 403},
		{"addp-catalog", "7", "service_access_token", "meta.task_provider.read", "", 403},
		{"addp-orchestrator", "7", "service_access_token", "meta.task_provider.read", "", 403},
		{"addp-meta", "7", "service_access_token", "meta.task_provider.read", "", 403},
		{"addp-web", "7", "first_party_access_token", "meta.task_provider.read", "", 403},
	} {
		t.Run(test.client+test.tenant+test.permission+test.suffix, func(t *testing.T) {
			router := gin.New()
			projection := authorizationFactsProjection(test.client, test.tenant, test.token, test.permission)
			router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
				if err := commonAuth.SetAuthContextForGin(c, projection); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			api := router.Group("/api/v1/meta")
			api.Use(commonAuth.MustNewContextGuard("tenant"))
			registerScanPlanFactsRoute(api, handler)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/meta/runtime/scan-executions/"+run.ExecutionID+"/authorization-facts"+test.suffix, nil))
			if response.Code != test.want || strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "execution_config") {
				t.Fatalf("unsafe/wrong response %d %s", response.Code, response.Body.String())
			}
			if test.want == 409 && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable facts failure")
			}
		})
	}
	// Empty typed scope cannot become an Allow or be widened to the engine.
	if err := db.Model(run).Update("execution_config", commonmodels.JSONMap{"engine_id": 9, "engine_version": "2", "scan_depth": "basic", "catalog_paths": []string{"unknown"}}).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	projection := authorizationFactsProjection("addp-system", "7", "service_access_token", "meta.task_provider.read")
	router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
		if err := commonAuth.SetAuthContextForGin(c, projection); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	api := router.Group("/api/v1/meta")
	api.Use(commonAuth.MustNewContextGuard("tenant"))
	registerScanPlanFactsRoute(api, handler)
	for _, body := range []string{"", "{}"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/meta/runtime/scan-executions/"+run.ExecutionID+"/authorization-facts", strings.NewReader(body)))
		want := 200
		if body != "" {
			want = 400
		}
		if response.Code != want {
			t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
		}
		if body == "" {
			var facts execution.MetaScanPlanFacts
			if err := facts.UnmarshalJSON(response.Body.Bytes()); err != nil || facts.SourceScope != nil || facts.BasisState != "incomplete" {
				t.Fatal("incomplete scope widened", err)
			}
		}
	}
}
