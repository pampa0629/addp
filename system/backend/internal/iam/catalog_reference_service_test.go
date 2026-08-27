package iam

import (
	"context"
	"errors"
	"testing"
)

func TestCatalogReferenceServicePreservesOrderAndUsesCurrentTenantMembership(t *testing.T) {
	repository := &fakeCatalogReferenceRepository{
		departments: []CatalogDepartmentProjection{
			{ID: 1, Name: "Sales", Code: "sales", Status: "active"},
			{ID: 2, Name: "Legacy", Code: "legacy", Status: "disabled"},
		},
		users: []CatalogUserProjection{
			{ID: 3, DisplayName: "Alice", PrincipalStatus: "active", MembershipStatus: "ended", Referenceable: false},
			{ID: 3, DisplayName: "Alice", PrincipalStatus: "active", MembershipStatus: "active", Referenceable: true},
			{ID: 4, DisplayName: "Bob", PrincipalStatus: "suspended", MembershipStatus: "active", Referenceable: false},
		},
	}
	service, err := NewCatalogReferenceService(repository)
	if err != nil {
		t.Fatal(err)
	}
	results, err := service.Resolve(context.Background(), 7, "addp-catalog", []CatalogReference{
		{SubjectType: CatalogSubjectTypeUser, ID: 3},
		{SubjectType: CatalogSubjectTypeDepartment, ID: 99},
		{SubjectType: CatalogSubjectTypeDepartment, ID: 2},
		{SubjectType: CatalogSubjectTypeUser, ID: 4},
		{SubjectType: CatalogSubjectTypeDepartment, ID: 1},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(results) != 5 || !results[0].Referenceable || results[1].Found || results[2].Referenceable || results[3].Referenceable || !results[4].Referenceable {
		t.Fatalf("results = %#v", results)
	}
	if results[0].MembershipStatus != "active" {
		t.Fatalf("selected user membership = %#v", results[0])
	}
}

func TestCatalogReferenceServiceRejectsOtherClientsAndInvalidSubjects(t *testing.T) {
	service, err := NewCatalogReferenceService(&fakeCatalogReferenceRepository{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(context.Background(), 7, "addp-asset", []CatalogReference{{SubjectType: CatalogSubjectTypeUser, ID: 1}}); !errors.Is(err, ErrInvalidCatalogReferenceRequest) {
		t.Fatalf("other client error = %v", err)
	}
	if _, err := service.Resolve(context.Background(), 7, "addp-catalog", []CatalogReference{{SubjectType: "project_group", ID: 1}}); !errors.Is(err, ErrInvalidCatalogReferenceRequest) {
		t.Fatalf("invalid subject error = %v", err)
	}
}

type fakeCatalogReferenceRepository struct {
	departments []CatalogDepartmentProjection
	users       []CatalogUserProjection
}

func (r *fakeCatalogReferenceRepository) ResolveCatalogDepartments(context.Context, int64, []int64) ([]CatalogDepartmentProjection, error) {
	return r.departments, nil
}

func (r *fakeCatalogReferenceRepository) ResolveCatalogUsers(context.Context, int64, []int64) ([]CatalogUserProjection, error) {
	return r.users, nil
}
