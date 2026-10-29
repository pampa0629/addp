package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
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
	router := SetupRouter(NewCleanupResultHandler(nil), cfg, service.NewMetadataService(nil, nil, nil, nil, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
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

type apiImportSourceStore struct {
	calls  int
	source client.ManagerImportSource
}

func (*apiImportSourceStore) Create(context.Context, *models.ImportSource) error { return nil }
func (*apiImportSourceStore) BindTask(context.Context, string, uint, uint) error { return nil }
func (s *apiImportSourceStore) Verify(_ context.Context, tenant uint, r client.ManagerImportSourceRequest) (*client.ManagerImportSource, error) {
	s.calls++
	if !s.source.Matches(tenant, r) {
		return nil, gorm.ErrRecordNotFound
	}
	return &s.source, nil
}
func TestImportSourceProductionRouteRequiresTransferAndPrivateExactBinding(t *testing.T) {
	permission := "manager.import_provenance.read"
	identities := map[string]authorization.AuthContext{}
	for _, key := range []string{"transfer", "wrong-client", "no-permission", "foreign", "user"} {
		tenant := "7"
		if key == "foreign" {
			tenant = "8"
		}
		value := authtest.NewTenantUserAuthContext(tenant, "91", []string{permission})
		if key != "user" {
			value.Principal.Type = "service_principal"
			value.Token.Type = "service_access_token"
			value.Authentication.Methods = []string{"service_secret"}
			value.Authentication.AssuranceLevel = "not_applicable"
			id := "addp-transfer"
			if key == "wrong-client" {
				id = "addp-meta"
			}
			value.Client.ClientID = &id
		}
		if key == "no-permission" {
			value.Authorization.RoleAssignments[0].Permissions = []string{"system.engine.read"}
		}
		identities[key] = value
	}
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, ok := identities[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	defer system.Close()
	store := &apiImportSourceStore{source: client.ManagerImportSource{TenantID: 7, UploaderID: 31, TaskID: 42, SourceLocator: "addp-infra://minio/manager/tenant_7/import/day/upload/roads.shp?type=object"}}
	cfg := &config.Config{}
	cfg.SystemServiceURL = system.URL
	router := SetupRouter(NewCleanupResultHandler(nil), cfg, service.NewMetadataService(nil, nil, nil, nil, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, NewTaskProviderHandler(nil, nil, nil, nil, nil), NewImportHandler(service.NewImportService(nil, "manager", nil, nil, store)), nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, managerprotection.NewReadBoundary(&readBoundaryTestStore{}), modulelifecycle.NewStandalone("manager"), nil, nil)
	request := client.ManagerImportSourceRequest{ExecutionID: uuid.NewString(), TaskID: 42, PrincipalID: 31, SourceLocator: store.source.SourceLocator}
	for _, kind := range []string{"transfer", "wrong-client", "no-permission", "foreign", "user", "uploader", "task", "source", "unknown-field"} {
		t.Run(kind, func(t *testing.T) {
			token := kind
			r := request
			extra := false
			switch kind {
			case "uploader":
				token = "transfer"
				r.PrincipalID = 32
			case "task":
				token = "transfer"
				r.TaskID = 43
			case "source":
				token = "transfer"
				r.SourceLocator = strings.Replace(r.SourceLocator, "roads.shp", "other.shp", 1)
			case "unknown-field":
				token = "transfer"
				extra = true
			}
			body, _ := json.Marshal(r)
			if extra {
				body = append(body[:len(body)-1], []byte(`,"token":"private"}`)...)
			}
			httpRequest := httptest.NewRequest("POST", "/api/v1/manager/runtime/import-sources/verify", bytes.NewReader(body))
			httpRequest.Header.Set("Authorization", "Bearer "+token)
			httpRequest.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httpRequest)
			want := 403
			switch kind {
			case "transfer":
				want = 200
			case "foreign", "uploader", "task", "source":
				want = 404
			case "unknown-field":
				want = 400
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
			}
		})
	}
}
