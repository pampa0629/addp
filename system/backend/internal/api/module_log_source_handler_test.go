package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	t.Setenv("ADDP_HOST_NODE_NAME", "other-system-pod")
	t.Setenv("ADDP_LOG_OBSERVER_NODES", "host,foreign")
	for _, tc := range []struct {
		name, client, permission, node string
		want                           int
	}{
		{"observer", "addp-log-observer-host", "system.module_log_source.create", "host", 200},
		{"wrong client", "addp-manager", "system.module_log_source.create", "host", 403},
		{"no permission", "addp-log-observer-host", "system.runtime_registry.update", "host", 403},
		{"foreign node", "addp-log-observer-host", "system.module_log_source.create", "foreign", 400},
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
			r.POST("/report", contextGuard, permissionGuard, h.ReportLogSources)
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

func TestLogProbeReplicasBindAuthenticatedNode(t *testing.T) {
	const instance = "1bd81c64-8ca8-413b-b321-481b9b865fe1"
	const marker = "addf8efe-eb1c-4aee-99c7-104c3606bdec"
	const peerMarker = "666870a2-48a8-4777-a644-bdbd171a71ee"
	t.Setenv("ADDP_OBSERVABILITY_LOGS_ENABLED", "true")
	t.Setenv("ADDP_LOG_OBSERVER_NODES", "addp-102,addp-138")
	t.Setenv("LOKI_READ_TOKEN", "central-only")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer central-only" {
			t.Error("wrong storage credential")
		}
		values := [][]string{}
		q := r.URL.Query()
		if !strings.Contains(q.Get("query"), `module_name="runtime-probe"`) || !strings.Contains(q.Get("query"), `instance_id="`+instance+`"`) {
			t.Error("unrestricted query")
		}
		for node, stored := range map[string]string{"addp-102": peerMarker, "addp-138": marker} {
			if strings.Contains(q.Get("query"), `node_name="`+node+`"`) && strings.Contains(q.Get("query"), stored) {
				ts := time.Now().Add(-time.Second)
				entry, _ := json.Marshal(runtimelog.Entry{Timestamp: ts.UTC().Format(time.RFC3339Nano), ID: instance + ":1", Module: "runtime-probe", Node: node, InstanceID: instance, Message: "runtime-log-delivery-probe " + stored})
				values = append(values, []string{strconv.FormatInt(ts.UnixNano(), 10), string(entry)})
			}
		}
		json.NewEncoder(w).Encode(gin.H{"status": "success", "data": gin.H{"resultType": "streams", "result": []any{gin.H{"values": values}}}})
	}))
	defer backend.Close()
	t.Setenv("LOKI_URL", backend.URL)
	for _, host := range []string{"addp-102", "addp-138"} {
		t.Setenv("ADDP_HOST_NODE_NAME", host)
		h := NewModuleRegistryHandler(nil, []byte("test-key"))
		for _, tc := range []struct {
			client, marker, extra string
			status                int
			found                 bool
		}{
			{"addp-log-observer-addp-138", marker, "", 200, true},
			{"addp-log-observer-addp-138", peerMarker, "", 200, false},
			{"addp-log-observer-addp-102", peerMarker, "", 200, true},
			{"addp-log-observer-addp-102", marker, "", 200, false},
			{"addp-log-observer-addp-138", peerMarker, `,"node":"addp-102"`, 400, false},
			{"addp-log-observer", marker, "", 403, false},
			{"addp-log-observer-foreign", marker, "", 403, false},
		} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			actor := testIAMServiceActorContext("platform", tc.client)
			if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
				t.Fatal(err)
			}
			c.Request = httptest.NewRequest("POST", "/confirm", strings.NewReader(`{"instance_id":"`+instance+`","marker":"`+tc.marker+`"`+tc.extra+`}`))
			h.ConfirmLogProbe(c)
			if w.Code != tc.status {
				t.Fatalf("%s %s status %d: %s", host, tc.client, w.Code, w.Body.String())
			}
			if w.Code == 200 && strings.TrimSpace(w.Body.String()) != fmt.Sprintf(`{"delivered":%t}`, tc.found) {
				t.Fatalf("unexpected result: %s", w.Body.String())
			}
		}
	}
}
