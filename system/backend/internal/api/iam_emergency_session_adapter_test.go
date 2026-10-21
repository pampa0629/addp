package api

import (
	"context"
	commonauth "github.com/addp/common/authorization"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type emergencyHTTPFake struct {
	calls  int
	input  iam.CreateEmergencySessionInput
	mutate iam.MutateEmergencySessionInput
}

func (f *emergencyHTTPFake) Create(_ context.Context, in iam.CreateEmergencySessionInput) (*iam.EmergencySessionRequest, error) {
	f.calls++
	f.input = in
	return &iam.EmergencySessionRequest{ID: in.ID, Version: 1}, nil
}
func (f *emergencyHTTPFake) Mutate(_ context.Context, in iam.MutateEmergencySessionInput, _ string) (*iam.EmergencySessionRequest, error) {
	f.calls++
	f.mutate = in
	return nil, commonapi.ErrConflict
}
func (f *emergencyHTTPFake) Get(context.Context, string, uuid.UUID) (*iam.EmergencySessionRequest, error) {
	f.calls++
	return nil, commonapi.ErrNotFound
}
func (f *emergencyHTTPFake) List(context.Context, string, int, int) ([]iam.EmergencySessionRequest, int64, error) {
	f.calls++
	return []iam.EmergencySessionRequest{}, 0, nil
}

func TestEmergencySessionHTTPStrictBindingAndUncertainRetryContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &emergencyHTTPFake{}
	h, _ := NewIAMEmergencySessionHandler(fake)
	router := gin.New()
	router.POST("/", h.Create)
	router.POST("/:id/execute", h.Execute)
	id := uuid.NewString()
	for _, body := range []string{`{}`, `{"id":"` + id + `","target_principal_id":9007199254740993}`, `{"id":"` + id + `","target_principal_id":"2","target_family_id":"3","tenant_id":"999"}`, `{"id":"` + id + `","target_principal_id":"2","target_family_id":"3","executor_principal_id":"4"}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != 400 || fake.calls != 0 {
			t.Fatalf("accepted caller-supplied boundary or malformed identity: %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"id":"`+id+`","target_principal_id":"9007199254740993","target_family_id":"3","reason":"suspicious session","incident_reference":"INC-1"}`))
	req.Header.Set("Authorization", "Bearer addp_at_test")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != 201 || fake.input.TargetPrincipalID != 9007199254740993 || fake.input.AccessToken != "addp_at_test" {
		t.Fatalf("canonical request mapping failed: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("receipt can be cached")
	}
	execution := uuid.NewString()
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/"+id+"/execute", strings.NewReader(`{"version":"4","execution_id":"`+execution+`"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "resource_version_conflict") || fake.mutate.ExecutionID.String() != execution {
		t.Fatal("execution conflict must preserve original receipt identity and never retry")
	}
}

func TestEmergencySessionRouteScopeAndExactPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"tenant", "platform"} {
		for _, allowed := range []bool{false, true} {
			fake := &emergencyHTTPFake{}
			h, _ := NewIAMEmergencySessionHandler(fake)
			auth := testIAMActorContext(kind)
			auth.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.emergency", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: kind, TenantID: auth.Context.TenantID}, Permissions: []string{"iam.user.read"}}}
			if allowed {
				auth.Authorization.RoleAssignments[0].Permissions = []string{"iam.emergency_session_revocation.read"}
			}
			runtime := &IAMRuntime{EmergencySessionHandler: h, Authentication: func(c *gin.Context) {
				if err := sharedauth.SetAuthContextForGin(c, auth); err != nil {
					t.Fatal(err)
				}
				c.Next()
			}, FirstPartyCredential: func(c *gin.Context) { c.Next() }}
			router := gin.New()
			if err := RegisterIAMEmergencySessionRoutes(router.Group(""), runtime); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/"+kind+"/emergency_session_requests", nil))
			expected := 403
			if allowed {
				expected = 200
			}
			if w.Code != expected {
				t.Fatalf("scope=%s allowed=%t got=%d", kind, allowed, w.Code)
			}
			other := "platform"
			if kind == "platform" {
				other = "tenant"
			}
			w = httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/"+other+"/emergency_session_requests", nil))
			if w.Code != 403 {
				t.Fatal("context guard bypassed")
			}
		}
	}
}
