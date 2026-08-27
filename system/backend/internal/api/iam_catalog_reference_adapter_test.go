package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

func TestIAMCatalogReferenceHandlerUsesCurrentTenantAndCanonicalIDs(t *testing.T) {
	service := &fakeIAMCatalogReferenceService{results: []iam.CatalogReferenceResolution{
		{SubjectType: iam.CatalogSubjectTypeDepartment, ID: 7, Found: true, Referenceable: true, Name: "Sales", Code: "sales", Status: "active"},
		{SubjectType: iam.CatalogSubjectTypeUser, ID: 9, Found: true, Referenceable: false, Name: "Alice", Status: "suspended", PrincipalStatus: "active", MembershipStatus: "suspended"},
	}}
	handler, err := NewIAMCatalogReferenceHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/resolve", withCatalogReferenceAuthContext(t, "addp-catalog"), handler.Resolve)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/resolve", strings.NewReader(`{"references":[{"subject_type":"department","id":"7"},{"subject_type":"user","id":"9"}]}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if service.tenantID != 3 || service.clientID != "addp-catalog" || len(service.references) != 2 || service.references[1].ID != 9 {
		t.Fatalf("service input = tenant:%d client:%q references:%#v", service.tenantID, service.clientID, service.references)
	}
	if body := response.Body.String(); !strings.Contains(body, `"id":"7"`) || !strings.Contains(body, `"membership_status":"suspended"`) {
		t.Fatalf("response = %s", body)
	}
}

func TestIAMCatalogReferenceHandlerRejectsOtherServiceClient(t *testing.T) {
	service := &fakeIAMCatalogReferenceService{}
	handler, err := NewIAMCatalogReferenceHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/resolve", withCatalogReferenceAuthContext(t, "addp-asset"), handler.Resolve)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/resolve", strings.NewReader(`{"references":[{"subject_type":"user","id":"9"}]}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || service.called {
		t.Fatalf("status = %d, called=%t, body=%s", response.Code, service.called, response.Body.String())
	}
}

type fakeIAMCatalogReferenceService struct {
	called     bool
	tenantID   int64
	clientID   string
	references []iam.CatalogReference
	results    []iam.CatalogReferenceResolution
}

func (s *fakeIAMCatalogReferenceService) Resolve(
	_ context.Context,
	tenantID int64,
	clientID string,
	references []iam.CatalogReference,
) ([]iam.CatalogReferenceResolution, error) {
	s.called = true
	s.tenantID = tenantID
	s.clientID = clientID
	s.references = append([]iam.CatalogReference(nil), references...)
	return s.results, nil
}

func withCatalogReferenceAuthContext(t *testing.T, clientID string) gin.HandlerFunc {
	t.Helper()
	issuedAt := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	tenantID := "3"
	membershipID := "11"
	return func(c *gin.Context) {
		authContext := commonauth.AuthContext{
			SchemaVersion:  commonauth.AuthContextSchemaVersion,
			Principal:      commonauth.AuthPrincipal{Type: "service_principal", ID: "5"},
			Context:        commonauth.AuthSessionContext{Type: "tenant", TenantID: &tenantID, TenantMembershipID: &membershipID},
			Authentication: commonauth.AuthenticationFacts{Methods: []string{"service_secret"}, AssuranceLevel: "not_applicable", AuthenticatedAt: issuedAt},
			Client:         commonauth.ClientConstraints{ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{}},
			Organization:   commonauth.OrganizationContext{Departments: []commonauth.DepartmentMembership{}, ProjectGroups: []commonauth.ProjectGroupMembership{}},
			Authorization:  commonauth.AuthorizationFacts{AuthorizationVersion: "1", RoleAssignments: []commonauth.RoleAssignment{}},
			Token:          commonauth.TokenFacts{Type: "service_access_token", IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour)},
		}
		if err := sharedauth.SetAuthContextForGin(c, authContext); err != nil {
			panic(err)
		}
		c.Next()
	}
}
