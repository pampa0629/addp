package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	standardauthorization "github.com/addp/standard/internal/authorization"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReferenceResolutionRouteAllowsModelCodesButKeepsGlossariesCatalogOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:standard-reference-resolution-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer catalog-token": {ClientID: "addp-catalog", Permissions: []string{standardauthorization.PermissionStandardDomainRead, standardauthorization.PermissionStandardElementRead, standardauthorization.PermissionStandardGlossaryRead}},
		"Bearer model-token":   {ClientID: "addp-model", Permissions: []string{standardauthorization.PermissionStandardDomainRead, standardauthorization.PermissionStandardElementRead}},
		"Bearer asset-token":   {ClientID: "addp-asset", Permissions: []string{standardauthorization.PermissionStandardDomainRead, standardauthorization.PermissionStandardElementRead}},
		"Bearer incomplete":    {ClientID: "addp-model", Permissions: []string{standardauthorization.PermissionStandardDomainRead}},
	})
	defer authServer.Close()
	resolutionService := service.NewReferenceResolutionService(&referenceResolutionAPITestRepository{})
	router := SetupRouter(db, nil, nil, nil, nil, nil, nil, nil, resolutionService, nil, nil, authServer.URL, modulelifecycle.NewStandalone("standard"))

	codeBody := `{"references":[{"object_type":"domain","code":"sales"},{"object_type":"element","code":"customer_id"}]}`
	response := performTenantRequest(router, http.MethodPost, "/api/v1/standard/references/resolve", "model-token", codeBody)
	if response.Code != http.StatusOK {
		t.Fatalf("model code resolution status = %d; body=%s", response.Code, response.Body.String())
	}
	for _, testCase := range []struct {
		token string
		body  string
		want  int
	}{
		{"catalog-token", `{"references":[{"object_type":"glossary","code":"customer"}]}`, http.StatusOK},
		{"model-token", `{"references":[{"object_type":"glossary","code":"customer"}]}`, http.StatusForbidden},
		{"asset-token", codeBody, http.StatusForbidden},
		{"incomplete", codeBody, http.StatusForbidden},
	} {
		response := performTenantRequest(router, http.MethodPost, "/api/v1/standard/references/resolve", testCase.token, testCase.body)
		if response.Code != testCase.want {
			t.Fatalf("token %q status = %d, want %d; body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
}

type referenceResolutionAPITestRepository struct{}

func (*referenceResolutionAPITestRepository) ResolveDomains(context.Context, int64, []int64) ([]models.Domain, error) {
	return nil, nil
}
func (*referenceResolutionAPITestRepository) ResolveGlossaries(context.Context, int64, []int64) ([]models.PublishedGlossaryReference, error) {
	return nil, nil
}
func (*referenceResolutionAPITestRepository) ResolveElements(context.Context, int64, []int64) ([]models.PublishedElementReference, error) {
	return nil, nil
}
func (*referenceResolutionAPITestRepository) ResolveDomainsByCodes(context.Context, int64, []string) ([]models.Domain, error) {
	return []models.Domain{{ID: 1, Code: "sales", Name: "Sales", LifecycleState: "active", Version: 1}}, nil
}
func (*referenceResolutionAPITestRepository) ResolveGlossariesByCodes(context.Context, int64, []string) ([]models.PublishedGlossaryReference, error) {
	return []models.PublishedGlossaryReference{{ID: 2, Code: "customer", Name: "Customer", LifecycleState: "active", Status: models.RevisionStatusPublished, Version: 1, RevisionID: 20, RevisionNo: 1}}, nil
}
func (*referenceResolutionAPITestRepository) ResolveElementsByCodes(context.Context, int64, []string) ([]models.PublishedElementReference, error) {
	return []models.PublishedElementReference{{ID: 3, Code: "customer_id", Name: "Customer ID", LifecycleState: "active", Status: models.RevisionStatusPublished, Version: 1, RevisionID: 30, RevisionNo: 1}}, nil
}
func (*referenceResolutionAPITestRepository) ListDomainCandidates(context.Context, int64, string, int, int) ([]models.Domain, int64, error) {
	return nil, 0, nil
}
func (*referenceResolutionAPITestRepository) ListGlossaryCandidates(context.Context, int64, string, int, int) ([]models.PublishedGlossaryReference, int64, error) {
	return nil, 0, nil
}
func (*referenceResolutionAPITestRepository) ListElementCandidates(context.Context, int64, string, int, int) ([]models.PublishedElementReference, int64, error) {
	return nil, 0, nil
}
