package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/standard/internal/models"
)

func TestReferenceResolutionServicePreservesOrderAndReferenceability(t *testing.T) {
	repository := &fakeReferenceResolutionRepository{
		domains: []models.Domain{{ID: 1, TenantID: 7, Name: "Sales", Code: "sales", Version: 2, LifecycleState: "active"}},
		glossaries: []models.Glossary{
			{ID: 2, TenantID: 7, Name: "Customer", Version: 3, Status: "approved"},
			{ID: 3, TenantID: 7, Name: "Draft", Version: 1, Status: "draft"},
		},
		elements: []models.Element{{ID: 4, TenantID: 7, Name: "Customer ID", Code: "customer_id", Version: 5, Status: "approved", LifecycleState: "deleting"}},
	}
	service := NewReferenceResolutionService(repository)

	results, err := service.Resolve(context.Background(), 7, []ReferenceResolutionRequest{
		{ObjectType: ReferenceTypeGlossary, ID: 2},
		{ObjectType: ReferenceTypeDomain, ID: 99},
		{ObjectType: ReferenceTypeElement, ID: 4},
		{ObjectType: ReferenceTypeGlossary, ID: 3},
		{ObjectType: ReferenceTypeDomain, ID: 1},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(results) != 5 || results[0].ID != 2 || results[1].Found || results[2].Referenceable || results[3].Referenceable || !results[4].Referenceable {
		t.Fatalf("results = %#v", results)
	}
	if len(repository.domainIDs) != 2 || repository.domainIDs[0] != 99 || repository.domainIDs[1] != 1 {
		t.Fatalf("domain ids = %#v", repository.domainIDs)
	}
}

func TestReferenceResolutionServiceRejectsInvalidBatch(t *testing.T) {
	service := NewReferenceResolutionService(&fakeReferenceResolutionRepository{})
	for _, references := range [][]ReferenceResolutionRequest{
		nil,
		{{ObjectType: "metric", ID: 1}},
		{{ObjectType: ReferenceTypeDomain, ID: 0}},
	} {
		if _, err := service.Resolve(context.Background(), 7, references); !errors.Is(err, ErrInvalidReferenceResolutionRequest) {
			t.Fatalf("Resolve(%#v) error = %v", references, err)
		}
	}
}

type fakeReferenceResolutionRepository struct {
	domains     []models.Domain
	glossaries  []models.Glossary
	elements    []models.Element
	domainIDs   []int64
	glossaryIDs []int64
	elementIDs  []int64
}

func (r *fakeReferenceResolutionRepository) ResolveDomains(_ context.Context, _ int64, ids []int64) ([]models.Domain, error) {
	r.domainIDs = append([]int64(nil), ids...)
	return r.domains, nil
}

func (r *fakeReferenceResolutionRepository) ResolveGlossaries(_ context.Context, _ int64, ids []int64) ([]models.Glossary, error) {
	r.glossaryIDs = append([]int64(nil), ids...)
	return r.glossaries, nil
}

func (r *fakeReferenceResolutionRepository) ResolveElements(_ context.Context, _ int64, ids []int64) ([]models.Element, error) {
	r.elementIDs = append([]int64(nil), ids...)
	return r.elements, nil
}
