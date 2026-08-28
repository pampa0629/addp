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
		elements: []models.PublishedElementReference{{ID: 4, TenantID: 7, Name: "Customer ID", Code: "customer_id", Version: 5, Status: models.RevisionStatusPublished, LifecycleState: "deleting", RevisionID: 40, RevisionNo: 2}},
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

func TestReferenceResolutionServiceListsReferenceableCandidates(t *testing.T) {
	repository := &fakeReferenceResolutionRepository{
		elements: []models.PublishedElementReference{{ID: 4, Name: "Customer ID", Code: "customer_id", Status: models.RevisionStatusPublished, LifecycleState: "active", RevisionID: 40, RevisionNo: 2}},
	}
	result, err := NewReferenceResolutionService(repository).ListCandidates(context.Background(), 7, ReferenceTypeElement, " customer ", 1, 20)
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if result.Total != 1 || len(result.Data) != 1 || result.Data[0].ID != 4 || result.Data[0].ObjectType != ReferenceTypeElement {
		t.Fatalf("result = %#v", result)
	}
	if _, err := NewReferenceResolutionService(repository).ListCandidates(context.Background(), 7, "metric", "", 1, 20); !errors.Is(err, ErrInvalidReferenceResolutionRequest) {
		t.Fatalf("invalid type error = %v", err)
	}
	if _, err := NewReferenceResolutionService(repository).ListCandidates(context.Background(), 7, ReferenceTypeDomain, "", 1, 51); !errors.Is(err, ErrInvalidReferenceResolutionRequest) {
		t.Fatalf("oversized page error = %v", err)
	}
}

type fakeReferenceResolutionRepository struct {
	domains     []models.Domain
	glossaries  []models.Glossary
	elements    []models.PublishedElementReference
	domainIDs   []int64
	glossaryIDs []int64
	elementIDs  []int64
}

func (r *fakeReferenceResolutionRepository) ListDomainCandidates(context.Context, int64, string, int, int) ([]models.Domain, int64, error) {
	return r.domains, int64(len(r.domains)), nil
}

func (r *fakeReferenceResolutionRepository) ListGlossaryCandidates(context.Context, int64, string, int, int) ([]models.Glossary, int64, error) {
	return r.glossaries, int64(len(r.glossaries)), nil
}

func (r *fakeReferenceResolutionRepository) ListElementCandidates(context.Context, int64, string, int, int) ([]models.PublishedElementReference, int64, error) {
	return r.elements, int64(len(r.elements)), nil
}

func (r *fakeReferenceResolutionRepository) ResolveDomains(_ context.Context, _ int64, ids []int64) ([]models.Domain, error) {
	r.domainIDs = append([]int64(nil), ids...)
	return r.domains, nil
}

func (r *fakeReferenceResolutionRepository) ResolveGlossaries(_ context.Context, _ int64, ids []int64) ([]models.Glossary, error) {
	r.glossaryIDs = append([]int64(nil), ids...)
	return r.glossaries, nil
}

func (r *fakeReferenceResolutionRepository) ResolveElements(_ context.Context, _ int64, ids []int64) ([]models.PublishedElementReference, error) {
	r.elementIDs = append([]int64(nil), ids...)
	return r.elements, nil
}
