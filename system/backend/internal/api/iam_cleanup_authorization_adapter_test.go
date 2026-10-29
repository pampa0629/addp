package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/addp/common/execution"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestCleanupHandlerBindsServiceContextAndRejectsExtraIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeExecutionAuthorizationService{}
	handler, err := NewIAMExecutionAuthorizationHandler(fake, fakeExecutionEngineResolver{})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/grants/:id", func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, testIAMServiceActorContext("tenant", "addp-meta")); err != nil {
			t.Fatal(err)
		}
		c.Next()
	}, handler.AuthorizeCleanup)
	body := map[string]any{"execution_id": "12345678-1234-1234-1234-123456789abc", "stage": "effect", "attempt": 1, "lease_token": "22345678-1234-1234-1234-123456789abc",
		"scope": execution.CleanupAuthorizationScope{RequestID: "32345678-1234-1234-1234-123456789abc", ParentExecutionID: "42345678-1234-1234-1234-123456789abc", RequestDigest: strings.Repeat("a", 64), Module: "meta"}}
	response := performIAMJSONRequest(t, router, http.MethodPost, "/grants/91", body, nil)
	if response.Code != http.StatusOK || fake.cleanupInput.ServiceClientID != "addp-meta" || fake.cleanupInput.AuthorizationID != 91 {
		t.Fatalf("response %d %s", response.Code, response.Body.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["lease_token"]; ok {
		t.Fatal("lease token disclosed")
	}
	for _, field := range []string{"tenant_id", "actor_principal_id", "engine_id"} {
		body[field] = "6"
		response = performIAMJSONRequest(t, router, http.MethodPost, "/grants/91", body, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("accepted %s", field)
		}
		delete(body, field)
	}
	body["stage"] = "claim"
	body["attempt"] = 0
	if response = performIAMJSONRequest(t, router, http.MethodPost, "/grants/91", body, nil); response.Code != http.StatusBadRequest {
		t.Fatal("claim with lease accepted")
	}
	delete(body, "lease_token")
	if response = performIAMJSONRequest(t, router, http.MethodPost, "/grants/91", body, nil); response.Code != http.StatusOK || fake.cleanupInput.Stage != "claim" || fake.cleanupInput.Attempt != 0 {
		t.Fatal("valid pending claim rejected", response.Body.String())
	}
}
