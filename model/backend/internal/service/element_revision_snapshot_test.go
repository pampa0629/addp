package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	commonclient "github.com/addp/common/client"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

func TestEntityApprovalFreezesAndReopenClearsElementRevision(t *testing.T) {
	server := newElementRevisionSnapshotServer(t, `{"data":[{"id":41,"tenant_id":1,"code":"customer_id","lifecycle_state":"active","current_revision":{"id":4103,"revision_no":3,"status":"published","name":"Customer ID","data_type":"bigint"}}]}`)
	defer server.Close()

	db := setupLifecycleServiceTestDB(t)
	elementID := int64(41)
	entity := models.Entity{TenantID: 1, Name: "Customer", Code: "customer", Status: "draft", CreatedBy: 1, Version: 1}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatal(err)
	}
	attribute := models.EntityAttribute{EntityID: entity.ID, ElementID: &elementID, Name: "Customer ID", ColumnName: "customer_id", DataType: "bigint", IsPK: true}
	if err := db.Create(&attribute).Error; err != nil {
		t.Fatal(err)
	}

	service := NewEntityService(repository.NewEntityRepository(db), repository.NewEntityRelationRepository(db))
	service.SetStandardClient(newElementRevisionSnapshotClient(server))
	approved, err := service.ApproveEntity(entity.ID, 1, 9, entity.Version)
	if err != nil {
		t.Fatalf("ApproveEntity() error = %v", err)
	}
	if approved.Status != "approved" {
		t.Fatalf("approved entity = %#v", approved)
	}
	attributes, err := service.GetAttributes(entity.ID, 1)
	if err != nil || len(attributes) != 1 || attributes[0].ElementRevisionID == nil || *attributes[0].ElementRevisionID != 4103 {
		t.Fatalf("approved attributes = %#v, err=%v", attributes, err)
	}

	reopened, err := service.ReopenEntity(entity.ID, 1, 9, approved.Version)
	if err != nil {
		t.Fatalf("ReopenEntity() error = %v", err)
	}
	attributes, err = service.GetAttributes(entity.ID, 1)
	if err != nil || reopened.Status != "draft" || attributes[0].ElementRevisionID != nil {
		t.Fatalf("reopened entity = %#v, attributes=%#v, err=%v", reopened, attributes, err)
	}
}

func TestLogicalTableApprovalFreezesAndReopenClearsElementRevision(t *testing.T) {
	server := newElementRevisionSnapshotServer(t, `{"data":[{"id":51,"tenant_id":1,"code":"order_id","lifecycle_state":"active","current_revision":{"id":5102,"revision_no":2,"status":"published","name":"Order ID","data_type":"bigint"}}]}`)
	defer server.Close()

	db := setupLifecycleServiceTestDB(t)
	elementID := int64(51)
	table := models.LogicalTable{TenantID: 1, Name: "Order", Code: "order", TableType: "entity", Layer: "dwd", Status: "draft", Materialization: models.JSONB{}, CreatedBy: 1, Version: 1}
	if err := db.Create(&table).Error; err != nil {
		t.Fatal(err)
	}
	field := models.LogicalField{TableID: table.ID, ElementID: &elementID, Name: "Order ID", ColumnName: "order_id", DataType: "bigint", IsPK: true, FieldRole: "regular"}
	if err := db.Create(&field).Error; err != nil {
		t.Fatal(err)
	}

	service := NewLogicalTableService(repository.NewLogicalTableRepository(db), repository.NewEntityRepository(db), repository.NewDWLayerRepository(db))
	service.SetStandardClient(newElementRevisionSnapshotClient(server))
	approved, err := service.ApproveLogicalTable(table.ID, 1, 9, table.Version)
	if err != nil {
		t.Fatalf("ApproveLogicalTable() error = %v", err)
	}
	fields, err := service.GetFields(table.ID, 1)
	if err != nil || len(fields) != 1 || fields[0].ElementRevisionID == nil || *fields[0].ElementRevisionID != 5102 {
		t.Fatalf("approved fields = %#v, err=%v", fields, err)
	}

	reopened, err := service.ReopenLogicalTable(table.ID, 1, 9, approved.Version)
	if err != nil {
		t.Fatalf("ReopenLogicalTable() error = %v", err)
	}
	fields, err = service.GetFields(table.ID, 1)
	if err != nil || reopened.Status != "draft" || fields[0].ElementRevisionID != nil {
		t.Fatalf("reopened table = %#v, fields=%#v, err=%v", reopened, fields, err)
	}
}

func TestEntityApprovalIsAtomicWhenElementHasNoEffectiveRevision(t *testing.T) {
	server := newElementRevisionSnapshotServer(t, `{"data":[{"id":61,"tenant_id":1,"code":"future_id","lifecycle_state":"active"}]}`)
	defer server.Close()
	db := setupLifecycleServiceTestDB(t)
	elementID := int64(61)
	entity := models.Entity{TenantID: 1, Name: "Future", Code: "future", Status: "draft", CreatedBy: 1, Version: 1}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.EntityAttribute{EntityID: entity.ID, ElementID: &elementID, Name: "ID", ColumnName: "id", DataType: "bigint", IsPK: true}).Error; err != nil {
		t.Fatal(err)
	}
	service := NewEntityService(repository.NewEntityRepository(db), repository.NewEntityRelationRepository(db))
	service.SetStandardClient(newElementRevisionSnapshotClient(server))
	if _, err := service.ApproveEntity(entity.ID, 1, 9, entity.Version); err == nil {
		t.Fatal("ApproveEntity() error = nil")
	}
	reloaded, _ := service.GetEntity(entity.ID, 1)
	attributes, _ := service.GetAttributes(entity.ID, 1)
	if reloaded.Status != "draft" || reloaded.Version != entity.Version || attributes[0].ElementRevisionID != nil {
		t.Fatalf("failed approval changed aggregate: entity=%#v attributes=%#v", reloaded, attributes)
	}
}

func newElementRevisionSnapshotServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements" || r.URL.Query().Get("as_of") == "" {
			t.Fatalf("unexpected Standard request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func newElementRevisionSnapshotClient(server *httptest.Server) *commonclient.StandardClient {
	return commonclient.NewStandardClient(server.URL, commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), server.Client())
}
