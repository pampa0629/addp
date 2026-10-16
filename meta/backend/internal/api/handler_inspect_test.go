package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/service"
)

func TestInfraInspectRequiresTransferTenantServiceBeforeReadingBody(t *testing.T) {
	const permission = "meta.inspect.execute"
	contexts := map[string]authorization.AuthContext{
		"transfer":    authorizationFactsProjection("addp-transfer", "7", "service_access_token", permission),
		"denied":      authorizationFactsProjection("addp-transfer", "7", "service_access_token", ""),
		"manager":     authorizationFactsProjection("addp-manager", "7", "service_access_token", permission),
		"third-party": authorizationFactsProjection("external-importer", "7", "service_access_token", permission),
		"user":        authorizationFactsProjection("addp-web", "7", "first_party_access_token", permission),
	}
	oauth := contexts["user"]
	oauth.Token.Type = "oauth_access_token"
	oauth.Client.ScopeMode, oauth.Client.Scopes = "restricted", []string{permission}
	contexts["oauth"] = oauth
	platform := authorization.CloneAuthContext(contexts["transfer"])
	platform.Context = authorization.AuthSessionContext{Type: "platform"}
	platform.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
	contexts["platform"] = platform

	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projection, ok := contexts[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(projection)
	}))
	t.Cleanup(system.Close)
	db := metatest.OpenMetadataDB(t)
	engine := service.NewEngineService(db, nil)
	cfg := &config.Config{}
	cfg.SystemServiceURL = system.URL
	router := SetupRouter(cfg, db, engine, service.NewScanService(db, engine), nil, nil, nil, nil, modulelifecycle.NewStandalone("meta"))
	for credential := range contexts {
		t.Run(credential, func(t *testing.T) {
			body := &inspectRequestBody{Reader: strings.NewReader(`{"locator":"invalid"}`)}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/meta/inspect", body)
			request.Header.Set("Authorization", "Bearer "+credential)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := http.StatusForbidden
			if credential == "transfer" {
				want = http.StatusBadRequest // The authorized request reaches locator validation.
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
			}
			if (credential == "transfer") != body.read {
				t.Fatalf("request body read=%v: rejected identities must not reach inspection", body.read)
			}
		})
	}
}

type inspectRequestBody struct {
	*strings.Reader
	read bool
}

func (b *inspectRequestBody) Read(data []byte) (int, error) {
	b.read = true
	return b.Reader.Read(data)
}
