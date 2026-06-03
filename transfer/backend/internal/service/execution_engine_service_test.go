package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	_ "github.com/addp/common/format/builtin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func TestTargetCatalogPathsUsesExactObjectForSingleObjectTarget(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Format:  "csv",
		Locator: "addp://engine/9/path/addp/gis/abc.csv?type=object",
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"addp/gis/abc.csv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesExactObjectForTopLevelSingleObject(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Format:  "csv",
		Locator: "addp://engine/9/path/addp/abc.csv?type=object",
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"addp/abc.csv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesObjectParentPrefixForMultiObjectTarget(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Format:  "shapefile",
		Locator: "addp://engine/9/path/addp/gis/abc.shp?type=object",
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"addp/gis"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesFileParentDirectory(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Locator: "addp://engine/9/path/exports/abc.shp?type=file",
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"exports"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesFilesystemRootForTopLevelFile(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Locator: "addp://engine/9/path/abc.csv?type=file",
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesNativeTableNamespace(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Locator: "addp://engine/9/path/public/roads?type=table",
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"public"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestRunningProgressCapsAtNinetyNine(t *testing.T) {
	tests := []struct {
		batchIndex int64
		want       float64
	}{
		{batchIndex: 0, want: 0},
		{batchIndex: 1, want: 1},
		{batchIndex: 50, want: 50},
		{batchIndex: 100, want: 99},
	}

	for _, tt := range tests {
		if got := runningProgress(tt.batchIndex); got != tt.want {
			t.Fatalf("runningProgress(%d) = %v, want %v", tt.batchIndex, got, tt.want)
		}
	}
}

func TestAttachSourceMetaAttributesLoadsMetaItem(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/12" {
			t.Fatalf("path = %q, want /api/v1/meta/items/12", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "7" {
			t.Fatalf("X-Tenant-ID = %q, want 7", got)
		}
		if got := r.Header.Get("X-Internal-API-Key"); got != "internal-key" {
			t.Fatalf("X-Internal-API-Key = %q, want internal-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.MetaItem{
			ID:       12,
			EngineID: 3,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"format": "parquet",
				},
			},
		})
	}))
	defer server.Close()

	service := &ExecutionEngineService{
		metaClient: commonClient.NewMetaClientWithInternalKey(server.URL, "internal-key"),
	}
	spec := &planner.TableExportTaskSpec{
		Source: planner.EndpointSpec{
			Locator: "addp://engine/3/path/datasets/roads.parquet?type=object&item_id=12",
		},
	}
	task := &models.TransferTask{
		TenantID: 7,
	}

	if err := service.attachSourceMetaAttributes(task, spec); err != nil {
		t.Fatalf("attachSourceMetaAttributes() error = %v", err)
	}
	itemAttrs, ok := spec.Source.Attributes["item"].(map[string]interface{})
	if !ok || itemAttrs["format"] != "parquet" {
		t.Fatalf("source attributes = %#v, want Meta item attributes", spec.Source.Attributes)
	}
}

func TestAttachSourceMetaAttributesRejectsEngineMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.MetaItem{
			ID:         12,
			EngineID:   4,
			Attributes: map[string]interface{}{"item": map[string]interface{}{"format": "parquet"}},
		})
	}))
	defer server.Close()

	service := &ExecutionEngineService{
		metaClient: commonClient.NewMetaClientWithInternalKey(server.URL, "internal-key"),
	}
	spec := &planner.TableExportTaskSpec{
		Source: planner.EndpointSpec{
			Locator: "addp://engine/3/path/datasets/roads.parquet?type=object&item_id=12",
		},
	}
	task := &models.TransferTask{
		TenantID: 7,
	}

	if err := service.attachSourceMetaAttributes(task, spec); err == nil {
		t.Fatal("attachSourceMetaAttributes() succeeded, want engine mismatch error")
	}
}
