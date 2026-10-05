package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/meilisearch/meilisearch-go"
)

func TestBuildAssetSearchFiltersPreservesUncategorizedSemantics(t *testing.T) {
	got := buildAssetSearchFilters(7, "dataset", []int64{-1})
	want := []string{
		"tenant_id = 7",
		`status = "published"`,
		`type_code = "dataset"`,
		"category_id IS NULL",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildAssetSearchFilters() = %#v, want %#v", got, want)
	}

	got = buildAssetSearchFilters(7, "", []int64{12, 15, 19})
	want = []string{"tenant_id = 7", `status = "published"`, "(category_id = 12 OR category_id = 15 OR category_id = 19)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildAssetSearchFilters() = %#v, want %#v", got, want)
	}
}

func TestAssetSDKSearchPreservesInt64AndTenantFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Filter string `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Filter != `tenant_id = 7 AND status = "published"` {
			t.Errorf("missing tenant filter: %q %v", request.Filter, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[{"id":9007199254740993}],"estimatedTotalHits":1}`))
	}))
	defer server.Close()
	index := &Indexer{client: meilisearch.New(server.URL, meilisearch.DisableRetries()), index: "assets", enabled: true}
	result, err := index.Search(7, "persons", "", nil, 10, 0)
	if err != nil || result.Total != 1 || len(result.IDs) != 1 || result.IDs[0] != 9007199254740993 {
		t.Fatalf("SDK hit decoding lost identity: %#v %v", result, err)
	}
}

func TestAssetSDKSubmissionDoesNotRetryServerFailure(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	defer server.Close()
	// A real constructor must also disable retries, before initialization succeeds.
	if _, err := NewIndexer(server.URL, "", "assets"); err == nil {
		t.Fatal("failed index setup accepted")
	}
	if requests.Load() != 1 {
		t.Fatalf("SDK unexpectedly retried submission: %d", requests.Load())
	}
}
