package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonauth "github.com/addp/common/middleware/auth"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fileFactsReaderTest struct {
	facts   *commonclient.DevelopFileExecutionFacts
	calls   int
	failure error
}

func (r *fileFactsReaderTest) Read(_ context.Context, tenant uint, id string) (*commonclient.DevelopFileExecutionFacts, error) {
	r.calls++
	if r.failure != nil {
		return nil, r.failure
	}
	if tenant != 7 || id != r.facts.ExecutionID {
		return nil, gorm.ErrRecordNotFound
	}
	return r.facts.Clone(), nil
}

func TestFileExecutionOwnerRoutesOnlyCurrentSystemTenantService(t *testing.T) {
	auth := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer system":        {ClientID: "addp-system", Permissions: []string{"develop.file_execution.read"}},
		"Bearer other":         {ClientID: "addp-develop", Permissions: []string{"develop.file_execution.read"}},
		"Bearer no-permission": {ClientID: "addp-system", Permissions: []string{"develop.task.read"}},
	})
	defer auth.Close()
	config := json.RawMessage(`{"content":{"workflow_definition":{"tasks":[]}},"timeout":60}`)
	intent, err := execution.NewDevelopFileIntent(config, 1, 60, []execution.DevelopFileSourceIntent{{NodeID: "load", Target: plugin.FileItemPath(11, "source.gpkg")}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader := &fileFactsReaderTest{facts: &commonclient.DevelopFileExecutionFacts{SchemaVersion: commonclient.DevelopFileExecutionSchema,
		ExecutionID: uuid.NewString(), TenantID: "7", ConfigJSON: config, Intent: *intent, AdmissionState: "pending",
		Actor: commonclient.DevelopFileExecutionActor{PrincipalID: "31", TenantMembershipID: "41", AuthorizationVersion: "5"}}}
	router := gin.New()
	api := router.Group("/api/v1/develop")
	api.Use(commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: auth.URL}), commonauth.MustNewContextGuard("tenant"))
	registerFileExecutionRoutes(api, reader)
	path := "/api/v1/develop/runtime/file-executions/" + reader.facts.ExecutionID
	for _, tc := range []struct {
		token, path string
		status      int
		calls       int
	}{
		{"", path, 401, 0}, {"other", path, 403, 0}, {"no-permission", path, 403, 0},
		{"system", path + "?tenant_id=8", 400, 0}, {"system", "/api/v1/develop/runtime/file-executions/not-uuid", 400, 0},
		{"system", path, 200, 1}, {"system", "/api/v1/develop/runtime/file-executions/" + uuid.NewString(), 404, 2},
	} {
		response := performDevelopCatalogRequest(router, http.MethodGet, tc.path, tc.token, "")
		if response.Code != tc.status || reader.calls != tc.calls {
			t.Fatalf("%s status=%d calls=%d body=%s", tc.token, response.Code, reader.calls, response.Body.String())
		}
		if response.Code == 200 {
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private facts may be cached")
			}
			var facts commonclient.DevelopFileExecutionFacts
			if json.Unmarshal(response.Body.Bytes(), &facts) != nil || facts.Validate() != nil || facts.Actor != reader.facts.Actor || string(facts.ConfigJSON) != string(config) {
				t.Fatal("frozen Owner projection changed")
			}
			for _, secret := range []string{"lease_token", "connection_info", "access_token", "allowed"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("unexpected field %s", secret)
				}
			}
		}
	}
	reader.failure = errors.New("database secret")
	response := performDevelopCatalogRequest(router, http.MethodGet, path, "system", "")
	if response.Code != 500 || strings.Contains(response.Body.String(), "database secret") {
		t.Fatal("internal error leaked")
	}
	userAuth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer user": {"develop.file_execution.read"}})
	defer userAuth.Close()
	userRouter := gin.New()
	group := userRouter.Group("/api/v1/develop", commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: userAuth.URL}), commonauth.MustNewContextGuard("tenant"))
	registerFileExecutionRoutes(group, reader)
	if got := performDevelopCatalogRequest(userRouter, http.MethodGet, path, "user", ""); got.Code != 403 {
		t.Fatalf("User bypass status=%d", got.Code)
	}
}

func TestFileExecutionOwnerRouteFailsClosedOnAuthenticationOutage(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer auth.Close()
	reader := &fileFactsReaderTest{}
	router := gin.New()
	api := router.Group("/api/v1/develop", commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: auth.URL}), commonauth.MustNewContextGuard("tenant"))
	registerFileExecutionRoutes(api, reader)
	response := performDevelopCatalogRequest(router, http.MethodGet, "/api/v1/develop/runtime/file-executions/"+uuid.NewString(), "system", "")
	if response.Code < 400 || reader.calls != 0 {
		t.Fatal("auth dependency outage reached Owner facts")
	}
}

// Exercise the System Common reader against the actual Owner route and stored
// facts. AuthContext is an explicit authority double, not a real Role or Grant.
func TestFileExecutionOwnerStoredFactsViaSystemSDK(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.FileExecution{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewFileExecutionRepository(db)
	actor, member, version, triggered := int64(31), int64(41), int64(5), 31
	original, err := repo.CreatePending(t.Context(), &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7,
		Module: "develop", Source: "develop", TaskType: "workflow", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", MaxAttempts: 1,
		TriggeredBy: &triggered, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version,
		ExecutionConfig: commonmodels.JSONMap{"timeout": 60, "exact_id": json.Number("9007199254740993")}},
		[]execution.DevelopFileSourceIntent{{NodeID: "load", Target: plugin.FileItemPath(11, "sources/A.gpkg")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	auth := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer system": {ClientID: "addp-system", Permissions: []string{"develop.file_execution.read"}},
	})
	defer auth.Close()
	router := gin.New()
	api := router.Group("/api/v1/develop", commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: auth.URL}), commonauth.MustNewContextGuard("tenant"))
	registerFileExecutionRoutes(api, repo)
	owner := httptest.NewServer(router)
	defer owner.Close()
	calls := 0
	sdk := commonclient.NewDevelopClient(owner.URL, commonclient.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
		calls++
		if tenant != 7 {
			t.Errorf("wrong service tenant %d", tenant)
		}
		return "system", nil
	}), owner.Client())
	facts, err := sdk.ReadFileExecution(t.Context(), 7, original.ExecutionID)
	if err != nil || calls != 1 || string(facts.ConfigJSON) != string(original.ConfigJSON) || facts.Actor != original.Actor || facts.Intent.ConfigDigest != original.Intent.ConfigDigest {
		t.Fatalf("Owner/System frozen facts changed: %v", err)
	}
	if !strings.Contains(string(facts.ConfigJSON), "9007199254740993") {
		t.Fatal("integer lost precision")
	}
	facts.ConfigJSON[0] = '['
	again, err := sdk.ReadFileExecution(t.Context(), 7, original.ExecutionID)
	if err != nil || calls != 2 || string(again.ConfigJSON) != string(original.ConfigJSON) {
		t.Fatalf("private projection mutated or credential not requested: %v", err)
	}
}
