package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	auth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/runtimelog"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLogSourceReportRequiresObserverPermissionAndBoundNode(t *testing.T) {
	t.Setenv("ADDP_HOST_NODE_NAME", "host")
	for _, tc := range []struct {
		name, client, permission, node string
		want                           int
	}{
		{"observer", "addp-log-observer", "system.module_log_source.create", "host", 200},
		{"wrong client", "addp-manager", "system.module_log_source.create", "host", 403},
		{"no permission", "addp-log-observer", "system.runtime_registry.update", "host", 403},
		{"foreign node", "addp-log-observer", "system.module_log_source.create", "foreign", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.AutoMigrate(&models.ModuleLogSource{}, &models.ModuleLogSourceNode{}, &models.ModuleLogSourceBoot{}); err != nil {
				t.Fatal(err)
			}
			h := NewModuleRegistryHandler(service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db)), []byte("test-key"))
			a := testIAMServiceActorContext("platform", tc.client)
			a.Authorization.RoleAssignments = []auth.RoleAssignment{{AssignmentID: "901", RoleKey: "platform.log_observer_runtime", Scope: auth.AssignmentScope{Type: "platform"}, Permissions: []string{tc.permission}, SourceType: "bootstrap", ValidFrom: a.Token.IssuedAt.Add(-time.Second)}}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if err := sharedauth.SetAuthContextForGin(c, a); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			contextGuard, err := middleware.NewIAMServiceContextGuard("platform")
			if err != nil {
				t.Fatal(err)
			}
			permissionGuard, err := middleware.NewIAMPermissionGuard("system.module_log_source.create")
			if err != nil {
				t.Fatal(err)
			}
			r.POST("/report", contextGuard, permissionGuard, sharedauth.MustNewServiceClientGuard("addp-log-observer"), h.ReportLogSources)
			report := runtimelog.SourceReport{Schema: runtimelog.SourceSchema, Node: tc.node, BootID: "boot", Sequence: 1, SampledAt: time.Now(), Complete: true, Issues: []runtimelog.SourceIssue{}, Sources: []runtimelog.Source{}}
			body, _ := json.Marshal(report)
			request := httptest.NewRequest(http.MethodPost, "/report", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			if w.Code != tc.want {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
		})
	}
}
