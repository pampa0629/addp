package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setManagerExecutionReadTestUser(c *gin.Context, tenantID, principalID uint) {
	identity := authtest.NewTenantUserAuthContext(strconv.FormatUint(uint64(tenantID), 10), strconv.FormatUint(uint64(principalID), 10), []string{"manager.derived_artifact.read"})
	if err := commonauth.SetAuthContextForGin(c, identity); err != nil {
		panic(err)
	}
}

func TestManagerExecutionReadIsolation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	testManagerExecutionReadIsolation(t, db)
	testManagerMosaicExecutionActor(t, db)
}

func TestIntegrationPostgresManagerExecutionReadIsolation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("Manager PostgreSQL gate enables integration tests")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS common").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&execution.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	testManagerExecutionReadIsolation(t, tx)
	testManagerMosaicExecutionActor(t, tx)
}

func testManagerExecutionReadIsolation(t *testing.T, db *gorm.DB) {
	t.Helper()
	const tenant = 710000091
	permissions := []string{"manager.derived_artifact.read"}
	identities := map[string]authorization.AuthContext{
		"owner":   authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "31", permissions),
		"peer":    authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "32", permissions),
		"oauth":   authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "31", permissions),
		"foreign": authtest.NewTenantUserAuthContext(strconv.Itoa(tenant+1), "31", permissions),
		"denied":  authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "31", []string{"monitor.execution.read"}),
	}
	oauth := identities["oauth"]
	oauth.Token.Type = "oauth_access_token"
	oauth.Client.ScopeMode, oauth.Client.Scopes = "restricted", []string{"addp.api"}
	identities["oauth"] = oauth
	for _, token := range []string{"machine", "wrong-machine", "machine-denied"} {
		identity := authtest.NewTenantUserAuthContext(strconv.Itoa(tenant), "91", []string{"manager.task_provider.read"})
		identity.Principal.Type, identity.Token.Type = "service_principal", "service_access_token"
		identity.Authentication.Methods, identity.Authentication.AssuranceLevel = []string{"service_secret"}, "not_applicable"
		clientID := "addp-orchestrator"
		if token == "wrong-machine" {
			clientID = "addp-meta"
		}
		identity.Client.ClientID = &clientID
		if token == "machine-denied" {
			identity.Authorization.RoleAssignments[0].Permissions = []string{"manager.derived_artifact.read"}
		}
		identities[token] = identity
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := identities[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok || r.URL.Path != "/api/v1/system/auth/context" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(identity)
	}))
	t.Cleanup(server.Close)
	history, empty := "deleted-definition", ""
	owner, peer, unknown := 31, 32, 0
	rows := map[string]execution.TaskExecution{
		"history": {SourceTaskID: &history, TriggeredBy: &peer},
		"own":     {TriggeredBy: &owner}, "peer": {TriggeredBy: &peer},
		"no-actor": {}, "empty-source": {SourceTaskID: &empty, TriggeredBy: &owner},
		"other-module": {Module: "meta", TaskType: "scan", SourceTaskID: &history},
		"profiling":    {TaskType: "data_profiling", SourceTaskID: &history},
		"cleanup":      {TaskType: "cleanup_executor", SourceTaskID: &history},
		"unknown-type": {TaskType: "unregistered", SourceTaskID: &history},
		"zero-actor":   {TriggeredBy: &unknown},
		"foreign":      {TenantID: tenant + 1, SourceTaskID: &history},
	}
	for taskType := range managerExecutionReadPermissions {
		if isManagerDerivedExecutionType(taskType) {
			rows[taskType] = execution.TaskExecution{TaskType: taskType, SourceTaskID: &history}
		}
	}
	ids := map[string]string{}
	for name, row := range rows {
		row.ExecutionID = uuid.NewString()
		if row.TenantID == 0 {
			row.TenantID = tenant
		}
		if row.Module == "" {
			row.Module = execution.ModuleManager
		}
		if row.TaskType == "" {
			row.TaskType = execution.TaskTypeEmbedding
		}
		row.Source, row.Status, row.TriggerType = "manager", "success", "manual"
		projection := managerResponseFixture(t)
		row.Metadata, row.ExecutionConfig = projection.Metadata, projection.ExecutionConfig
		// Each authorization can back only one execution in the canonical store.
		authorizationID := int64(tenant)*1000 + int64(len(ids)) + 1
		row.ExecutionAuthorizationID = &authorizationID
		row.ActorPrincipalID = projection.ActorPrincipalID
		row.ActorTenantMembershipID = projection.ActorTenantMembershipID
		row.IssuedAuthorizationVersion = projection.IssuedAuthorizationVersion
		row.AuthorizationExpiresAt = projection.AuthorizationExpiresAt
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		ids[name] = row.ExecutionID
	}
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", ids["peer"]).Update("parent_execution_id", ids["history"]).Error; err != nil {
		t.Fatal(err)
	}
	handler := NewTaskProviderHandler(nil, nil, nil, nil, execution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.Use(commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: server.URL}), commonauth.MustNewContextGuard("tenant"))
	router.GET("/executions/:execution_id", commonauth.MustNewPermissionGuard("manager.derived_artifact.read"), handler.ExecutionStatus)
	router.GET("/task-provider/executions/:execution_id", commonauth.MustNewServiceClientGuard("addp-orchestrator"), commonauth.MustNewPermissionGuard("manager.task_provider.read"), handler.ProviderExecutionStatus)
	request := func(token, route, name string, expected int) {
		t.Helper()
		id := ids[name]
		if id == "" {
			id = uuid.NewString()
		}
		req := httptest.NewRequest("GET", route+id, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != expected {
			t.Fatalf("%s %s %s: status=%d want=%d body=%s", token, route, name, response.Code, expected, response.Body.String())
		}
		if expected == 404 && strings.Contains(response.Body.String(), id) {
			t.Fatal("denied response leaked execution identity")
		}
		if expected == 200 {
			for _, private := range []string{"private-sentinel", "execution_config", "execution_authorization_id", "actor_principal_id", "actor_tenant_membership_id", "issued_authorization_version", "authorization_expires_at", "schema_snapshot", "field_mappings", "stdout", "stderr", "access_plan"} {
				if strings.Contains(response.Body.String(), private) {
					t.Fatalf("authorized execution response exposed %s", private)
				}
			}
		}
	}
	for _, token := range []string{"owner", "oauth", "peer"} {
		request(token, "/executions/", "history", 200)
		allowed, denied := "own", "peer"
		if token == "peer" {
			allowed, denied = denied, allowed
		}
		request(token, "/executions/", allowed, 200)
		request(token, "/executions/", denied, 404)
	}
	for _, name := range []string{"no-actor", "zero-actor", "empty-source", "other-module", "profiling", "cleanup", "unknown-type", "foreign", "missing"} {
		request("owner", "/executions/", name, 404)
	}
	request("foreign", "/executions/", "history", 404)
	request("denied", "/executions/", "history", 403)
	request("machine-denied", "/executions/", "history", 403)
	for _, name := range []string{"history", "peer", "no-actor"} {
		request("machine", "/task-provider/executions/", name, 200)
	}
	for _, name := range []string{"other-module", "profiling", "cleanup", "unknown-type", "foreign", "missing"} {
		request("machine", "/task-provider/executions/", name, 404)
	}
	for _, token := range []string{"owner", "wrong-machine", "machine-denied"} {
		request(token, "/task-provider/executions/", "history", 403)
	}
	for taskType := range managerExecutionReadPermissions {
		if isManagerDerivedExecutionType(taskType) {
			request("peer", "/executions/", taskType, 200)
			request("machine", "/task-provider/executions/", taskType, 200)
		}
	}
	// The same session is re-resolved after revocation; earlier reads grant no cache.
	identities["owner"] = identities["denied"]
	request("owner", "/executions/", "history", 403)
}
