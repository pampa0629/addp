package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meilisearch/meilisearch-go"
)

func TestAssetIndexInitializationRequiresTerminalTasks(t *testing.T) {
	for _, test := range []struct {
		name            string
		exists          bool
		primaryKey      string
		readCode        string
		failedTask      int
		wantError       bool
		wantSubmissions int
	}{
		{name: "reuse existing index", exists: true, primaryKey: "id", wantSubmissions: 3},
		{name: "create missing index", wantSubmissions: 4},
		{name: "wrong primary key", exists: true, primaryKey: "other", wantError: true},
		{name: "read forbidden", readCode: "invalid_api_key", wantError: true},
		{name: "creation failed", failedTask: 1, wantError: true, wantSubmissions: 1},
		{name: "settings failed", exists: true, primaryKey: "id", failedTask: 1, wantError: true, wantSubmissions: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var submissions, completed int
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == "/indexes/assets" {
					if test.exists {
						fmt.Fprintf(w, `{"uid":"assets","primaryKey":%q}`, test.primaryKey)
					} else {
						code := test.readCode
						if code == "" {
							code = "index_not_found"
							w.WriteHeader(404)
						} else {
							w.WriteHeader(403)
						}
						// A misleading message must not authorize creation.
						fmt.Fprintf(w, `{"message":"index_not_found","code":%q,"type":"invalid_request"}`, code)
					}
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/tasks/%d", submissions) {
					status := "succeeded"
					if test.failedTask == submissions {
						status = "failed"
					}
					completed = submissions
					fmt.Fprintf(w, `{"uid":%d,"status":%q}`, submissions, status)
					return
				}
				if r.Method != http.MethodGet {
					if completed != submissions {
						t.Error("submitted next operation before previous task completed")
					}
					if test.exists && r.URL.Path == "/indexes" {
						t.Error("recreated existing index")
					}
					submissions++
					w.WriteHeader(202)
					fmt.Fprintf(w, `{"taskUid":%d,"status":"enqueued"}`, submissions)
					return
				}
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(500)
			}))
			defer server.Close()
			_, err := NewIndexer(server.URL, "", "assets")
			mu.Lock()
			defer mu.Unlock()
			if (err != nil) != test.wantError || submissions != test.wantSubmissions {
				t.Fatalf("error=%v submissions=%d; want error=%v submissions=%d", err, submissions, test.wantError, test.wantSubmissions)
			}
		})
	}
}

func TestAssetIndexTaskWaitIsBoundedAndDoesNotResubmit(t *testing.T) {
	var submissions atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/indexes/assets" {
			fmt.Fprint(w, `{"uid":"assets","primaryKey":"id"}`)
		} else if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"uid":0,"status":"processing"}`)
		} else {
			submissions.Add(1)
			w.WriteHeader(202)
			fmt.Fprint(w, `{"taskUid":0,"status":"enqueued"}`)
		}
	}))
	defer server.Close()
	index := &Indexer{client: meilisearch.New(server.URL, meilisearch.DisableRetries()), index: "assets", enabled: true}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := index.ensureIndex(ctx); err == nil {
		t.Fatal("unfinished task accepted")
	}
	if submissions.Load() != 1 {
		t.Fatalf("unexpected resubmission: %d", submissions.Load())
	}
}

func TestAssetProjectionRebuildWaitsForTerminalTasks(t *testing.T) {
	for _, test := range []struct {
		name            string
		docs            []AssetIndexDoc
		failedTask      int
		wantError       bool
		wantSubmissions int
	}{
		{name: "empty projection", wantSubmissions: 1},
		{name: "rebuild projection", docs: []AssetIndexDoc{{ID: 17, TenantID: 7}}, wantSubmissions: 2},
		{name: "clear failed", docs: []AssetIndexDoc{{ID: 17, TenantID: 7}}, failedTask: 1, wantError: true, wantSubmissions: 1},
		{name: "write failed", docs: []AssetIndexDoc{{ID: 17, TenantID: 7}}, failedTask: 2, wantError: true, wantSubmissions: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var submissions, completed int
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/tasks/%d", submissions-1) {
					status := "succeeded"
					if test.failedTask == submissions {
						status = "failed"
					}
					completed = submissions
					fmt.Fprintf(w, `{"uid":%d,"status":%q}`, submissions-1, status)
					return
				}
				if completed != submissions {
					t.Error("rebuild submitted before clearing completed")
				}
				if submissions == 0 && r.Method == http.MethodDelete && r.URL.Path == "/indexes/assets/documents" {
					// UID 0 is a valid terminal task, not a missing receipt.
				} else if submissions == 1 && r.Method == http.MethodPost && r.URL.Path == "/indexes/assets/documents" {
					var docs []AssetIndexDoc
					if err := json.NewDecoder(r.Body).Decode(&docs); err != nil || len(docs) != 1 || docs[0].ID != 17 || docs[0].TenantID != 7 || docs[0].Tags == nil {
						t.Errorf("invalid projection payload: %#v %v", docs, err)
					}
				} else {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
					return
				}
				w.WriteHeader(202)
				fmt.Fprintf(w, `{"taskUid":%d,"status":"enqueued"}`, submissions)
				submissions++
			}))
			defer server.Close()
			index := &Indexer{client: meilisearch.New(server.URL, meilisearch.DisableRetries()), index: "assets", enabled: true}
			err := index.ReplaceAssets(test.docs)
			mu.Lock()
			defer mu.Unlock()
			if (err != nil) != test.wantError || submissions != test.wantSubmissions || completed != submissions {
				t.Fatalf("error=%v submitted=%d completed=%d; want error=%v submitted=%d", err, submissions, completed, test.wantError, test.wantSubmissions)
			}
		})
	}
}

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
