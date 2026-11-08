package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	_ "github.com/addp/common/engine/plugins/minio"
	sharedauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/secretcipher"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInstanceOverviewHTTPPermissionPrecisionAndSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name         string
		permitted    bool
		tenant       uint
		sourceStatus int
		query        string
		want         int
	}{
		{"success", true, 3, 200, "", 200}, {"permission", false, 3, 200, "", 403}, {"foreign", true, 8, 200, "", 403},
		{"query", true, 3, 200, "?unknown=1", 400}, {"source-denied", true, 3, 403, "", 200}, {"source-error", true, 3, 400, "", 503},
		{"unsupported", true, 3, 200, "", 422},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(test.sourceStatus)
				if test.sourceStatus != 200 {
					code := "InvalidRequest"
					if test.sourceStatus == 403 {
						code = "AccessDenied"
					}
					fmt.Fprintf(w, `<Error><Code>%s</Code><Message>private-native-error</Message></Error>`, code)
					return
				}
				fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets></Buckets></ListAllMyBucketsResult>`)
			}))
			defer source.Close()
			db, err := gorm.Open(sqlite.Open("file:overview_"+test.name+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&models.Engine{}); err != nil {
				t.Fatal(err)
			}
			key := []byte("addp-dev-encryption-key-2025!!!!")
			access, _ := secretcipher.Encrypt("fixture-access", key)
			secret, _ := secretcipher.Encrypt("fixture-secret", key)
			tenant := test.tenant
			engine := &models.Engine{ID: 71, Name: "Native fixture", TenantID: &tenant, Version: 2, EngineType: "minio", LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"endpoint": strings.TrimPrefix(source.URL, "http://"), "access_key": access, "secret_key": secret}}
			if test.name == "unsupported" {
				engine.EngineType = "s3"
			}
			repo := repository.NewEngineRepository(db)
			if err := repo.Create(engine); err != nil {
				t.Fatal(err)
			}
			h := NewEngineHandler(service.NewEngineService(repo, key, nil))
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.Use(func(c *gin.Context) {
				a := testIAMActorContext("tenant")
				if test.permitted {
					a.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.overview", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: a.Context.TenantID}, Permissions: []string{"system.engine.read"}}}
				}
				if err := sharedauth.SetAuthContextForGin(c, a); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			guard, err := middleware.NewIAMPermissionGuard("system.engine.read")
			if err != nil {
				t.Fatal(err)
			}
			router.GET("/engines/:id/overview", guard, h.InstanceOverview)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", "/engines/71/overview"+test.query, nil))
			if rec.Code != test.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "fixture-secret") || strings.Contains(rec.Body.String(), "private-native-error") {
				t.Fatal("private source detail leaked")
			}
			if test.want == 403 || test.want == 400 || test.want == 422 {
				if calls != 0 {
					t.Fatal("unauthorized request reached source")
				}
			}
			if test.name == "success" {
				var body map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body["engine_id"] != "71" || !strings.Contains(rec.Body.String(), `"value":"0"`) || rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatal(rec.Body.String())
				}
			}
		})
	}
}
