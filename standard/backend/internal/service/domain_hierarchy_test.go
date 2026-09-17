package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/addp/standard/internal/models"
)

func TestDomainHierarchyOrderPathsAndInvalidParents(t *testing.T) {
	parent, child := int64(1), int64(2)
	rows := []models.Domain{
		{ID: 3, Name: "Domestic", ParentID: &child},
		{ID: 2, Name: "VIP", ParentID: &parent},
		{ID: 4, Name: "Outdoor", SortOrder: -1},
		{ID: 1, Name: "Customer"},
	}
	tree, paths, err := buildDomainHierarchy(rows)
	if err != nil || len(tree) != 2 || tree[0].ID != 4 || tree[1].Children[0].ID != 2 || !reflect.DeepEqual(paths[3], []string{"Customer", "VIP", "Domestic"}) {
		t.Fatalf("tree=%#v paths=%#v err=%v", tree, paths, err)
	}
	for _, invalid := range [][]models.Domain{
		{{ID: 2, ParentID: &parent}},
		{{ID: 1, ParentID: &child}, {ID: 2, ParentID: &parent}},
		{{ID: 1}, {ID: 1}},
	} {
		if _, _, err := buildDomainHierarchy(invalid); err == nil {
			t.Fatalf("accepted malformed hierarchy: %#v", invalid)
		}
	}
}

func TestDomainCandidatesFilterAndPaginateAfterHierarchyOrdering(t *testing.T) {
	parent, child := int64(1), int64(2)
	repository := &fakeReferenceResolutionRepository{domains: []models.Domain{
		{ID: 4, Name: "Outdoor", Code: "outdoor", LifecycleState: "active"},
		{ID: 3, Name: "Domestic", Code: "domestic", ParentID: &child, LifecycleState: "active"},
		{ID: 2, Name: "VIP", Code: "customer_vip", ParentID: &parent, LifecycleState: "active"},
		{ID: 1, Name: "Customer", Code: "customer", LifecycleState: "active"},
		{ID: 5, Name: "Deleting", LifecycleState: "deleting"},
	}}
	svc := NewReferenceResolutionService(repository)
	for _, search := range []string{"", " customer "} {
		page, err := svc.ListCandidates(context.Background(), 7, ReferenceTypeDomain, search, 2, 1)
		if err != nil || len(page.Data) != 1 || page.Data[0].ID != 2 || !reflect.DeepEqual(page.Data[0].DomainPath, []string{"Customer", "VIP"}) {
			t.Fatalf("page=%#v err=%v", page, err)
		}
	}
	page, err := svc.ListCandidates(context.Background(), 7, ReferenceTypeDomain, "CUSTOMER / VIP / DOMESTIC", 1, 20)
	if err != nil || page.Total != 1 || page.Data[0].ID != 3 {
		t.Fatalf("path search=%#v err=%v", page, err)
	}
	page, err = svc.ListCandidates(context.Background(), 7, ReferenceTypeDomain, "", 100, 20)
	if err != nil || len(page.Data) != 0 || page.Total != 4 {
		t.Fatalf("empty page=%#v err=%v", page, err)
	}
	resolved, err := svc.Resolve(context.Background(), 7, []ReferenceResolutionRequest{{ObjectType: ReferenceTypeDomain, ID: 3}})
	if err != nil || !reflect.DeepEqual(resolved[0].DomainPath, []string{"Customer", "VIP", "Domestic"}) {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
}
