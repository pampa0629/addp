package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestPlatformPublicationRouteRequiresFixedAuthorizedPlatformMachine(t *testing.T) {
	actor := testIAMServiceActorContext("platform", "addp-ontology")
	resolver := &iamActorResolver{authContext: &actor}
	authentication, err := middleware.NewIAMAuthenticationMiddleware(resolver)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &IAMRuntime{Authentication: authentication, ServiceCredential: credential}
	router := gin.New()
	if err := RegisterPlatformPublicationCheckRoute(router.Group("/api/v1/system"), runtime); err != nil {
		t.Fatal(err)
	}
	grant := func() {
		actor.Authorization.RoleAssignments = []authorization.RoleAssignment{{AssignmentID: "901", RoleKey: "platform.ontology_runtime", Scope: authorization.AssignmentScope{Type: actor.Context.Type, TenantID: actor.Context.TenantID}, Permissions: []string{authorization.PlatformDefinitionPublishPermission}, SourceType: "bootstrap", ValidFrom: actor.Token.IssuedAt}}
	}
	binding := authorization.PlatformPublicationCheck{Capability: "transfer.task.create", Revision: "2", Digest: strings.Repeat("a", 64)}
	data, _ := json.Marshal(binding)
	request := func(query, body, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/system/runtime/platform-definition-publication-checks"+query, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
		}
		return w
	}
	grant()
	w := request("", string(data), "addp_at_test", 200)
	var observation authorization.PlatformPublicationObservation
	if err := json.Unmarshal(w.Body.Bytes(), &observation); err != nil || observation.Validate(binding) != nil || observation.PrincipalID != actor.Principal.ID || observation.AuthorizationVersion != actor.Authorization.AuthorizationVersion || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response=%s %v", w.Body.String(), err)
	}
	request("", string(data), "", 401)
	for _, client := range []string{"addp-agent", "addp-system", "addp-monitor"} {
		actor = testIAMServiceActorContext("platform", client)
		grant()
		request("", string(data), "addp_at_test", 403)
	}
	actor = testIAMServiceActorContext("tenant", "addp-ontology")
	grant()
	request("", string(data), "addp_at_test", 403)
	actor = testIAMActorContext("platform")
	grant()
	request("", string(data), "addp_at_test", 403)
	actor = testIAMServiceActorContext("platform", "addp-ontology")
	request("", string(data), "addp_at_test", 403)
	grant()
	for _, body := range []string{"", "null", "{}", string(data) + "{}", strings.TrimSuffix(string(data), "}") + `,"tenant_id":"0"}`, strings.TrimSuffix(string(data), "}") + `,"payload":{}}`, strings.Replace(string(data), `"revision":"2"`, `"revision":"2","revision":"3"`, 1), string(data) + strings.Repeat(" ", 2048)} {
		request("", body, "addp_at_test", 400)
	}
	request("?revision=3", string(data), "addp_at_test", 400)
	actor.Token.Type = middleware.IAMTokenTypeOAuthAccess
	request("", string(data), "addp_at_test", 403)
}

func TestPlatformPublicationRouteRequiresRuntimeDependencies(t *testing.T) {
	if RegisterPlatformPublicationCheckRoute(nil, nil) == nil {
		t.Fatal("accepted missing runtime")
	}
	if RegisterPlatformPublicationCheckRoute(gin.New().Group("/api/v1/system"), &IAMRuntime{}) == nil {
		t.Fatal("accepted missing guards")
	}
}
