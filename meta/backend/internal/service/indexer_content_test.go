package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
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
	indexer.IndexTableContent(context.Background(), &commonModels.Engine{ID: 9, Name: "Warehouse", EngineType: "postgresql"}, 7, "public", datatype.TableInfo{
		Name: "orders", Comment: "Orders", Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}},
	}, []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}}, &models.MetaItem{
		Fingerprint: "fingerprint-1", ItemType: "table", Name: "orders", FullName: "public.orders", RowCount: &rowCount,
	})
	if received.DocumentID != "fingerprint-1" || received.DataItemType != "table" || received.EngineID != 9 || received.Schema != "public" || len(received.Fields) != 1 {
		t.Fatalf("received document = %#v", received)
	}
	if received.PayloadKind != commonClient.ManagerContentPayloadTechnicalMetadata || len(received.Metadata) != 0 {
		t.Fatalf("received table payload kind=%q metadata=%#v", received.PayloadKind, received.Metadata)
	}
}
