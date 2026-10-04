package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type grantRuntimeFixture struct {
	issues, queries int
	actor           engineaccess.FulfillmentRuntimeActor
	id              uuid.UUID
	binding         shared.SharingFulfillmentBinding
	grant           *shared.SharingFulfillmentGrant
	err             error
}

func (f *grantRuntimeFixture) IssueFulfillmentGrant(_ context.Context, actor engineaccess.FulfillmentRuntimeActor, id uuid.UUID, binding shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentGrant, error) {
	f.issues++
	f.actor = actor
	f.id, f.binding = id, binding
	return f.grant, f.err
}

func (f *grantRuntimeFixture) ResolveFulfillmentGrant(_ context.Context, actor engineaccess.FulfillmentRuntimeActor, id uuid.UUID, binding shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentGrantLookup, error) {
	f.queries++
	f.actor = actor
	f.id, f.binding = id, binding
	if f.err != nil {
		return nil, f.err
	}
	return &shared.SharingFulfillmentGrantLookup{Found: f.grant != nil, Grant: f.grant}, nil
}

func grantRuntimeRouter(t *testing.T, ac *shared.AuthContext, service fulfillmentGrantService) *gin.Engine {
	t.Helper()
	authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: ac})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	if err := RegisterEngineAccessFulfillmentGrantRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, ServiceCredential: credential}, &EngineAccessFulfillmentGrantHandler{service: service}); err != nil {
		t.Fatal(err)
	}
	return router
}

func TestFulfillmentGrantHTTPIdentityBindingAndHistory(t *testing.T) {
	ac := testIAMServiceActorContext("tenant", "addp-catalog")
	clientID := "addp-catalog"
	ac.Client.ClientID = &clientID
	ac.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.catalog_runtime", SourceType: "bootstrap", ValidFrom: time.Now().Add(-time.Minute),
		Scope: shared.AssignmentScope{Type: "tenant", TenantID: ac.Context.TenantID}, Permissions: []string{engineaccess.FulfillmentReconcilePermission}}}
	binding := shared.SharingFulfillmentBinding{CallerPrincipalID: 41, Operator: shared.SharingFulfillmentOperator{PrincipalID: 3, MembershipID: 4, AuthorizationVersion: 5},
		Path: plugin.TabularItemPath(12, "schema", "public", "fixture"), DecisionID: uuid.New(), RequirementVersion: 1, RecipientType: "user", RecipientID: 7, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked}
	id := uuid.New()
	prefix := "/api/v1/system/runtime/engine-access-fulfillments/"
	path := prefix + id.String() + "/grant"
	f := &grantRuntimeFixture{}
	router := grantRuntimeRouter(t, &ac, f)
	miss := engineDelegationTestRequest(t, router, "POST", path+"/resolve", binding, 200)
	if miss.Body.String() != `{"found":false}` || f.issues != 0 || f.queries != 1 {
		t.Fatalf("lookup issued or confused absence: %s %+v", miss.Body.String(), f)
	}
	f.grant = &shared.SharingFulfillmentGrant{RequestID: id, GrantedAt: time.Now().UTC()}
	for _, suffix := range []string{"", "/resolve"} {
		response := engineDelegationTestRequest(t, router, "POST", path+suffix, binding, 200)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body) != 2 {
			t.Fatalf("unexpected history fields: %s %v", response.Body.String(), err)
		}
		if suffix == "/resolve" {
			grant := body["grant"]
			found := string(body["found"]) == "true"
			body = nil
			if !found || json.Unmarshal(grant, &body) != nil || len(body) != 2 {
				t.Fatalf("bad found history: %s", response.Body.String())
			}
		}
		if string(body["request_id"]) != `"`+id.String()+`"` || string(body["granted_at"]) == "" {
			t.Fatalf("lost grant identity/time: %s", response.Body.String())
		}
	}
	if f.issues != 1 || f.queries != 2 || f.actor.ClientID != clientID || f.actor.TenantID != 3 || f.actor.PrincipalID != 41 || f.actor.MembershipID != 4 || !f.actor.TokenExpiresAt.Equal(ac.Token.ExpiresAt) {
		t.Fatalf("lost authenticated actor: %+v", f)
	}
	if f.id != id || !reflect.DeepEqual(f.binding, binding) {
		t.Fatal("handler changed the original immutable binding")
	}
	encoded, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	var withExtra map[string]any
	if err := json.Unmarshal(encoded, &withExtra); err != nil {
		t.Fatal(err)
	}
	withExtra["tenant_id"] = "2"
	for _, suffix := range []string{"", "/resolve"} {
		for _, body := range []any{nil, withExtra, map[string]any{"active": true}} {
			engineDelegationTestRequest(t, router, "POST", path+suffix, body, 400)
		}
		engineDelegationTestRequest(t, router, "POST", path+suffix+"?tenant_id=2", binding, 400)
		engineDelegationTestRequest(t, router, "POST", prefix+uuid.Nil.String()+"/grant"+suffix, binding, 400)
		engineDelegationTestRequest(t, router, "POST", prefix+"BAD/grant"+suffix, binding, 400)
		forged := binding
		forged.CallerPrincipalID++
		engineDelegationTestRequest(t, router, "POST", path+suffix, forged, 403)
		clientID = "addp-meta"
		engineDelegationTestRequest(t, router, "POST", path+suffix, binding, 403)
		clientID = "addp-catalog"
		permissions := ac.Authorization.RoleAssignments
		ac.Authorization.RoleAssignments = []shared.RoleAssignment{}
		engineDelegationTestRequest(t, router, "POST", path+suffix, binding, 403)
		ac.Authorization.RoleAssignments = permissions
		for _, incompatible := range []shared.AuthContext{testIAMActorContext("tenant"), testIAMServiceActorContext("platform", "addp-catalog")} {
			engineDelegationTestRequest(t, grantRuntimeRouter(t, &incompatible, f), "POST", path+suffix, binding, 403)
		}
		unauthenticated := httptest.NewRecorder()
		grantRuntimeRouter(t, nil, f).ServeHTTP(unauthenticated, httptest.NewRequest("POST", path+suffix, nil))
		if unauthenticated.Code != 401 {
			t.Fatalf("missing credential: %d", unauthenticated.Code)
		}
		engineDelegationTestRequest(t, router, "GET", path+suffix, nil, 404)
	}
	if f.issues != 1 || f.queries != 2 {
		t.Fatal("denied/invalid request reached grant service")
	}
}

