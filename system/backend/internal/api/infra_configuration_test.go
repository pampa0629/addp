package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	requestid "github.com/addp/common/middleware/requestid"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/infraconfig"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestInfraConfigurationPlatformAndPermissions(t *testing.T) {
	const endpoint = "/platform/infra_configuration"
	const apply = endpoint + "/redis/parameters/slowlog-max-len/apply"
	const read = "system.infra_configuration.read"
	const update = "system.infra_configuration.update"
	for _, tc := range []struct {
		name, scope, method, path, body string
		permissions                     []string
		status                          int
	}{
		{"platform read", "platform", "GET", endpoint, "", []string{read}, 200},
		{"missing read", "platform", "GET", endpoint, "", nil, 403},
		{"tenant cannot access", "tenant", "GET", endpoint, "", []string{read}, 403},
		{"read only", "platform", "POST", apply, `{"value":"30"}`, []string{read}, 403},
		{"unknown input", "platform", "POST", apply, `{"value":"30","endpoint":"other"}`, []string{read, update}, 400},
		{"unknown component", "platform", "POST", endpoint + "/unsupported/parameters/x/apply", `{"value":"30"}`, []string{read, update}, 404},
		{"no binding", "platform", "POST", apply, `{"value":"30"}`, []string{read, update}, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := testIAMActorContext(tc.scope)
			if len(tc.permissions) != 0 {
				actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{
					AssignmentID: "91", RoleKey: "custom.settings", Scope: commonauth.AssignmentScope{
						Type: actor.Context.Type, TenantID: actor.Context.TenantID,
					}, Permissions: tc.permissions, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt,
				}}
			}
			runtime := &IAMRuntime{
				Authentication: func(c *gin.Context) {
					if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
						t.Fatal(err)
					}
					c.Next()
				},
				UserAccessCredential: func(c *gin.Context) { c.Next() },
			}
			router := gin.New()
			handler := &InfraConfigurationHandler{service: infraconfig.New(&config.Config{InfraDatabaseProvider: "postgres"})}
			if err := RegisterInfraConfigurationRoutes(router.Group(""), runtime, handler); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if request.Method == http.MethodGet && tc.status == http.StatusOK && !strings.Contains(response.Body.String(), `"status":"unbound"`) {
				t.Fatal(response.Body.String())
			}
		})
	}
}

func TestInfraConfigurationAudit(t *testing.T) {
	if os.Getenv("ADDP_INFRA_CONFIGURATION_INTEGRATION") != "1" {
		t.Skip("requires owned PostgreSQL and Redis from make test-system-infra-configuration")
	}
	password := os.Getenv("ADDP_INFRA_GATE_MANAGER_PASSWORD")
	cfg := &config.Config{
		InfraDatabaseProvider: "postgres", InfraPGConfigPassword: password, InfraRedisConfigPassword: password,
		PostgresHost: "127.0.0.1", PostgresPort: os.Getenv("ADDP_INFRA_GATE_PG_PORT"), PostgresDB: "addp_test",
		PostgresUser: "infra_test", PostgresPassword: os.Getenv("ADDP_INFRA_GATE_PASSWORD"),
		RedisHost: "127.0.0.1", RedisPort: os.Getenv("ADDP_INFRA_GATE_REDIS_PORT"),
	}
	db, err := gorm.Open(postgres.Open(cfg.PostgreSQLDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	var principalID int64
	if err := db.Raw("INSERT INTO system.principals (principal_type) VALUES ('user') RETURNING id").Scan(&principalID).Error; err != nil {
		t.Fatalf("create audit actor: %v", err)
	}
	actor := testIAMActorContext("platform")
	actor.Principal.ID = strconv.FormatInt(principalID, 10)
	actor.Authorization.RoleAssignments = []commonauth.RoleAssignment{{
		AssignmentID: "91", RoleKey: "platform.system_administrator", Scope: commonauth.AssignmentScope{Type: "platform"},
		Permissions: []string{"system.infra_configuration.read", "system.infra_configuration.update"},
		SourceType:  "bootstrap", ValidFrom: actor.Token.IssuedAt,
	}}
	runtime := &IAMRuntime{
		Authentication: func(c *gin.Context) {
			if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
				t.Fatal(err)
			}
			c.Next()
		},
		UserAccessCredential: func(c *gin.Context) { c.Next() },
	}
	router := gin.New()
	router.Use(requestid.RequestIDMiddleware())
	handler := &InfraConfigurationHandler{service: infraconfig.New(cfg), audit: iam.NewAuditWriter(iam.NewRepository(db))}
	if err := RegisterInfraConfigurationRoutes(router.Group("/api/v1/system"), runtime, handler); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ component, parameter, before, after string }{
		{"database", "maintenance_work_mem", "196608", "197632"},
		{"redis", "slowlog-max-len", "192", "193"},
	} {
		t.Run(tc.component, func(t *testing.T) {
			path := "/api/v1/system/platform/infra_configuration/" + tc.component + "/parameters/" + tc.parameter + "/apply"
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"value":"`+tc.after+`"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var outcome infraconfig.Outcome
			if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil || response.Code != http.StatusOK ||
				!outcome.AuditRecorded || outcome.Application != "confirmed" || outcome.Persistence != "confirmed" ||
				outcome.PreviousValue != tc.before || outcome.Current == nil || outcome.Current.Value != tc.after {
				t.Fatalf("apply status = %d, body = %s, decode error = %v", response.Code, response.Body, err)
			}
			requestID := response.Header().Get(requestid.RequestIDHeader)
			var events []iam.AuditLog
			if err := db.Where("request_id = ? AND entity_type = ?", requestID, "infra_parameter").Order("id").Find(&events).Error; err != nil {
				t.Fatal(err)
			}
			if requestID == "" || len(events) != 2 {
				t.Fatalf("request %q has %d audit records, want 2", requestID, len(events))
			}
			for i, name := range []string{"apply_requested", "apply_completed"} {
				event := events[i]
				if event.EventName != "system.infra_configuration."+name || event.Result != iam.AuditResultSucceeded ||
					event.PrincipalID == nil || *event.PrincipalID != principalID ||
					event.ContextType == nil || *event.ContextType != "platform" ||
					event.EntityID == nil || *event.EntityID != tc.component+"/"+tc.parameter {
					t.Fatalf("audit event %d identity or result mismatch: %+v", i, event)
				}
				var details map[string]string
				if err := json.Unmarshal(event.Details, &details); err != nil || details["previous_value"] != tc.before || details["requested_value"] != tc.after {
					t.Fatalf("audit event %d details = %s, decode error = %v", i, event.Details, err)
				}
				if i == 1 && (details["application"] != "confirmed" || details["persistence"] != "confirmed" || details["observed_value"] != tc.after) {
					t.Fatalf("completion details = %s", event.Details)
				}
			}
			pending, completed := events[0], events[1]
			if pending.HTTPMethod != nil || pending.ResourcePath != nil || pending.HTTPStatus != nil {
				t.Fatal("request event carries response metadata before the write")
			}
			if completed.HTTPMethod == nil || *completed.HTTPMethod != http.MethodPost ||
				completed.ResourcePath == nil || *completed.ResourcePath != path ||
				completed.HTTPStatus == nil || *completed.HTTPStatus != http.StatusOK {
				t.Fatalf("completion lacks HTTP response metadata: %+v", completed)
			}
		})
	}
}
