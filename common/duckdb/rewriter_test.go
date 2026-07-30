package duckdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

func TestIsObjectTableItemUsesContentAttributes(t *testing.T) {
	t.Parallel()

	item := commonModels.MetaItem{
		ItemType: "object",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    ".parquet",
			},
		},
	}

	if !IsObjectTableItem(item) {
		t.Fatal("object table parquet item should be recognized")
	}

	item.ItemType = "table"
	if IsObjectTableItem(item) {
		t.Fatal("catalog table item should not be recognized as object/file table")
	}
}

func TestBuildObjectTableMapUsesMetaItemList(t *testing.T) {
	t.Parallel()

	var requestedPath string
	var requestedAuthorization string
	var requestedLegacyHeaders bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedAuthorization = r.Header.Get("Authorization")
		requestedLegacyHeaders = r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != ""
		if r.URL.RawQuery != "" {
			t.Fatalf("query = %q, want empty branch query", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]commonModels.MetaItem{{
			ItemType: "object",
			Name:     "sales",
			FullName: "lake/sales",
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "table",
					"format":    "parquet",
				},
				"storage": map[string]interface{}{
					"physical_path": "bucket/lake/sales",
				},
			},
		}})
	}))
	defer server.Close()

	metaClient := commonClient.NewMetaClient(server.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}))
	tables := BuildObjectTableMap(nil, 7, []commonModels.Engine{{
		ID:         3,
		Name:       "lake",
		EngineType: "minio",
	}}, metaClient)

	if requestedPath != "/api/v1/meta/engines/3/items" {
		t.Fatalf("requested path = %q, want item list endpoint", requestedPath)
	}
	if requestedAuthorization != "Bearer test-token" || requestedLegacyHeaders {
		t.Fatalf("auth headers = authorization:%q legacy:%t", requestedAuthorization, requestedLegacyHeaders)
	}
	if tables["lake"]["sales"] != "bucket/lake/sales" || tables["lake"]["lake/sales"] != "bucket/lake/sales" {
		t.Fatalf("tables = %#v", tables)
	}
}
