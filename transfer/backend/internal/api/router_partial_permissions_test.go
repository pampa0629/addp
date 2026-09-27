package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	transferauthorization "github.com/addp/transfer/internal/authorization"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

func TestCreateOnlyTenantCanReadStaticCapabilitiesWithoutTaskRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer create-only": {
			ClientID:    "addp-web",
			Permissions: []string{transferauthorization.PermissionTransferTaskCreate},
		},
	})
	defer authServer.Close()

	router := SetupRouter(nil, nil, nil, nil, authServer.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
	for _, test := range []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{name: "capabilities with create only", path: "/api/v1/transfer/capabilities", token: "create-only", want: http.StatusOK},
		{name: "task list without read", path: "/api/v1/transfer/task-definitions", token: "create-only", want: http.StatusForbidden},
		{name: "capabilities without login", path: "/api/v1/transfer/capabilities", want: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestTenantTaskRoutesRespectPartialPermissionsAndTenantBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskHandlerTestDB(t)
	taskIDs := make(map[uint]uint)
	for _, task := range []models.TransferTask{
		{TenantID: 7, Name: "tenant-seven-task", TaskType: "sync", Config: models.JSONMap{}, Status: models.TaskStatusIdle},
		{TenantID: 8, Name: "tenant-eight-task", TaskType: "sync", Config: models.JSONMap{}, Status: models.TaskStatusIdle},
	} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("seed transfer task: %v", err)
		}
		taskIDs[task.TenantID] = task.ID
	}

	identities := map[string]struct {
		tenantID    string
		permissions []string
	}{
		"Bearer no-permission": {tenantID: "7"},
		"Bearer read-only":     {tenantID: "7", permissions: []string{transferauthorization.PermissionTransferTaskRead}},
		"Bearer create-only":   {tenantID: "7", permissions: []string{transferauthorization.PermissionTransferTaskCreate}},
		"Bearer execute-only":  {tenantID: "7", permissions: []string{transferauthorization.PermissionTransferTaskExecute}},
		"Bearer other-tenant":  {tenantID: "8", permissions: []string{transferauthorization.PermissionTransferTaskRead}},
	}
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}
		identity, ok := identities[r.Header.Get("Authorization")]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		authContext := authtest.NewTenantUserAuthContext(identity.tenantID, "91", identity.permissions)
		if len(identity.permissions) == 0 {
			authContext.Authorization.RoleAssignments = []authorization.RoleAssignment{}
		}
		if err := json.NewEncoder(w).Encode(authContext); err != nil {
			t.Errorf("encode AuthContext: %v", err)
		}
	}))
	defer authServer.Close()

	router := SetupRouter(service.NewTaskService(db, nil, nil), nil, nil, nil,
		authServer.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
	for _, tc := range []struct {
		name, method, path, token, body string
		want                            int
		contains, excludes              string
	}{
		{name: "anonymous list", method: http.MethodGet, path: "/api/v1/transfer/task-definitions", want: http.StatusUnauthorized},
		{name: "no permission list", method: http.MethodGet, path: "/api/v1/transfer/task-definitions", token: "no-permission", want: http.StatusForbidden},
		{name: "no permission create", method: http.MethodPost, path: "/api/v1/transfer/task-definitions", token: "no-permission", body: "{", want: http.StatusForbidden},
		{name: "read only list", method: http.MethodGet, path: "/api/v1/transfer/task-definitions", token: "read-only", want: http.StatusOK, contains: "tenant-seven-task", excludes: "tenant-eight-task"},
		{name: "read only own detail", method: http.MethodGet, path: fmt.Sprintf("/api/v1/transfer/task-definitions/%d", taskIDs[7]), token: "read-only", want: http.StatusOK, contains: "tenant-seven-task"},
		{name: "read only foreign detail", method: http.MethodGet, path: fmt.Sprintf("/api/v1/transfer/task-definitions/%d", taskIDs[8]), token: "read-only", want: http.StatusNotFound},
		{name: "read only cannot create", method: http.MethodPost, path: "/api/v1/transfer/task-definitions", token: "read-only", body: "{", want: http.StatusForbidden},
		{name: "read only cannot execute", method: http.MethodPost, path: fmt.Sprintf("/api/v1/transfer/task-definitions/%d/start", taskIDs[7]), token: "read-only", want: http.StatusForbidden},
		{name: "create only cannot list", method: http.MethodGet, path: "/api/v1/transfer/task-definitions", token: "create-only", want: http.StatusForbidden},
		{name: "create only reaches validation", method: http.MethodPost, path: "/api/v1/transfer/task-definitions", token: "create-only", body: "{", want: http.StatusBadRequest},
		{name: "execute only cannot list", method: http.MethodGet, path: "/api/v1/transfer/task-definitions", token: "execute-only", want: http.StatusForbidden},
		{name: "execute only reaches validation", method: http.MethodPost, path: "/api/v1/transfer/task-definitions/invalid/start", token: "execute-only", want: http.StatusBadRequest},
		{name: "other tenant list", method: http.MethodGet, path: "/api/v1/transfer/task-definitions", token: "other-tenant", want: http.StatusOK, contains: "tenant-eight-task", excludes: "tenant-seven-task"},
		{name: "other tenant detail", method: http.MethodGet, path: fmt.Sprintf("/api/v1/transfer/task-definitions/%d", taskIDs[7]), token: "other-tenant", want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("%s %s status = %d, want %d; body=%s", tc.method, tc.path, response.Code, tc.want, response.Body.String())
			}
			if tc.contains != "" && !strings.Contains(response.Body.String(), tc.contains) {
				t.Fatalf("response missing %q: %s", tc.contains, response.Body.String())
			}
			if tc.excludes != "" && strings.Contains(response.Body.String(), tc.excludes) {
				t.Fatalf("response unexpectedly contains %q: %s", tc.excludes, response.Body.String())
			}
		})
	}
}
