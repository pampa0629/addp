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
		projectGroups: []CatalogProjectGroupProjection{
			{ID: 5, Name: "Delivery", Code: "delivery", Status: "active"},
			{ID: 6, Name: "Archived", Code: "archived", Status: "closed"},
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
		{SubjectType: CatalogSubjectTypeProjectGroup, ID: 5},
		{SubjectType: CatalogSubjectTypeProjectGroup, ID: 6},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(results) != 7 || !results[0].Referenceable || results[1].Found || results[2].Referenceable || results[3].Referenceable || !results[4].Referenceable || !results[5].Referenceable || results[6].Referenceable {
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
	if _, err := service.Resolve(context.Background(), 7, "addp-catalog", []CatalogReference{{SubjectType: "workspace", ID: 1}}); !errors.Is(err, ErrInvalidCatalogReferenceRequest) {
		t.Fatalf("invalid subject error = %v", err)
	}
}

func TestCatalogReferenceServiceListsCandidatesForCatalogOnly(t *testing.T) {
	service, err := NewCatalogReferenceService(&fakeCatalogReferenceRepository{})
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := service.ListCandidates(context.Background(), 7, "addp-catalog", CatalogSubjectTypeDepartment, "sales", 1, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0].Name != "Sales" {
		t.Fatalf("items = %#v, total=%d, err=%v", items, total, err)
	}
	if _, _, err := service.ListCandidates(context.Background(), 7, "addp-asset", CatalogSubjectTypeUser, "", 1, 20); !errors.Is(err, ErrInvalidCatalogReferenceRequest) {
		t.Fatalf("other client error = %v", err)
	}
}

type fakeCatalogReferenceRepository struct {
	departments   []CatalogDepartmentProjection
	users         []CatalogUserProjection
	projectGroups []CatalogProjectGroupProjection
}

func (r *fakeCatalogReferenceRepository) ResolveCatalogDepartments(context.Context, int64, []int64) ([]CatalogDepartmentProjection, error) {
	return r.departments, nil
}

func (r *fakeCatalogReferenceRepository) ResolveCatalogUsers(context.Context, int64, []int64) ([]CatalogUserProjection, error) {
	return r.users, nil
}

func (r *fakeCatalogReferenceRepository) ResolveCatalogProjectGroups(context.Context, int64, []int64) ([]CatalogProjectGroupProjection, error) {
	return r.projectGroups, nil
}

func (r *fakeCatalogReferenceRepository) ListCatalogDepartmentCandidates(context.Context, int64, string, int, int) ([]CatalogReferenceCandidate, int64, error) {
	items := []CatalogReferenceCandidate{{SubjectType: CatalogSubjectTypeDepartment, ID: 1, Name: "Sales", Code: "sales", Status: "active"}}
	return items, int64(len(items)), nil
}

func (r *fakeCatalogReferenceRepository) ListCatalogUserCandidates(context.Context, int64, string, int, int) ([]CatalogReferenceCandidate, int64, error) {
	items := []CatalogReferenceCandidate{{SubjectType: CatalogSubjectTypeUser, ID: 3, Name: "Alice", Code: "alice", Status: "active"}}
	return items, int64(len(items)), nil
}
