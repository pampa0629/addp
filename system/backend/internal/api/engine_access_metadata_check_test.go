package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type metadataCheckFixture struct {
	calls       int
	wrongTarget bool
}

func (f *metadataCheckFixture) CheckManagerSearchRead(_ context.Context, credential string, request authorization.SourceMetadataCheckRequest) (*authorization.SourceSearchCheck, error) {
	f.calls++
	if credential != "addp_at_test" && credential != "addp_dat_test" {
		panic("wrong search credential")
	}
	return &authorization.SourceSearchCheck{ObservedAt: time.Now(), Targets: []authorization.SourceSearchTargetCheck{{
		SourceMetadataTargetCheck: authorization.SourceMetadataTargetCheck{Target: request.Targets[0], Visible: true},
	}}}, nil
}

func TestManagerSearchMetadataCheckUsesFixedAudienceAndScope(t *testing.T) {
	var logs bytes.Buffer
	logger.Init(logger.Options{Writer: &logs})
	t.Cleanup(func() { logger.Init(logger.Options{}) })
	target := plugin.TabularItemPath(12, "schema", "public", "private_search_fixture")
	input := authorization.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{target}}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, audience, scope string
		status                int
	}{
		{"ordinary", "", "", 200},
		{"search delegate", "manager", "data.search", 200},
		{"preview delegate", "manager", "data.preview", 403},
		{"Meta delegate", "meta", "resource.children.list", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := sourceReadCheckAuth()
			current.Authorization.RoleAssignments[0].Permissions = []string{engineaccess.ManagerSearchPermission}
			token := "addp_at_test"
			if tc.audience != "" {
				current.Token.Type = middleware.IAMTokenTypeDelegatedAccess
				current.Client.Audiences = []string{tc.audience}
				current.Client.ScopeMode = "restricted"
				current.Client.Scopes = []string{tc.scope}
				current.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *current.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
				token = "addp_dat_test"
			}
			authenticate, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &current})
			if err != nil {
				t.Fatal(err)
			}
			credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
			if err != nil {
				t.Fatal(err)
			}
			fixture := &metadataCheckFixture{}
			router := modulelifecycle.NewRouter()
			if err := RegisterEngineAccessMetadataCheckRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, UserAccessCredential: credential}, &EngineAccessMetadataCheckHandler{service: fixture}); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "/api/v1/system/engine-access/metadata-checks/manager-search", strings.NewReader(string(body)))
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			logs.Reset()
			router.ServeHTTP(response, request)
			if response.Code != tc.status || (fixture.calls == 1) != (tc.status == 200) {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, fixture.calls, response.Body.String())
			}
			if tc.status == 200 {
				var observed authorization.SourceSearchCheck
				if json.Unmarshal(response.Body.Bytes(), &observed) != nil || !observed.Matches(input) || !observed.Targets[0].Visible || observed.Targets[0].ContentReadable || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("invalid search observation")
				}
				if bytes.Contains(response.Body.Bytes(), []byte("manager_search")) {
					t.Fatal("diagnostics reached the HTTP response")
				}
			}
			var completion struct {
				Message     string                      `json:"msg"`
				RequestID   string                      `json:"request_id"`
				Diagnostics map[string]map[string]int64 `json:"diagnostics"`
			}
			if err := json.Unmarshal(logs.Bytes(), &completion); err != nil || completion.Message != "http.request.completed" ||
				completion.RequestID == "" || completion.RequestID != response.Header().Get("X-Request-ID") {
				t.Fatalf("invalid request completion log: %v", err)
			}
			if tc.status != 200 {
				if len(completion.Diagnostics) != 0 {
					t.Fatal("rejected search emitted diagnostics")
				}
				return
			}
			fields := completion.Diagnostics["manager_search"]
			keys := []string{"candidate_count", "whole_candidate_count", "engine_query_count", "rule_item_count",
				"rule_query_count", "route_auth_and_guards_ns", "request_binding_ns", "pre_meta_credential_ns",
				"metadata_binding_ns", "meta_facts_ns", "engine_lookup_ns", "rule_preparation_ns", "rule_observation_ns",
				"observed_credential_ns", "rule_query_ns", "response_ns"}
			if len(completion.Diagnostics) != 1 || len(fields) != len(keys) {
				t.Fatalf("unexpected diagnostic fields: %v", fields)
			}
			for _, key := range keys {
				if value, ok := fields[key]; !ok || value < 0 {
					t.Fatalf("invalid diagnostic field %s", key)
				}
			}
			if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "private_search_fixture") {
				t.Fatal("completion log contained request data")
			}
		})
	}
}

