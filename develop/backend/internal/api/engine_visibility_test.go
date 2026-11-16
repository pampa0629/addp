package api

import (
	"encoding/json"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueryEngineOptionsRequireCurrentUserDataVisibility(t *testing.T) {
	for _, endpoint := range []string{"/engines", "/source-engines"} {
		for _, mode := range []string{"allow", "none", "unavailable", "wrong-target"} {
			t.Run(endpoint+"/"+mode, func(t *testing.T) {
				caps, err := dbbridge.GenerateCapabilities("mysql")
				if err != nil {
					t.Fatal(err)
				}
				capabilities := models.JSONString(caps)
				native := models.EngineRuntimeDescriptor{ID: 12, Name: "private-mysql", EngineType: "mysql", LifecycleState: "active", ConnectionStatus: "online", Capabilities: &capabilities}
				raw, err := dbbridge.GenerateCapabilities("duckdb")
				if err != nil {
					t.Fatal(err)
				}
				computeCaps := models.JSONString(raw)
				compute := models.EngineRuntimeDescriptor{ID: 18, Name: "compute-runtime", EngineType: "duckdb", LifecycleState: "active", ConnectionStatus: "online", Capabilities: &computeCaps}
				checks := 0
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/system/runtime/engine-descriptors":
						if r.Header.Get("Authorization") != "Bearer addp_at_service" {
							t.Error("descriptor service identity changed")
						}
						json.NewEncoder(w).Encode(map[string]interface{}{"data": []models.EngineRuntimeDescriptor{native, compute}, "total": 2, "page": 1, "page_size": 100})
					case "/api/v1/system/engine-access/metadata-checks/develop-navigation":
						checks++
						if r.Header.Get("Authorization") != "Bearer addp_at_user" || r.Header.Get("X-Tenant-ID") != "" || r.Method != "POST" {
							t.Error("original user replaced")
						}
						if mode == "unavailable" {
							w.WriteHeader(503)
							return
						}
						var q authorization.SourceNavigationCheckRequest
						if json.NewDecoder(r.Body).Decode(&q) != nil || !q.Valid() || len(q.Targets) != 1 || q.Targets[0].EngineID != 12 {
							t.Fatal("unexpected source candidates")
						}
						result := authorization.SourceMetadataCheck{ObservedAt: time.Now(), Targets: []authorization.SourceMetadataTargetCheck{{Target: q.Targets[0], Visible: mode == "allow"}}}
						if mode == "wrong-target" {
							result.Targets[0].Target.EngineID++
						}
						json.NewEncoder(w).Encode(result)
					default:
						t.Errorf("unexpected System path %s", r.URL.Path)
						w.WriteHeader(404)
					}
				}))
				defer upstream.Close()
				h := NewEngineHandler(commonClient.NewSystemServiceClient(upstream.URL, staticDevelopServiceTokens("addp_at_service"), nil))
				router := gin.New()
				router.GET(endpoint, func(c *gin.Context) {
					setTenantAuthContextForTest(c, 7, 1)
					if endpoint == "/source-engines" {
						h.ListSourceEngines(c)
					} else {
						h.ListEngines(c)
					}
				})
				req := httptest.NewRequest("GET", endpoint, nil)
				req.Header.Set("Authorization", "Bearer addp_at_user")
				resp := httptest.NewRecorder()
				router.ServeHTTP(resp, req)
				if checks != 1 {
					t.Fatalf("data visibility checks=%d; response=%s", checks, resp.Body.String())
				}
				if mode == "unavailable" || mode == "wrong-target" {
					if resp.Code != 503 {
						t.Fatalf("dependency status=%d body=%s", resp.Code, resp.Body.String())
					}
					return
				}
				var got []models.EngineRuntimeDescriptor
				if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &got) != nil {
					t.Fatalf("unexpected response %d %s", resp.Code, resp.Body.String())
				}
				want := 0
				if endpoint == "/engines" {
					want++
				}
				if mode == "allow" {
					want++
				}
				if len(got) != want || (endpoint == "/engines" && got[len(got)-1].ID != 18) {
					t.Fatalf("unauthorized source or compute runtime changed: %+v", got)
				}
				if resp.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("visibility result cacheable")
				}
			})
		}
	}
}
