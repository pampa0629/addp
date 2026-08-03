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

func TestIsObjectTableItemRejectsFormatsWithoutRuntimeReader(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"orc", "avro"} {
		item := commonModels.MetaItem{
			ItemType: "object",
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "table",
					"format":    format,
				},
			},
		}
		if IsObjectTableItem(item) {
			t.Fatalf("%s object must not be advertised as readable through read_parquet", format)
		}
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
			Name:     "sales-data",
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
	if tables["lake"]["sales-data"] != "bucket/lake/sales" ||
		tables["lake"]["sales_data"] != "bucket/lake/sales" ||
		tables["lake"]["lake/sales"] != "bucket/lake/sales" {
		t.Fatalf("tables = %#v", tables)
	}
}

func TestRewriteTwoPartReferenceDoesNotMatchTableNamePrefix(t *testing.T) {
	rewriter := NewSQLRewriter(nil, 0)
	query, err := rewriter.RewriteWithEngines(
		context.Background(),
		"SELECT * FROM Business_MinIO.lake3_parquet",
		map[string]map[string]string{
			"Business_MinIO": {
				"lake3":         "manager/lake3",
				"lake3_parquet": "manager/lake3.parquet",
			},
		},
	)
	if err != nil {
		t.Fatalf("RewriteWithEngines() error = %v", err)
	}
	want := "SELECT * FROM read_parquet('s3://manager/lake3.parquet')"
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}
