package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetClientPreservesDownstreamStatusWithoutResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "database password leaked", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewAssetClient(server.URL)
	_, err := client.GetAssetDetail(context.Background(), "user-access-token", 12)
	if err == nil {
		t.Fatal("GetAssetDetail() error = nil")
	}
	var apiError *AssetAPIError
	if !errors.As(err, &apiError) {
		t.Fatalf("GetAssetDetail() error type = %T, want *AssetAPIError", err)
	}
	if apiError.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", apiError.StatusCode, http.StatusServiceUnavailable)
	}
	if strings.Contains(err.Error(), "database password") {
		t.Fatalf("error leaked downstream body: %v", err)
	}
}

func TestAssetClientUsesRequestScopedUserBearerWithoutIdentityFields(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if got := r.Header.Get("Authorization"); got != "Bearer user-access-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("legacy headers were sent: %#v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		for _, key := range []string{"tenant_id", "applicant_id", "user_id", "asset_id"} {
			if _, exists := body[key]; exists {
				t.Errorf("request body contains caller-controlled identity field %q", key)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/applications") {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1,"asset_id":12,"applicant_id":9}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":2,"asset_id":12,"user_id":9,"score":5}`))
	}))
	defer server.Close()

	client := NewAssetClient(server.URL)
	if _, err := client.CreateApplication(context.Background(), "user-access-token", 12, CreateApplicationRequest{
		Reason: "research", DurationDay: 30,
	}); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := client.UpsertRating(context.Background(), "user-access-token", 12, UpsertRatingRequest{
		Score: 5, Comment: "useful",
	}); err != nil {
		t.Fatalf("UpsertRating: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
}

func TestAssetClientCallsOnlyConsumerProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/asset/consumer/") {
			t.Errorf("path = %q, want consumer projection", r.URL.Path)
		}
		if r.URL.Query().Get("status") != "" {
			t.Errorf("caller supplied status filter: %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	}))
	defer server.Close()

	client := NewAssetClient(server.URL)
	if _, err := client.GetAssets(context.Background(), "user-access-token", AssetQueryOptions{Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("GetAssets: %v", err)
	}
}
