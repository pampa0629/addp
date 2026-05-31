package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

func TestObjectTableConfigFromMetaItemUsesCommonDuckDBDescriptor(t *testing.T) {
	t.Parallel()

	config := objectTableConfigFromMetaItem(&commonModels.MetaItem{
		ItemType: "object",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    ".parquet",
			},
			"storage": map[string]interface{}{
				"physical_path": "/lake/sales",
			},
		},
	})

	if config["physical_path"] != "lake/sales" || config["layout"] != "single" || config["format"] != "parquet" {
		t.Fatalf("object table config = %#v", config)
	}
}

func TestObjectTableConfigFromMetaItemRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()

	config := objectTableConfigFromMetaItem(&commonModels.MetaItem{
		ItemType: "object",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"format":    "csv",
				"layout":    "single",
			},
			"storage": map[string]interface{}{
				"physical_path": "/lake/sales.csv",
			},
		},
	})

	if len(config) != 0 {
		t.Fatalf("object table config = %#v, want empty", config)
	}
}

func TestDetectObjectTableUsesMetaItemByCatalogPath(t *testing.T) {
	t.Parallel()

	var requestedPath string
	var requestedCatalogPath string
	var requestedTenant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedCatalogPath = r.URL.Query().Get("catalog_path")
		requestedTenant = r.Header.Get("X-Tenant-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.MetaItem{
			ItemType: "object",
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "table",
					"format":    "parquet",
				},
				"storage": map[string]interface{}{
					"physical_path": "bucket/public/sales",
				},
			},
		})
	}))
	defer server.Close()

	service := &QueryServiceService{
		metaClient: client.NewMetaClientWithInternalKey(server.URL, "internal-key"),
	}
	config := service.detectObjectTable(7, 3, "public", "sales")

	if requestedPath != "/api/v1/meta/items/by-catalog-path" {
		t.Fatalf("requested path = %q, want by-catalog-path item endpoint", requestedPath)
	}
	if requestedCatalogPath != "public.sales" {
		t.Fatalf("catalog_path = %q, want public.sales", requestedCatalogPath)
	}
	if requestedTenant != "7" {
		t.Fatalf("X-Tenant-ID = %q, want 7", requestedTenant)
	}
	if config["physical_path"] != "bucket/public/sales" || config["format"] != "parquet" {
		t.Fatalf("object table config = %#v", config)
	}
}