func TestFulfillmentGrantHTTPStableLocalizedErrors(t *testing.T) {
	ac := testIAMServiceActorContext("tenant", "addp-catalog")
	clientID := "addp-catalog"
	ac.Client.ClientID = &clientID
	ac.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.catalog_runtime", SourceType: "bootstrap", ValidFrom: time.Now().Add(-time.Minute),
		Scope: shared.AssignmentScope{Type: "tenant", TenantID: ac.Context.TenantID}, Permissions: []string{engineaccess.FulfillmentReconcilePermission}}}
	binding := shared.SharingFulfillmentBinding{CallerPrincipalID: 41, Operator: shared.SharingFulfillmentOperator{PrincipalID: 3, MembershipID: 4, AuthorizationVersion: 5},
		Path: plugin.TabularItemPath(12, "schema", "public", "fixture"), DecisionID: uuid.New(), RequirementVersion: 1, RecipientType: "user", RecipientID: 7, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked}
	encoded, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/system/runtime/engine-access-fulfillments/" + uuid.NewString() + "/grant"
	for _, tc := range []struct {
		err          error
		code, zh, en string
	}{
		{engineaccess.ErrGrantWindowExpired, "engine_access_grant_window_expired", "原自动办理窗口已到期；请重新核验并发起新的办理，不延续旧依据", "The original fulfillment window has expired; revalidate and start a new fulfillment without reusing the old basis"},
		{engineaccess.ErrFulfillmentAlreadyClosed, "engine_access_fulfillment_closed", "原办理请求已关闭，不能签发新授权", "The original fulfillment request is closed; no new Grant can be issued"},
		{engineaccess.ErrFulfillmentBindingConflict, "engine_access_fulfillment_binding_conflict", "本次参数与原办理请求不一致，请查询原请求，不得改变范围或期限", "Parameters differ from the original fulfillment; resolve the original request without changing its scope or expiry"},
	} {
		for language, message := range map[string]string{"zh-CN": tc.zh, "en": tc.en} {
			for _, suffix := range []string{"", "/resolve"} {
				req := httptest.NewRequest("POST", path+suffix, strings.NewReader(string(encoded)))
				req.Header.Set("Authorization", "Bearer addp_at_fixture")
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept-Language", language)
				response := httptest.NewRecorder()
				grantRuntimeRouter(t, &ac, &grantRuntimeFixture{err: errors.Join(commonapi.ErrConflict, tc.err)}).ServeHTTP(response, req)
				var body IAMErrorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 409 || body.ErrorCode == nil || *body.ErrorCode != tc.code || body.Error != message {
					t.Fatalf("language=%s status=%d body=%s err=%v", language, response.Code, response.Body.String(), err)
				}
			}
		}
	}
	for failure, status := range map[error]int{commonapi.ErrForbidden: 403, commonapi.ErrUnauthorized: 401, errors.New("storage failed"): 500} {
		for _, suffix := range []string{"", "/resolve"} {
			engineDelegationTestRequest(t, grantRuntimeRouter(t, &ac, &grantRuntimeFixture{err: failure}), "POST", path+suffix, binding, status)
		}
	}
}
