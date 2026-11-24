package preview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/rustfs"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
)

func TestIntegrationRustFSPreview(t *testing.T) {
	if os.Getenv("ADDP_RUSTFS_INTEGRATION") != "1" {
		t.Skip("owned RustFS T2 required")
	}
	p := &rustfs.RustFSPlugin{}
	plugin.Register(p)
	item, err := os.ReadFile(os.Getenv("ADDP_RUSTFS_TEST_META_ITEM"))
	if err != nil {
		t.Fatal(err)
	}
	bucket := os.Getenv("ADDP_RUSTFS_TEST_BUCKET")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("engine_id") != "91" || r.URL.Query().Get("catalog_path") != bucket+"/样例/订单.csv" {
			t.Errorf("unexpected metadata request: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/api/v1/meta/items/by-catalog-path" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(item)
	}))
	defer server.Close()
	meta := client.NewMetaClient(server.URL, client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "rustfs-t2-metadata-token", nil }))
	registry := objectcontent.NewObjectContentRegistry()
	objectcontent.LoadObjectContentPlugins(registry, "../../plugins")
	provider := NewObjectCatalogPreviewProvider(&repository.MetadataRepository{}, meta, registry)
	result, err := provider.Preview(t.Context(), &PreviewRequest{
		Engine: &models.Engine{ID: 91, EngineType: p.Type(), ConnectionInfo: models.ConnectionInfo{
			"endpoint": os.Getenv("ADDP_RUSTFS_TEST_ENDPOINT"), "access_key": os.Getenv("ADDP_RUSTFS_TEST_ACCESS_KEY"), "secret_key": os.Getenv("ADDP_RUSTFS_TEST_SECRET_KEY"), "use_ssl": false,
		}}, Schema: bucket, Table: "样例/订单.csv", TenantID: uintPtr(1), Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Object == nil || result.Object.Content == nil {
		t.Fatalf("missing CSV preview: %#v", result)
	}
	if result.Object.Content.Kind != "table" {
		t.Fatalf("unexpected preview kind: %#v", result.Object.Content)
	}
	content, ok := result.Object.Content.JSON.(map[string]interface{})
	if !ok {
		t.Fatalf("missing table content: %#v", result.Object.Content.JSON)
	}
	rows, ok := content["rows"].([]map[string]interface{})
	if !ok || len(rows) != 2 || rows[0]["city"] != "长沙" || rows[1]["city"] != "株洲" {
		t.Fatalf("CSV rows differ: %#v", content["rows"])
	}
}
