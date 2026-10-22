package api

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

func TestScanTaskFactsRuntimeGuardAndReadOnlyContract(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := db.Exec(`CREATE TABLE meta.scan_tasks (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, engine_id INTEGER NOT NULL, scope TEXT, parameters TEXT, deleted_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	const taskID uint = 42
	if err := db.Exec(`INSERT INTO meta.scan_tasks(id,tenant_id,engine_id,scope,parameters) VALUES(42,7,9,'{"type":"engine"}','{"scan_depth":"basic","force":false,"password":"must-not-leak"}')`).Error; err != nil {
		t.Fatal(err)
	}
	h := &Handler{executionService: service.NewScanExecutionService(db, nil, nil, nil)}
	for _, tc := range []struct {
		client, tenant, token, permission, suffix, body string
		want                                            int
	}{
		{"addp-system", "7", "service_access_token", "meta.task_provider.read", "", "", 200},
		{"addp-system", "8", "service_access_token", "meta.task_provider.read", "", "", 404},
		{"addp-system", "7", "service_access_token", "meta.task_provider.read", "?engine_id=9", "", 400},
		{"addp-system", "7", "service_access_token", "meta.task_provider.read", "", "{}", 400},
		{"addp-system", "7", "service_access_token", "", "", "", 403},
		{"addp-orchestrator", "7", "service_access_token", "meta.task_provider.read", "", "", 403},
		{"addp-meta", "7", "service_access_token", "meta.task_provider.read", "", "", 403},
		{"addp-web", "7", "first_party_access_token", "meta.task_provider.read", "", "", 403},
	} {
		t.Run(tc.client+tc.tenant+tc.suffix+tc.body, func(t *testing.T) {
			r := gin.New()
			projection := authorizationFactsProjection(tc.client, tc.tenant, tc.token, tc.permission)
			r.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
				if err := commonAuth.SetAuthContextForGin(c, projection); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			api := r.Group("/api/v1/meta")
			api.Use(commonAuth.MustNewContextGuard("tenant"))
			registerScanPlanFactsRoute(api, h)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/meta/runtime/scan-tasks/"+strconv.FormatUint(uint64(taskID), 10)+"/authorization-facts"+tc.suffix, strings.NewReader(tc.body)))
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			for _, secret := range []string{"must-not-leak", "password", "private display", "execution_config"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatal("private fact leaked", secret)
				}
			}
			if tc.want == 200 {
				var facts execution.MetaScanTaskFacts
				if err := facts.UnmarshalJSON(response.Body.Bytes()); err != nil || facts.TaskID != strconv.FormatUint(uint64(taskID), 10) || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("invalid/cached facts", err)
				}
			}
		})
	}
}
