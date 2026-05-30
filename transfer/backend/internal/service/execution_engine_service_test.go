package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func TestTargetCatalogPathsUsesObjectParentPrefix(t *testing.T) {
	endpoint := planner.EndpointSpec{
		EndpointResource: planner.EndpointResourceSpec{
			Kind: planner.EndpointResourceKindObject,
			Path: map[string]interface{}{"bucket": "addp", "path": "gis/abc.shp"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"addp/gis"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesBucketForTopLevelObject(t *testing.T) {
	endpoint := planner.EndpointSpec{
		EndpointResource: planner.EndpointResourceSpec{
			Kind: planner.EndpointResourceKindObject,
			Path: map[string]interface{}{"bucket": "addp", "path": "abc.csv"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"addp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesFileParentDirectory(t *testing.T) {
	endpoint := planner.EndpointSpec{
		EndpointResource: planner.EndpointResourceSpec{
			Kind: planner.EndpointResourceKindFile,
			Path: map[string]interface{}{"path": "exports/abc.shp"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"exports"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesFilesystemRootForTopLevelFile(t *testing.T) {
	endpoint := planner.EndpointSpec{
		EndpointResource: planner.EndpointResourceSpec{
			Kind: planner.EndpointResourceKindFile,
			Path: map[string]interface{}{"path": "abc.csv"},
		},
	}

	got := targetCatalogPaths(endpoint)
	want := []string{"/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestTargetCatalogPathsUsesNativeTableNamespace(t *testing.T) {
	endpoint := planner.EndpointSpec{
		EndpointResource: planner.EndpointResourceSpec{
			Kind: planner.EndpointResourceKindNativeTable,
			Path: map[string]interface{}{"schema": "public", "table": "roads"},
		},
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
			Engine:     planner.EngineRef{ID: 3},
			MetaItemID: 12,
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
			Engine:     planner.EngineRef{ID: 3},
			MetaItemID: 12,
		},
	}
	task := &models.TransferTask{
		TenantID: 7,
	}

	if err := service.attachSourceMetaAttributes(task, spec); err == nil {
		t.Fatal("attachSourceMetaAttributes() succeeded, want engine mismatch error")
	}
}
