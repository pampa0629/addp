package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/manager/internal/config"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/addp/manager/internal/service"
)

func TestImportRouteChecksUserAndAllPermissionsBeforeReadingFiles(t *testing.T) {
	permissions := []string{"manager.data_item.create", "transfer.task.create", "transfer.task.execute"}
	identities := map[string]authorization.AuthContext{
		"allowed": authtest.NewTenantUserAuthContext("7", "31", permissions),
		"machine": authtest.NewTenantUserAuthContext("7", "91", permissions),
	}
	machine := identities["machine"]
	machine.Principal.Type, machine.Token.Type = "service_principal", "service_access_token"
	machine.Authentication.Methods, machine.Authentication.AssuranceLevel = []string{"service_secret"}, "not_applicable"
	clientID := "addp-manager"
	machine.Client.ClientID = &clientID
	identities["machine"] = machine
	for i, permission := range permissions {
		other := append([]string{}, permissions[:i]...)
		other = append(other, permissions[i+1:]...)
		identities[permission] = authtest.NewTenantUserAuthContext("7", "31", other)
	}
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := identities[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok || r.URL.Path != "/api/v1/system/auth/context" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(identity)
	}))
	defer authServer.Close()
	cfg := &config.Config{}
	cfg.SystemServiceURL = authServer.URL
	router := SetupRouter(cfg, service.NewMetadataService(nil, nil, nil, nil, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, NewTaskProviderHandler(nil, nil, nil, nil, nil), NewImportHandler(nil), nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, managerprotection.NewReadBoundary(&readBoundaryTestStore{}), modulelifecycle.NewStandalone("manager"), nil, nil)
	for token := range identities {
		t.Run(token, func(t *testing.T) {
			body := &uploadReadProbe{}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/manager/imports", body)
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Content-Type", "multipart/form-data; boundary=probe")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := http.StatusForbidden
			if token == "allowed" {
				want = http.StatusBadRequest
			} // authorized request reaches the unconfigured handler
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
			}
			if token != "allowed" && body.read {
				t.Fatal("request files read before identity/permission/configuration check")
			}
		})
	}
}
