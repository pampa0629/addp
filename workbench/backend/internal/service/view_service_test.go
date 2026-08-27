package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/repository"
)

type fakeDescriptorReader struct {
	descriptor *models.ConsumerDescriptor
	err        error
	requests   []DescriptorRequest
}

func (f *fakeDescriptorReader) GetDescriptor(_ context.Context, request DescriptorRequest) (*models.ConsumerDescriptor, error) {
	f.requests = append(f.requests, request)
	return f.descriptor, f.err
}

type memoryViewRepository struct{ views map[string]models.View }

func newMemoryViewRepository() *memoryViewRepository {
	return &memoryViewRepository{views: map[string]models.View{}}
}
func (r *memoryViewRepository) List(tenantID, ownerUserID int64, offset, limit int) ([]models.View, int64, error) {
	items := make([]models.View, 0)
	for _, view := range r.views {
		if view.TenantID == tenantID && view.OwnerUserID == ownerUserID {
			items = append(items, view)
		}
	}
	total := int64(len(items))
	if offset >= len(items) {
		return []models.View{}, total, nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], total, nil
}
func (r *memoryViewRepository) Get(tenantID, ownerUserID int64, id string) (*models.View, error) {
	view, ok := r.views[id]
	if !ok || view.TenantID != tenantID || view.OwnerUserID != ownerUserID {
		return nil, repository.ErrViewNotFound
	}
	copy := view
	return &copy, nil
}
func (r *memoryViewRepository) Create(view *models.View) error {
	now := time.Now().UTC()
	view.CreatedAt, view.UpdatedAt = now, now
	r.views[view.ID] = *view
	return nil
}
func (r *memoryViewRepository) Update(view *models.View, expectedVersion int64) error {
	current, ok := r.views[view.ID]
	if !ok || current.TenantID != view.TenantID || current.OwnerUserID != view.OwnerUserID {
		return repository.ErrViewNotFound
	}
	if current.Version != expectedVersion {
		return repository.ErrViewVersionConflict
	}
	view.Version = expectedVersion + 1
	view.CreatedAt = current.CreatedAt
	view.UpdatedAt = time.Now().UTC()
	r.views[view.ID] = *view
	return nil
}
func (r *memoryViewRepository) Delete(tenantID, ownerUserID int64, id string) error {
	view, ok := r.views[id]
	if !ok || view.TenantID != tenantID || view.OwnerUserID != ownerUserID {
		return repository.ErrViewNotFound
	}
	delete(r.views, id)
	return nil
}

func TestViewServiceEnforcesOwnerAndDescriptorValidation(t *testing.T) {
	repository := newMemoryViewRepository()
	descriptors := &fakeDescriptorReader{descriptor: testDescriptor(false)}
	views := NewViewService(repository, descriptors)
	input := testViewRequest()

	created, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "user-token"}, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.TenantID != 7 || created.OwnerUserID != 11 || created.Version != 1 || created.ServiceRef.ServiceID != 23 || created.ContractFingerprint != testFingerprint() {
		t.Fatalf("created view = %#v", created)
	}
	if len(descriptors.requests) != 1 || descriptors.requests[0].BearerToken != "user-token" || descriptors.requests[0].Ref.ServiceID != 23 {
		t.Fatalf("descriptor requests = %#v", descriptors.requests)
	}
	if _, err := views.Get(7, 12, created.ID); !errors.Is(err, ErrViewNotFound) {
		t.Fatalf("other owner Get() error = %v", err)
	}
	if _, _, err := views.List(7, 11, 1, 20); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(descriptors.requests) != 1 {
		t.Fatalf("read-only operations called Descriptor %d times", len(descriptors.requests))
	}

	input.Version = int64Pointer(1)
	input.Name = "Updated"
	updated, err := views.Update(context.Background(), 7, 11, created.ID, DescriptorRequest{BearerToken: "user-token"}, input)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 2 || updated.Name != "Updated" {
		t.Fatalf("updated view = %#v", updated)
	}
	input.Version = int64Pointer(1)
	if _, err := views.Update(context.Background(), 7, 11, created.ID, DescriptorRequest{BearerToken: "user-token"}, input); !errors.Is(err, ErrViewVersionConflict) {
		t.Fatalf("stale Update() error = %v", err)
	}
	if err := views.Delete(7, 12, created.ID); !errors.Is(err, ErrViewNotFound) {
		t.Fatalf("other owner Delete() error = %v", err)
	}
	if err := views.Delete(7, 11, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestViewServiceRejectsServiceRefChangesAndInvalidRenderers(t *testing.T) {
	repository := newMemoryViewRepository()
	descriptors := &fakeDescriptorReader{descriptor: testDescriptor(false)}
	views := NewViewService(repository, descriptors)
	input := testViewRequest()
	created, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "token"}, input)
	if err != nil {
		t.Fatal(err)
	}

	input.Version = int64Pointer(1)
	input.ServiceRef = &models.ServiceReference{ServiceType: "query", ServiceID: 24}
	if _, err := views.Update(context.Background(), 7, 11, created.ID, DescriptorRequest{BearerToken: "token"}, input); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("changed ref Update() error = %v", err)
	}

	invalid := testViewRequest()
	invalid.RendererConfig = json.RawMessage(`{"columns":["id"],"unknown":true}`)
	if _, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "token"}, invalid); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("unknown renderer field Create() error = %v", err)
	}
	mapInput := testViewRequest()
	mapInput.RendererType = models.RendererTypeMap
	mapInput.RendererConfig = json.RawMessage(`{"geometry_field":"shape","tooltip_fields":["id"]}`)
	if _, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "token"}, mapInput); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("non-spatial map Create() error = %v", err)
	}

	chartInput := testViewRequest()
	chartInput.RendererType = models.RendererTypeChart
	chartInput.RendererConfig = json.RawMessage(`{"chart_type":"bar","dimension":"id","measures":["status"]}`)
	if _, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "token"}, chartInput); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("non-numeric chart measure Create() error = %v", err)
	}
	validChart := testViewRequest()
	validChart.RendererType = models.RendererTypeChart
	validChart.RendererConfig = json.RawMessage(`{"chart_type":"bar","dimension":"id","measures":["amount"]}`)
	if _, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "token"}, validChart); err != nil {
		t.Fatalf("canonical decimal chart Create() error = %v", err)
	}

	invalidDefault := testViewRequest()
	invalidDefault.DefaultParameterValues["status"] = json.RawMessage(`42`)
	if _, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "token"}, invalidDefault); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("invalid default parameter Create() error = %v", err)
	}

	invalidOperationDescriptor := testDescriptor(false)
	invalidOperationDescriptor.Operations[0].Path = "https://example.invalid/collect"
	descriptors.descriptor = invalidOperationDescriptor
	if _, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "token"}, testViewRequest()); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("external operation Create() error = %v", err)
	}

	descriptors.descriptor = testDescriptor(true)
	validMap := testViewRequest()
	validMap.QueryTemplate.Select = append(validMap.QueryTemplate.Select, "shape")
	validMap.RendererType = models.RendererTypeMap
	validMap.RendererConfig = json.RawMessage(`{"geometry_field":"shape","tooltip_fields":["id"]}`)
	if _, err := views.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "token"}, validMap); err != nil {
		t.Fatalf("spatial map Create() error = %v", err)
	}
}

