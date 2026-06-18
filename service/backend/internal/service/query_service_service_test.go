package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	serviceModels "github.com/addp/service/internal/models"
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

func TestDetectObjectTableUsesMetaItemByID(t *testing.T) {
	t.Parallel()

	var requestedPath string
	var requestedTenant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
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
	config := service.detectObjectTable(7, 33)

	if requestedPath != "/api/v1/meta/items/33" {
		t.Fatalf("requested path = %q, want item endpoint", requestedPath)
	}
	if requestedTenant != "7" {
		t.Fatalf("X-Tenant-ID = %q, want 7", requestedTenant)
	}
	if config["physical_path"] != "bucket/public/sales" || config["format"] != "parquet" {
		t.Fatalf("object table config = %#v", config)
	}
}

func TestTableResourceRefFromRequestDerivesExecutionSnapshot(t *testing.T) {
	t.Parallel()

	engineID := uint(9)
	ref, err := tableResourceRefFromRequest(&serviceModels.CreateQueryServiceRequest{
		ConfigType: "table",
		EngineID:   &engineID,
		DataConfig: map[string]interface{}{
			"locator": "addp://engine/9/path/public/sales?type=table&item_id=33",
		},
	})
	if err != nil {
		t.Fatalf("tableResourceRefFromRequest() error = %v", err)
	}

	if ref.EngineID != 9 || ref.SchemaName != "public" || ref.TableName != "sales" || ref.ItemID != 33 {
		t.Fatalf("table ref = %+v", ref)
	}
}

func TestTableResourceRefFromRequestRejectsLegacyTableIdentity(t *testing.T) {
	t.Parallel()

	_, err := tableResourceRefFromRequest(&serviceModels.CreateQueryServiceRequest{
		ConfigType: "table",
		SchemaName: "public",
		TableName:  "sales",
	})
	if err == nil {
		t.Fatal("tableResourceRefFromRequest() error = nil, want missing locator error")
	}
}
