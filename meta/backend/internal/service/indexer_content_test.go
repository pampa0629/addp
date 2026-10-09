package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
)

func TestIndexerServiceSubmitsTableProjectionToManagerOwner(t *testing.T) {
	var received commonClient.ManagerContentDocument
	manager := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/api/v1/manager/runtime/content-documents/fingerprint-1" || request.Header.Get("Authorization") != "Bearer meta-tenant-7" {
			t.Fatalf("request = %s %s authorization=%q", request.Method, request.URL.String(), request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer manager.Close()
	client := commonClient.NewManagerContentClient(manager.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "meta-tenant-7", nil
	}), manager.Client())
	indexer := NewIndexerService(client, nil)
	rowCount := int64(12)
	indexer.IndexTechnicalMetadata(context.Background(), &commonModels.Engine{ID: 9, Name: "Warehouse", EngineType: "postgresql"}, 7, &models.MetaNode{NodeType: "schema", Name: "public", FullName: "public"}, &models.MetaItem{
		Fingerprint: "fingerprint-1", ItemType: "table", Name: "orders", FullName: "public.orders", RowCount: &rowCount,
		Attributes: models.JSONMap{"type_info": map[string]interface{}{"table": datatype.TableInfoPayload(&datatype.TableInfo{
			Name: "orders", Comment: "Orders", Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}},
		})}},
	})
	if received.DocumentID != "fingerprint-1" || received.DataItemType != "table" || received.EngineID != 9 || received.Schema != "public" || len(received.Fields) != 1 {
		t.Fatalf("received document = %#v", received)
	}
	if received.PayloadKind != commonClient.ManagerContentPayloadTechnicalMetadata || len(received.Metadata) != 0 {
		t.Fatalf("received table payload kind=%q metadata=%#v", received.PayloadKind, received.Metadata)
	}
}

func TestIndexerServiceCollectionProjectionExcludesSampleValues(t *testing.T) {
	var received commonClient.ManagerContentDocument
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/manager/runtime/content-documents/collection-fingerprint" || r.Header.Get("Authorization") != "Bearer tenant-7" {
			t.Errorf("unexpected projection request: %s %s", r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer manager.Close()
	client := commonClient.NewManagerContentClient(manager.URL, commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
		if tenant != 7 {
			t.Errorf("tenant=%d, want 7", tenant)
		}
		return "tenant-7", nil
	}), manager.Client())
	item := &models.MetaItem{ID: 111, Fingerprint: "collection-fingerprint", ItemType: "collection", Name: "Outdoors", FullName: "Outdoor.Outdoors", Attributes: models.JSONMap{
		"type_info": map[string]interface{}{"table": map[string]interface{}{"fields": []interface{}{
			map[string]interface{}{"name": "leader", "type": "json", "path": []string{"leader"}, "samples": []string{"private-value"}},
			map[string]interface{}{"name": "leader.nickName", "type": "string", "path": []string{"leader", "nickName"}, "top_values": []string{"private-value"}},
		}, "native": map[string]interface{}{"samples": []string{"private-value"}}}},
		"capabilities": map[string]interface{}{"statistics": map[string]interface{}{"samples": []string{"private-value"}}},
		"plain_text":   "private-value",
	}}
	resource := &commonModels.Engine{ID: 11, EngineType: "mongodb", Name: "MongoDB"}
	indexer := NewIndexerService(client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !indexer.IndexTechnicalMetadata(t.Context(), resource, 7, &models.MetaNode{NodeType: "database", Name: "Outdoor", FullName: "Outdoor"}, item) {
		t.Fatal("projection failed")
	}
	if received.PayloadKind != commonClient.ManagerContentPayloadTechnicalMetadata || received.DataItemType != "collection" || received.TableKind != "collection" || received.Schema != "Outdoor" || len(received.Fields) != 2 || received.Fields[1].Name != "leader.nickName" || received.Fields[1].DataType != "string" || received.Validate() != nil {
		t.Fatalf("collection projection=%+v", received)
	}
	loc, err := resourcetree.ParseURI(received.Locator)
	if err != nil || loc.Type != "collection" || !reflect.DeepEqual(loc.Path, []string{"Outdoor", "Outdoors"}) || loc.ItemID == nil || *loc.ItemID != 111 {
		t.Fatalf("locator=%s error=%v", received.Locator, err)
	}
	encoded, err := json.Marshal(received)
	if err != nil || strings.Contains(string(encoded), "private-value") || len(received.Metadata) != 0 {
		t.Fatalf("sample values leaked into projection: %s %v", encoded, err)
	}
	manager.Close()
	if indexer.IndexTechnicalMetadata(t.Context(), resource, 7, &models.MetaNode{NodeType: "database", Name: "Outdoor", FullName: "Outdoor"}, item) {
		t.Fatal("unavailable Manager reported a successful projection")
	}
	if NewIndexerService(nil, nil).IndexTechnicalMetadata(t.Context(), resource, 7, &models.MetaNode{NodeType: "database", Name: "Outdoor", FullName: "Outdoor"}, item) {
		t.Fatal("unconfigured Manager reported a successful projection")
	}
}

