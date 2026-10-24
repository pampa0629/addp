package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/postgresql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/dataprotection"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

func TestServiceRoleRefusalsAreTypedLocalizedAndDoNotLeakDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, item := range []struct {
		err    error
		status int
		code   string
		zh, en string
	}{
		{dataprotection.ErrRoleOptionUnavailable, 409, "query_protection_choice_invalid", "所选脱敏方式", "selected masking method"},
		{dataprotection.ErrRoleOptionIneligible, 403, "query_protection_roles_unavailable", "当前登录账号", "current signed-in user"},
	} {
		for _, lang := range []string{"zh-cn", "en"} {
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.GET("/refusal", func(c *gin.Context) {
				writeQueryExecutionError(c, errors.Join(item.err, errors.New("private-source-and-credential-detail")))
			})
			request := httptest.NewRequest("GET", "/refusal", nil)
			request.Header.Set("Accept-Language", lang)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			body := response.Body.String()
			want := item.zh
			if lang == "en" {
				want = item.en
			}
			if response.Code != item.status || !strings.Contains(body, item.code) || !strings.Contains(body, want) || strings.Contains(body, "private-source") {
				t.Fatalf("%s: %d %s", lang, response.Code, body)
			}
		}
	}
}

type apiRoleTestTokens struct{}

func (apiRoleTestTokens) Token(context.Context, uint) (string, error) { return "addp_at_owner", nil }
func (apiRoleTestTokens) PlatformToken(context.Context) (string, error) {
	return "addp_at_platform", nil
}

type apiRoleOptionsGate struct{}

func (apiRoleOptionsGate) BeginPreparedQuery(context.Context, uint, plugin.EnginePlugin, plugin.PreparedQuery, *dataprotection.QueryRoleSelection) (func(*plugin.QueryResult) error, func(), error) {
	return nil, nil, errors.New("must not execute")
}
func (apiRoleOptionsGate) BeginUnresolvedRead(context.Context, uint) (func(), error) {
	return nil, errors.New("must not execute")
}
func (apiRoleOptionsGate) RoleOptions(_ context.Context, tenant int64, target dataprotection.ResourceReference, roles []string) ([]dataprotection.FieldRoleOptions, error) {
	if tenant != 7 || len(roles) != 1 || roles[0] != "custom.analytics" {
		return nil, errors.New("wrong binding")
	}
	return []dataprotection.FieldRoleOptions{{Target: target, ComponentKey: "phone", Options: []dataprotection.RoleOptionCandidate{{ID: "only-option", Name: "Full mask"}}}}, nil
}
func (apiRoleOptionsGate) ValidateRoleChoices(context.Context, int64, *dataprotection.QueryRoleSelection) error {
	return nil
}

func TestApplicationHeaderDoesNotBorrowUserRoleContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"service.definition.read"})
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/auth/subject-roles":
			reads++
			if r.Header.Get("Authorization") != "Bearer addp_at_current" || r.URL.RawQuery != "" {
				t.Error("role observation changed identity or selectors")
			}
			json.NewEncoder(w).Encode(authorization.CurrentSubjectRoles{TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: *facts.Context.TenantMembershipID, AuthorizationVersion: facts.Authorization.AuthorizationVersion, RoleKeys: []string{"custom.analytics"}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute)})
		case "/api/v1/system/runtime/engine-descriptors/11":
			if r.Header.Get("Authorization") != "Bearer addp_at_owner" {
				t.Error("engine facts did not use owner identity")
			}
			json.NewEncoder(w).Encode(map[string]any{"id": 11, "engine_type": "postgresql"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	executor := svc.NewQueryExecutorService(nil, client.NewSystemServiceClient(server.URL, apiRoleTestTokens{}, server.Client()), nil)
	executor.SetProtectionGate(apiRoleOptionsGate{})
	handler := NewQueryServiceHandler(nil, executor)
	for _, application := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, "/?target_locator="+url.QueryEscape("addp://engine/11/path/public/persons?type=table&item_id=33"), nil)
		request.Header.Set("Authorization", "Bearer addp_at_current")
		if application {
			request.Header.Set("X-API-Key", "application-present")
		}
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = request
		if err := commonauth.SetAuthContextForGin(c, facts); err != nil {
			t.Fatal(err)
		}
		before := reads
		handler.QueryProtectionOptions(c)
		if application {
			if response.Code != 403 || reads != before {
				t.Fatalf("application borrowed user roles: %d %d", response.Code, reads)
			}
		} else if response.Code != 200 || reads != before+1 || !strings.Contains(response.Body.String(), "only-option") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("user candidates: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestQueryDefinitionAcceptsOnlyRoleReferencesNotAlgorithms(t *testing.T) {
	for _, raw := range []string{`null`, `[{"algorithm":"allow"}]`, `[{"role_keys":["tenant.administrator"]}]`} {
		q := &models.QueryService{DataConfig: models.JSONB{}}
		var req models.CreateQueryServiceRequest
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"service_name":"test","title":"Test","config_type":"table","data_config":{"protection_choices":`+raw+`}}`))
		c.Request.Header.Set("Content-Type", "application/json")
		if err := bindQueryDefinition(c, &req); err == nil {
			q.DataConfig = models.JSONB(req.DataConfig)
			if _, err := q.ProtectionChoices(); err == nil {
				t.Fatal("invalid role configuration accepted")
			}
		}
	}
}
