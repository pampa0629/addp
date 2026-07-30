package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
)

func TestFetchDiscoverableAssetsUsesOnlyTenantServiceBearer(t *testing.T) {
	var tokenTenantID uint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/meta/assets/discoverable" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer asset-service-token" {
			t.Fatalf("Authorization=%q", authorization)
		}
		if request.Header.Get("X-Internal-API-Key") != "" || request.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("legacy internal headers were sent: %#v", request.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"source_reference":"17:public.orders","name":"orders","description":""}]`))
	}))
	defer server.Close()

	tokens := commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		tokenTenantID = tenantID
		return "asset-service-token", nil
	})
	assetService := NewAssetService(nil, nil, tokens, nil)
	items, err := assetService.fetchDiscoverableAssets(
		context.Background(), server.URL, "/api/v1/meta/assets/discoverable", 23,
	)
	if err != nil {
		t.Fatalf("fetchDiscoverableAssets() error = %v", err)
	}
	if tokenTenantID != 23 || len(items) != 1 || items[0].SourceReference != "17:public.orders" {
		t.Fatalf("tenant=%d items=%#v", tokenTenantID, items)
	}
}

func TestFetchDiscoverableAssetsRejectsLegacyWrappedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	tokens := commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "asset-service-token", nil
	})
	assetService := NewAssetService(nil, nil, tokens, nil)
	if _, err := assetService.fetchDiscoverableAssets(context.Background(), server.URL, "/discoverable", 23); err == nil {
		t.Fatal("fetchDiscoverableAssets() accepted legacy wrapped response")
	}
}
