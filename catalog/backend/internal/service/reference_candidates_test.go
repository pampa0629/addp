package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
)

type fakeReferenceCandidateResolver struct {
	result        *ReferenceCandidateList
	err           error
	referenceType string
}

func TestDomainCandidateAdapterPreservesPathAcrossPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"object_type":"domain","id":7,"name":"VIP","code":"vip","status":"active","domain_path":["Customer","VIP"]}],"total":2,"page":2,"page_size":1,"total_pages":2}`))
	}))
	defer server.Close()
	client := commonClient.NewStandardClient(server.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}), server.Client())
	result, err := NewStandardClientCandidateResolver(client).ListReferenceCandidates(context.Background(), 7, "domain", "", 2, 1)
	if err != nil || len(result.Data) != 1 || result.Data[0].ID != "7" || !reflect.DeepEqual(result.Data[0].DomainPath, []string{"Customer", "VIP"}) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
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
