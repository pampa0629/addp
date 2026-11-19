package search

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestElasticsearchAssetProjectionRequests(t *testing.T) {
	var calls []string
	var callsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsMu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		callsMu.Unlock()
		if user, secret, ok := r.BasicAuth(); !ok || user != "owner" || secret != "fixture" {
			t.Error("search request did not use the configured identity")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/asset_published":
			fmt.Fprint(w, `{"acknowledged":true}`)
		case "/asset_published/_delete_by_query":
			fmt.Fprint(w, `{"timed_out":false,"failures":[],"version_conflicts":0}`)
		case "/asset_published/_bulk":
			if r.Header.Get("Content-Type") != "application/x-ndjson" || r.URL.Query().Get("refresh") != "wait_for" {
				t.Error("projection writes did not use NDJSON and wait_for")
			}
			scanner := bufio.NewScanner(r.Body)
			scanner.Scan()
			var action map[string]map[string]string
			if err := json.Unmarshal(scanner.Bytes(), &action); err != nil || action["index"]["_id"] != "9" {
				t.Errorf("unexpected index action: %v, %v", action, err)
			}
			scanner.Scan()
			var doc AssetIndexDoc
			if err := json.Unmarshal(scanner.Bytes(), &doc); err != nil || doc.TenantID != 7 || doc.Status != "published" {
				t.Errorf("unexpected projection: %+v, %v", doc, err)
			}
			fmt.Fprint(w, `{"items":[{"index":{"status":201}}]}`)
		case "/asset_published/_search":
			var query object
			if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
				t.Error(err)
			}
			data, _ := json.Marshal(query)
			for _, want := range []string{`"tenant_id":7`, `"status":"published"`, `"type_code":"data"`, `"field":"category_id"`, `"from":2`} {
				if !strings.Contains(string(data), want) {
					t.Errorf("missing %s in %s", want, data)
				}
			}
			fmt.Fprint(w, `{"hits":{"total":{"value":3},"hits":[{"_id":"9223372036854775806"}]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	index, err := NewElasticsearchIndexer(server.URL, "owner", "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := index.ReplaceAssets([]AssetIndexDoc{{ID: 9, TenantID: 7, Name: "订单", Status: "published"}}); err != nil {
		t.Fatal(err)
	}
	result, err := index.Search(7, "订单", "data", []int64{-1}, 1, 2)
	if err != nil || result.Total != 3 || !reflect.DeepEqual(result.IDs, []int64{9223372036854775806}) {
		t.Fatalf("unexpected search result: %+v, %v", result, err)
	}
	want := []string{"PUT /asset_published", "POST /asset_published/_delete_by_query", "POST /asset_published/_bulk", "POST /asset_published/_search"}
	callsMu.Lock()
	defer callsMu.Unlock()
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("projection requests %v, want %v", calls, want)
	}
}