func (f *metadataCheckFixture) CheckMetaMetadataRead(_ context.Context, credential string, request authorization.SourceMetadataCheckRequest) (*authorization.SourceMetadataCheck, error) {
	f.calls++
	target := request.Targets[0]
	if credential != "addp_at_test" {
		panic("wrong credential")
	}
	if f.wrongTarget {
		target = plugin.TabularItemPath(13, "schema", "public", "other")
	}
	return &authorization.SourceMetadataCheck{ObservedAt: time.Now(), Targets: []authorization.SourceMetadataTargetCheck{{Target: target, Visible: false}}}, nil
}

func TestMetadataCheckRouteContract(t *testing.T) {
	target := plugin.TabularItemPath(12, "schema", "public", "C")
	body, _ := json.Marshal(authorization.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{target}})
	for _, tc := range []struct {
		name, tokenType, body, query string
		noPermission, wrongTarget    bool
		status, calls                int
	}{
		{"ordinary User", middleware.IAMTokenTypeFirstPartyAccess, string(body), "", false, false, 200, 1},
		{"no function", middleware.IAMTokenTypeFirstPartyAccess, string(body), "", true, false, 403, 0},
		{"service", middleware.IAMTokenTypeServiceAccess, string(body), "", false, false, 403, 0},
		{"delegated", middleware.IAMTokenTypeDelegatedAccess, string(body), "", false, false, 200, 1},
		{"ancestors", middleware.IAMTokenTypeDelegatedAccess, string(body), "", false, false, 200, 1},
		{"wrong audience", middleware.IAMTokenTypeDelegatedAccess, string(body), "", false, false, 403, 0},
		{"wrong scope", middleware.IAMTokenTypeDelegatedAccess, string(body), "", false, false, 403, 0},
		{"multiple scopes", middleware.IAMTokenTypeDelegatedAccess, string(body), "", false, false, 403, 0},
		{"delegate no function", middleware.IAMTokenTypeDelegatedAccess, string(body), "", true, false, 403, 0},
		{"empty", middleware.IAMTokenTypeFirstPartyAccess, `{}`, "", false, false, 400, 0},
		{"self authority", middleware.IAMTokenTypeFirstPartyAccess, `{"targets":[],"action":"read"}`, "", false, false, 400, 0},
		{"query", middleware.IAMTokenTypeFirstPartyAccess, string(body), "?tenant_id=3", false, false, 400, 0},
		{"trailing JSON", middleware.IAMTokenTypeFirstPartyAccess, string(body) + `{}`, "", false, false, 400, 0},
		{"wrong target response", middleware.IAMTokenTypeFirstPartyAccess, string(body), "", false, true, 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := sourceReadCheckAuth()
			if tc.tokenType == middleware.IAMTokenTypeServiceAccess {
				current = testIAMServiceActorContext("tenant", "addp-meta")
				current.Authorization.RoleAssignments = sourceReadCheckAuth().Authorization.RoleAssignments
			}
			current.Token.Type = tc.tokenType
			current.Authorization.RoleAssignments[0].Permissions = []string{engineaccess.MetaCatalogReadPermission, "system.engine.read"}
			if tc.noPermission {
				current.Authorization.RoleAssignments = []authorization.RoleAssignment{}
			}
			if tc.tokenType == middleware.IAMTokenTypeDelegatedAccess {
				current.Client.Audiences = []string{"meta"}
				current.Client.ScopeMode = "restricted"
				current.Client.Scopes = []string{"resource.children.list"}
				switch tc.name {
				case "ancestors":
					current.Client.Scopes = []string{"resource.ancestors.get"}
				case "wrong audience":
					current.Client.Audiences = []string{"manager"}
				case "wrong scope":
					current.Client.Scopes = []string{"data.preview"}
				case "multiple scopes":
					current.Client.Scopes = []string{"resource.ancestors.get", "resource.children.list"}
				}
				current.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *current.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
			}
			authenticate, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &current})
			if err != nil {
				t.Fatal(err)
			}
			credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
			if err != nil {
				t.Fatal(err)
			}
			fixture := &metadataCheckFixture{wrongTarget: tc.wrongTarget}
			router := gin.New()
			if err := RegisterEngineAccessMetadataCheckRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, UserAccessCredential: credential}, &EngineAccessMetadataCheckHandler{service: fixture}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api/v1/system/engine-access/metadata-checks/meta-query"+tc.query, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer addp_at_test")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || fixture.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, fixture.calls, response.Body.String())
			}
			if tc.tokenType == middleware.IAMTokenTypeDelegatedAccess {
				businessCredential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess, middleware.IAMTokenTypeDelegatedAccess, middleware.IAMTokenTypeServiceAccess)
				if err != nil {
					t.Fatal(err)
				}
				if err := RegisterIAMMigratedBusinessRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, UserAccessCredential: credential, BusinessCredential: businessCredential}, &EngineHandler{}, &APIConsumerHandler{}, &CleanupHandler{}); err != nil {
					t.Fatal(err)
				}
				adjacent := httptest.NewRecorder()
				adjacentRequest := httptest.NewRequest("GET", "/api/v1/system/engines", nil)
				adjacentRequest.Header.Set("Authorization", "Bearer addp_dat_meta")
				router.ServeHTTP(adjacent, adjacentRequest)
				if adjacent.Code != 403 {
					t.Fatalf("delegate reached adjacent System route: %d", adjacent.Code)
				}
			}
			if tc.status == 200 {
				var result authorization.SourceMetadataCheck
				if json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.Matches(authorization.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{target}}) || result.Targets[0].Visible || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("unsafe visible response")
				}
			}
		})
	}
}