func testViewRequest() models.ViewWriteRequest {
	return models.ViewWriteRequest{
		Name: "Orders", Description: "Order list", ServiceRef: &models.ServiceReference{ServiceType: "query", ServiceID: 23},
		ParameterDefinitions: []models.ViewParameterDefinition{{Key: "status", Label: "Status", ControlType: "select"}},
		QueryTemplate: models.ViewQueryTemplate{
			Select: []string{"id", "amount"}, ParameterFilters: []models.ViewParameterFilter{{ParameterKey: "status", Field: "status", Operator: "eq"}},
			OrderBy: []models.QueryOrder{{Field: "id", Direction: "asc"}}, PageLimit: 50, Format: "json",
		},
		DefaultParameterValues: map[string]json.RawMessage{"status": json.RawMessage(`"paid"`)},
		RendererType:           models.RendererTypeTable, RendererConfig: json.RawMessage(`{"columns":["id","amount"]}`),
	}
}

func testDescriptor(spatial bool) *models.ConsumerDescriptor {
	descriptor := &models.ConsumerDescriptor{
		SchemaVersion: models.ConsumerDescriptorSchemaVersion,
		Ref:           models.ServiceReference{ServiceType: "query", ServiceID: 23}, Status: "active", ContractFingerprint: testFingerprint(),
		Operations: []models.ConsumerOperation{{
			Key: "query", Method: "POST", Path: "/api/query/orders/query",
			InputKind: "structured_query", OutputKind: "tabular",
		}},
		InputContract: models.StructuredQueryInputContract{
			Kind: "structured_query",
			Fields: []models.ConsumerQueryField{
				{Name: "id", Type: datatype.FieldTypeString, Selectable: true, Sortable: true},
				{Name: "amount", Type: datatype.FieldTypeDecimal, Selectable: true},
				{Name: "status", Type: datatype.FieldTypeString, Selectable: true, Filterable: true, Operators: []string{"eq", "in"}},
			},
			Filter: models.ConsumerFilterContract{Combinators: []string{"and", "or", "not"}, MaxDepth: 16, MaxNodes: 256, MaxInValues: 1000},
			Order:  models.ConsumerOrderContract{Directions: []string{"asc", "desc"}, StableKey: []string{"id"}},
			Page:   models.ConsumerPageContract{Kind: "cursor", DefaultLimit: 50, MaxLimit: 1000}, Formats: []string{"json", "csv"},
		},
		OutputContract: models.TabularOutputContract{Kind: "tabular", Fields: []models.ConsumerOutputField{
			{Name: "id", Type: datatype.FieldTypeString}, {Name: "amount", Type: datatype.FieldTypeDecimal}, {Name: "status", Type: datatype.FieldTypeString},
		}},
	}
	if spatial {
		descriptor.InputContract.Fields = append(descriptor.InputContract.Fields, models.ConsumerQueryField{Name: "shape", Type: datatype.FieldTypeGeometry, Selectable: true})
		descriptor.InputContract.Formats = append(descriptor.InputContract.Formats, "geojson")
		descriptor.OutputContract.Fields = append(descriptor.OutputContract.Fields, models.ConsumerOutputField{Name: "shape", Type: datatype.FieldTypeGeometry})
		descriptor.OutputContract.Spatial = &models.ConsumerSpatialContract{PrimaryGeometryField: "shape", GeometryFields: []models.ConsumerGeometryField{{Name: "shape"}}}
		descriptor.OutputContract.Kind = "spatial_tabular"
		descriptor.Operations[0].OutputKind = "spatial_tabular"
	}
	return descriptor
}

func testFingerprint() string {
	return "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
func int64Pointer(value int64) *int64 { return &value }
