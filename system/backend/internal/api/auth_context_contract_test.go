package api

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSwaggerAuthorizationContextContract(t *testing.T) {
	type schemaRef struct {
		Ref string `json:"$ref"`
	}
	type response struct {
		Schema schemaRef `json:"schema"`
	}
	type swaggerOperation struct {
		Security  []map[string][]string `json:"security"`
		Responses map[string]response   `json:"responses"`
	}
	type pathItem struct {
		Get swaggerOperation `json:"get"`
	}
	type definition struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	type securityDefinition struct {
		Type string `json:"type"`
		Name string `json:"name"`
		In   string `json:"in"`
	}
	type swaggerDocument struct {
		Paths               map[string]pathItem           `json:"paths"`
		Definitions         map[string]definition         `json:"definitions"`
		SecurityDefinitions map[string]securityDefinition `json:"securityDefinitions"`
	}

	raw, err := os.ReadFile("../../docs/swagger.json")
	if err != nil {
		t.Fatalf("read swagger.json: %v", err)
	}

	var document swaggerDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode swagger.json: %v", err)
	}

	authOperation := document.Paths["/auth/context"].Get
	if len(authOperation.Security) != 1 {
		t.Fatalf("/auth/context security = %#v, want one BearerAuth requirement", authOperation.Security)
	}
	if _, ok := authOperation.Security[0]["BearerAuth"]; !ok {
		t.Fatalf("/auth/context security = %#v, want BearerAuth", authOperation.Security)
	}

	bearer := document.SecurityDefinitions["BearerAuth"]
	if bearer.Type != "apiKey" || bearer.Name != "Authorization" || bearer.In != "header" {
		t.Fatalf("BearerAuth = %#v, want Authorization header apiKey", bearer)
	}

	const definitionName = "authorization.AuthContext"
	const definitionRef = "#/definitions/" + definitionName
	if got := authOperation.Responses["200"].Schema.Ref; got != definitionRef {
		t.Fatalf("/auth/context 200 schema = %q, want %q", got, definitionRef)
	}

	properties := document.Definitions[definitionName].Properties
	requiredProperties := []string{
		"schema_version",
		"principal",
		"context",
		"organization",
		"authorization",
		"authentication",
		"client",
		"delegation",
		"token",
	}
	for _, property := range requiredProperties {
		if _, ok := properties[property]; !ok {
			t.Errorf("AuthorizationContext schema missing property %q", property)
		}
	}
}

func TestSwaggerOAuthRoutes(t *testing.T) {
	raw, err := os.ReadFile("../../docs/swagger.json")
	if err != nil {
		t.Fatalf("read swagger.json: %v", err)
	}
	var document struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode swagger.json: %v", err)
	}
	for _, path := range []string{
		"/auth/delegations",
		"/oauth/authorization_requests",
		"/oauth/authorization_requests/{request_id}",
		"/oauth/authorizations",
		"/oauth/device/code",
		"/oauth/device/authorizations",
		"/oauth/token",
		"/oauth/revoke",
		"/logout",
	} {
		if _, ok := document.Paths[path]; !ok {
			t.Errorf("swagger missing OAuth route %q", path)
		}
	}
}

func TestSwaggerOAuthClientCredentialsContract(t *testing.T) {
	raw, err := os.ReadFile("../../docs/swagger.json")
	if err != nil {
		t.Fatalf("read swagger.json: %v", err)
	}
	type parameter struct {
		Name     string   `json:"name"`
		In       string   `json:"in"`
		Required bool     `json:"required"`
		Enum     []string `json:"enum"`
	}
	type operation struct {
		Parameters []parameter `json:"parameters"`
		Responses  map[string]struct {
			Schema struct {
				Ref string `json:"$ref"`
			} `json:"schema"`
		} `json:"responses"`
	}
	var document struct {
		Paths map[string]struct {
			Post operation `json:"post"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode swagger.json: %v", err)
	}
	token := document.Paths["/oauth/token"].Post
	wantParameters := map[string]string{
		"Authorization": "header",
		"grant_type":    "formData",
		"scope":         "formData",
		"audience":      "formData",
		"tenant_id":     "formData",
		"context_type":  "formData",
	}
	for _, parameter := range token.Parameters {
		if wantIn, exists := wantParameters[parameter.Name]; exists {
			if parameter.In != wantIn {
				t.Errorf("/oauth/token parameter %s in = %q, want %q", parameter.Name, parameter.In, wantIn)
			}
			delete(wantParameters, parameter.Name)
		}
		if parameter.Name == "grant_type" {
			if !parameter.Required || !containsString(parameter.Enum, "client_credentials") {
				t.Errorf("/oauth/token grant_type = %#v", parameter)
			}
		}
	}
	if len(wantParameters) != 0 {
		t.Fatalf("/oauth/token missing parameters: %#v", wantParameters)
	}
	if got := token.Responses["200"].Schema.Ref; got != "#/definitions/internal_api.IAMOAuthTokenResponse" {
		t.Fatalf("/oauth/token 200 schema = %q", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
