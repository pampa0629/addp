package service

import (
	"context"
	"errors"
	"testing"
)

type fakeReferenceCandidateResolver struct {
	result        *ReferenceCandidateList
	err           error
	referenceType string
}

func (r *fakeReferenceCandidateResolver) ListReferenceCandidates(
	_ context.Context, _ int64, referenceType, _ string, _, _ int,
) (*ReferenceCandidateList, error) {
	r.referenceType = referenceType
	return r.result, r.err
}

func TestEntryServiceDispatchesReferenceCandidatesToFactOwner(t *testing.T) {
	standard := &fakeReferenceCandidateResolver{result: &ReferenceCandidateList{
		Data:  []ReferenceCandidate{{ReferenceType: ReferenceCandidateDomain, ID: "7", Name: "Sales", Code: "sales", Status: "active"}},
		Total: 1, Page: 1, PageSize: 20, TotalPages: 1,
	}}
	system := &fakeReferenceCandidateResolver{}
	service := NewEntryService(nil, nil, nil).WithReferenceCandidateResolvers(standard, system)
	result, err := service.ListReferenceCandidates(context.Background(), 3, ReferenceCandidateDomain, "sales", 1, 20)
	if err != nil || result.Total != 1 || standard.referenceType != ReferenceCandidateDomain || system.referenceType != "" {
		t.Fatalf("result=%#v standard=%q system=%q err=%v", result, standard.referenceType, system.referenceType, err)
	}
}

func TestEntryServiceReferenceCandidatesFailWithoutOwnerFallback(t *testing.T) {
	standard := &fakeReferenceCandidateResolver{err: errors.New("owner unavailable")}
	service := NewEntryService(nil, nil, nil).WithReferenceCandidateResolvers(standard, nil)
	if _, err := service.ListReferenceCandidates(context.Background(), 3, ReferenceCandidateElement, "", 1, 20); !errors.Is(err, ErrReferenceValidationUnavailable) {
		t.Fatalf("unavailable owner error = %v", err)
	}
	if _, err := service.ListReferenceCandidates(context.Background(), 3, "engine", "", 1, 20); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("invalid type error = %v", err)
	}
}
