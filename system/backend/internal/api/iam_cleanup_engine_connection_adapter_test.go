package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/addp/common/execution"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestCleanupConnectionHandlerUsesExactServiceAndClosedBody(t *testing.T) {
	fake := &fakeExecutionAuthorizationService{}
	handler, err := NewIAMExecutionAuthorizationHandler(fake, fakeExecutionEngineResolver{})
	if err != nil {
		t.Fatal(err)
	}
	identity := "transfer"
	router := gin.New()
	router.POST("/grants/:id", func(c *gin.Context) {
		actor := testIAMServiceActorContext("tenant", "addp-"+identity)
		if identity == "user" {
			actor = testIAMActorContext("tenant")
		}
		if err := sharedauth.SetAuthContextForGin(c, actor); err != nil {
			t.Fatal(err)
		}
	}, handler.AuthorizeCleanupEngineConnection)
	body := map[string]any{"execution_id": uuid.NewString(), "attempt": 1, "lease_token": uuid.NewString(), "engine_id": "9", "scope": execution.CleanupAuthorizationScope{RequestID: uuid.NewString(), ParentExecutionID: uuid.NewString(), RequestDigest: strings.Repeat("a", 64), Module: "transfer"}}
	response := performIAMJSONRequest(t, router, http.MethodPost, "/grants/91", body, nil)
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || fake.cleanupInput.Stage != "effect" || fake.cleanupInput.ServiceClientID != "addp-transfer" || fake.cleanupInput.TenantID != 3 {
		t.Fatalf("connection handler status=%d", response.Code)
	}
	var value map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal("invalid connection JSON")
	}
	if len(value) != 9 || value["purpose"] != execution.CleanupEngineConnectionPurpose || value["engine_id"] != "9" || value["lease_token"] != nil || len(value["engine"].(map[string]any)) != 6 {
		t.Fatal("connection response is not the closed purpose DTO")
	}
	for _, field := range []string{"tenant_id", "actor_principal_id", "purpose", "stage", "extra"} {
		body[field] = "caller-controlled"
		if response := performIAMJSONRequest(t, router, http.MethodPost, "/grants/91", body, nil); response.Code != 400 {
			t.Fatalf("untrusted field %s accepted", field)
		}
		delete(body, field)
	}
	for _, identity = range []string{"user", "meta"} {
		if response := performIAMJSONRequest(t, router, http.MethodPost, "/grants/91", body, nil); response.Code != 403 {
			t.Fatal("connection issued to wrong credential/Owner")
		}
	}
}