func (f *metadataCheckFixture) CheckMetaLineageRead(ctx context.Context, credential string, request authorization.SourceMetadataCheckRequest) (*authorization.SourceMetadataCheck, error) {
	return f.CheckMetaMetadataRead(ctx, credential, request)
}

func TestLineageMetadataCheckUsesLineagePermissionWithoutDelegation(t *testing.T) {
	target := plugin.TabularItemPath(12, "schema", "public", "C")
	body, _ := json.Marshal(authorization.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{target}})
	for _, tc := range []struct {
		name, token, permission string
		status                  int
	}{
		{"lineage only", middleware.IAMTokenTypeFirstPartyAccess, engineaccess.MetaLineageReadPermission, 200},
		{"catalog only", middleware.IAMTokenTypeFirstPartyAccess, engineaccess.MetaCatalogReadPermission, 403},
		{"delegate", middleware.IAMTokenTypeDelegatedAccess, engineaccess.MetaLineageReadPermission, 403},
		{"machine", middleware.IAMTokenTypeServiceAccess, engineaccess.MetaLineageReadPermission, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := sourceReadCheckAuth()
			current.Token.Type = tc.token
			current.Authorization.RoleAssignments[0].Permissions = []string{tc.permission}
			if tc.token == middleware.IAMTokenTypeDelegatedAccess {
				current.Client.Audiences = []string{"meta"}
				current.Client.ScopeMode = "restricted"
				current.Client.Scopes = []string{"resource.children.list"}
				current.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *current.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
			}
			authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &current})
			if err != nil {
				t.Fatal(err)
			}
			credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
			if err != nil {
				t.Fatal(err)
			}
			fixture := &metadataCheckFixture{}
			router := gin.New()
			if err := RegisterEngineAccessMetadataCheckRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: credential}, &EngineAccessMetadataCheckHandler{service: fixture}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api/v1/system/engine-access/metadata-checks/meta-lineage", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer addp_at_test")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			expected := 0
			if tc.status == 200 {
				expected = 1
			}
			if fixture.calls != expected {
				t.Fatalf("authority called %d times", fixture.calls)
			}
		})
	}
}