func TestCatalogContentKeepsSnapshotsSeparate(t *testing.T) {
	var documents []commonClient.ManagerContentDocument
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var doc commonClient.ManagerContentDocument
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			t.Error(err)
		}
		documents = append(documents, doc)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer manager.Close()
	client := commonClient.NewManagerContentClient(manager.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "tenant-7", nil }), manager.Client())
	indexer := NewIndexerService(client, nil)
	resource := &commonModels.Engine{ID: 11, EngineType: "minio", Name: "Objects"}
	item := &models.MetaItem{ID: 42, Fingerprint: "object-fingerprint", ItemType: "object", Name: "roads.csv", FullName: "datasets/tables/roads.csv", Attributes: models.JSONMap{
		"storage":   map[string]interface{}{"bucket": "datasets", "path": "tables/", "content_type": "text/csv"},
		"item":      map[string]interface{}{"format": "csv"},
		"type_info": map[string]interface{}{"table": map[string]interface{}{"fields": []interface{}{map[string]interface{}{"name": "road_id", "type": "string", "samples": []string{"private-value"}}}}},
	}}
	if !indexer.IndexTechnicalMetadata(t.Context(), resource, 7, nil, item) || !indexer.IndexCatalogContent(t.Context(), resource, 7, item, "protected-route-text", false) {
		t.Fatal("projection failed")
	}
	if len(documents) != 2 {
		t.Fatalf("documents=%d", len(documents))
	}
	for _, doc := range documents[:1] {
		loc, err := resourcetree.ParseURI(doc.Locator)
		if err != nil || !reflect.DeepEqual(loc.Path, []string{"datasets", "tables", "roads.csv"}) || doc.Bucket != "datasets" || doc.Path != "tables/" || doc.FullName != item.FullName || len(doc.Fields) != 1 || doc.Fields[0].Name != "road_id" {
			t.Fatalf("registered technical facts lost: %+v error=%v", doc, err)
		}
	}
	if documents[1].Validate() != nil || len(documents[1].Fields) != 0 || documents[1].Locator != "" || documents[1].Metadata != nil || documents[0].Content != "" || documents[0].Metadata != nil || documents[1].Content != "protected-route-text" || documents[1].PayloadKind != commonClient.ManagerContentPayloadExtractedContent {
		t.Fatal("technical and extracted payload boundaries changed")
	}
}

func TestDeletedItemProjectionUsesOnlyItsFingerprint(t *testing.T) {
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Query().Get("document_id") != "one-table" || r.URL.Query().Get("engine_id") != "9" || r.URL.Query().Get("schema") != "" {
			t.Fatalf("broad deletion scope: %s", r.URL)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer manager.Close()
	client := commonClient.NewManagerContentClient(manager.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "tenant-7", nil }), manager.Client())
	NewIndexerService(client, nil).DeleteItemFromIndex(7, 9, "one-table")
}
