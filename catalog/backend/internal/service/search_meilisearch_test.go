package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMeilisearchCatalogIndexReusesExistingIndex(t *testing.T) {
	t.Parallel()

	var createRequests atomic.Int64
	var nextTask atomic.Int64
	nextTask.Store(100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "available"})
		case r.Method == http.MethodGet && r.URL.Path == "/indexes/catalog_entries":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uid": "catalog_entries", "primaryKey": "id",
				"createdAt": time.Now().UTC(), "updatedAt": time.Now().UTC(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/indexes":
			createRequests.Add(1)
			http.Error(w, `{"message":"index already exists","code":"index_already_exists"}`, http.StatusBadRequest)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/indexes/catalog_entries/settings/"):
			taskID := nextTask.Add(1)
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"taskUid": taskID, "indexUid": "catalog_entries", "status": "enqueued",
				"type": "settingsUpdate", "enqueuedAt": time.Now().UTC(),
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tasks/"):
			taskID, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/tasks/"), 10, 64)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uid": taskID, "indexUid": "catalog_entries", "status": "succeeded",
				"type": "settingsUpdate", "enqueuedAt": time.Now().UTC(),
			})
		default:
			http.Error(w, `{"message":"unexpected test request"}`, http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	index, err := NewMeilisearchCatalogIndex(server.URL, "", "catalog_entries")
	if err != nil {
		t.Fatal(err)
	}
	if err := index.ensureIndex(context.Background()); err != nil {
		t.Fatalf("ensure existing Catalog index: %v", err)
	}
	if createRequests.Load() != 0 {
		t.Fatalf("existing index triggered %d create requests", createRequests.Load())
	}
}
