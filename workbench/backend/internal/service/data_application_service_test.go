package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/repository"
	"gorm.io/datatypes"
)

type memoryDataApplicationRepository struct {
	views     map[string]models.View
	apps      map[string]models.DataApplication
	revisions map[string][]models.DataApplicationRevision
}

func newMemoryDataApplicationRepository() *memoryDataApplicationRepository {
	return &memoryDataApplicationRepository{
		views: map[string]models.View{}, apps: map[string]models.DataApplication{}, revisions: map[string][]models.DataApplicationRevision{},
	}
}

func (r *memoryDataApplicationRepository) List(tenantID, ownerUserID int64, offset, limit int) ([]models.DataApplication, int64, error) {
	items := make([]models.DataApplication, 0)
	for _, application := range r.apps {
		if application.TenantID == tenantID && application.OwnerUserID == ownerUserID {
			items = append(items, application)
		}
	}
	total := int64(len(items))
	if offset >= len(items) {
		return []models.DataApplication{}, total, nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], total, nil
}

func (r *memoryDataApplicationRepository) Get(tenantID, ownerUserID int64, id string) (*models.DataApplication, error) {
	application, ok := r.apps[id]
	if !ok || application.TenantID != tenantID || application.OwnerUserID != ownerUserID {
		return nil, repository.ErrDataApplicationNotFound
	}
	copy := application
	return &copy, nil
}

func (r *memoryDataApplicationRepository) GetSourceViews(tenantID, ownerUserID int64, ids []string) ([]models.View, error) {
	items := make([]models.View, 0, len(ids))
	for _, id := range ids {
		view, ok := r.views[id]
		if !ok || view.TenantID != tenantID || view.OwnerUserID != ownerUserID {
			return nil, repository.ErrViewNotFound
		}
		items = append(items, view)
	}
	return items, nil
}

func (r *memoryDataApplicationRepository) Create(application *models.DataApplication) error {
	now := time.Now().UTC()
	application.CreatedAt, application.UpdatedAt = now, now
	r.apps[application.ID] = *application
	return nil
}

func (r *memoryDataApplicationRepository) Update(application *models.DataApplication, expectedVersion int64) error {
	current, ok := r.apps[application.ID]
	if !ok || current.TenantID != application.TenantID || current.OwnerUserID != application.OwnerUserID {
		return repository.ErrDataApplicationNotFound
	}
	if current.Version != expectedVersion {
		return repository.ErrDataApplicationVersionConflict
	}
	current.Name, current.Description = application.Name, application.Description
	current.DraftSnapshot = append([]byte(nil), application.DraftSnapshot...)
	current.DraftContentHash = application.DraftContentHash
	current.Version++
	current.UpdatedAt = time.Now().UTC()
	r.apps[current.ID] = current
	return nil
}

func (r *memoryDataApplicationRepository) Publish(tenantID, ownerUserID int64, id string, expectedVersion int64, publishedBy int64) (*models.DataApplicationRevision, error) {
	application, ok := r.apps[id]
	if !ok || application.TenantID != tenantID || application.OwnerUserID != ownerUserID {
		return nil, repository.ErrDataApplicationNotFound
	}
	if application.Version != expectedVersion {
		return nil, repository.ErrDataApplicationVersionConflict
	}
	revisionNumber := int64(len(r.revisions[id]) + 1)
	revision := models.DataApplicationRevision{
		ApplicationID: id, TenantID: tenantID, RevisionNumber: revisionNumber,
		Name: application.Name, Description: application.Description, Snapshot: append([]byte(nil), application.DraftSnapshot...),
		ContentHash: application.DraftContentHash, PublishedBy: publishedBy, PublishedAt: time.Now().UTC(),
	}
	r.revisions[id] = append(r.revisions[id], revision)
	application.PublicationStatus = models.PublicationStatusPublished
	application.CurrentRevisionNumber = &revisionNumber
	application.CurrentRevisionHash = revision.ContentHash
	application.Version++
	r.apps[id] = application
	return &revision, nil
}

func (r *memoryDataApplicationRepository) Offline(tenantID, ownerUserID int64, id string, expectedVersion int64) error {
	application, ok := r.apps[id]
	if !ok || application.TenantID != tenantID || application.OwnerUserID != ownerUserID {
		return repository.ErrDataApplicationNotFound
	}
	if application.Version != expectedVersion {
		return repository.ErrDataApplicationVersionConflict
	}
	if application.CurrentRevisionNumber == nil || application.PublicationStatus != models.PublicationStatusPublished {
		return repository.ErrDataApplicationNotPublished
	}
	application.PublicationStatus = models.PublicationStatusOffline
	application.Version++
	r.apps[id] = application
	return nil
}

func (r *memoryDataApplicationRepository) Delete(tenantID, ownerUserID int64, id string, expectedVersion int64) error {
	application, ok := r.apps[id]
	if !ok || application.TenantID != tenantID || application.OwnerUserID != ownerUserID {
		return repository.ErrDataApplicationNotFound
	}
	if application.Version != expectedVersion {
		return repository.ErrDataApplicationVersionConflict
	}
	if application.CurrentRevisionNumber != nil {
		return repository.ErrDataApplicationAlreadyPublished
	}
	delete(r.apps, id)
	return nil
}

func (r *memoryDataApplicationRepository) GetRuntime(tenantID, ownerUserID int64, id string) (*models.DataApplicationRevision, error) {
	application, ok := r.apps[id]
	if !ok || application.TenantID != tenantID || application.OwnerUserID != ownerUserID || application.PublicationStatus != models.PublicationStatusPublished || application.CurrentRevisionNumber == nil {
		return nil, repository.ErrDataApplicationNotPublished
	}
	revisions := r.revisions[id]
	copy := revisions[len(revisions)-1]
	return &copy, nil
}

func TestDataApplicationServiceOwnsSnapshotAndImmutableRevision(t *testing.T) {
	repository := newMemoryDataApplicationRepository()
	view := dataApplicationSourceView(7, 11)
	repository.views[view.ID] = view
	descriptors := &fakeDescriptorReader{descriptor: testDescriptor(false)}
	applications := NewDataApplicationService(repository, descriptors)

	created, err := applications.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "user-token"}, models.DataApplicationCreateRequest{
		Name: "Order application", Description: "Published order view", SourceViewIDs: []string{view.ID},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Version != 1 || created.PublicationStatus != models.PublicationStatusUnpublished || len(created.Snapshot.Components) != 1 || len(created.Snapshot.Parameters) != 1 {
		t.Fatalf("created application = %#v", created)
	}
	if created.Snapshot.Parameters == nil || created.Snapshot.ParameterBindings == nil || created.Snapshot.Page.Placements == nil {
		t.Fatalf("snapshot collections must use JSON arrays: %#v", created.Snapshot)
	}
	if created.Snapshot.Components[0].Title != "Orders" || created.Snapshot.Components[0].ServiceRef.ServiceID != 23 {
		t.Fatalf("component snapshot = %#v", created.Snapshot.Components[0])
	}

	view.Name = "Changed source view"
	repository.views[view.ID] = view
	delete(repository.views, view.ID)
	loaded, err := applications.Get(7, 11, created.ID)
	if err != nil || loaded.Snapshot.Components[0].Title != "Orders" {
		t.Fatalf("snapshot changed with source View: loaded=%#v err=%v", loaded, err)
	}

	published, err := applications.Publish(context.Background(), 7, 11, created.ID, DescriptorRequest{BearerToken: "user-token"}, 1)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Version != 2 || published.CurrentRevisionNumber == nil || *published.CurrentRevisionNumber != 1 || published.HasUnpublishedChanges {
		t.Fatalf("published application = %#v", published)
	}
	runtimeBeforeEdit, err := applications.Runtime(7, 11, created.ID)
	if err != nil || runtimeBeforeEdit.Name != "Order application" || runtimeBeforeEdit.RevisionNumber != 1 {
		t.Fatalf("Runtime() before edit = %#v, %v", runtimeBeforeEdit, err)
	}

	draft := published.Snapshot
	draft.Page.Title = "Edited draft page"
	updated, err := applications.Update(context.Background(), 7, 11, created.ID, DescriptorRequest{BearerToken: "user-token"}, models.DataApplicationUpdateRequest{
		Name: "Edited draft", Description: published.Description, Snapshot: draft, Version: 2,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 3 || !updated.HasUnpublishedChanges || updated.PublicationStatus != models.PublicationStatusPublished {
		t.Fatalf("updated application = %#v", updated)
	}
	runtimeAfterEdit, err := applications.Runtime(7, 11, created.ID)
	if err != nil || runtimeAfterEdit.Name != "Order application" || runtimeAfterEdit.Snapshot.Page.Title == "Edited draft page" {
		t.Fatalf("published revision changed with draft: runtime=%#v err=%v", runtimeAfterEdit, err)
	}

	if _, err := applications.Update(context.Background(), 7, 11, created.ID, DescriptorRequest{}, models.DataApplicationUpdateRequest{
		Name: "Stale", Snapshot: draft, Version: 2,
	}); !errors.Is(err, ErrDataApplicationVersionConflict) {
		t.Fatalf("stale Update() error = %v", err)
	}
	if err := applications.Delete(7, 11, created.ID, 3); !errors.Is(err, ErrDataApplicationAlreadyPublished) {
		t.Fatalf("published Delete() error = %v", err)
	}
	offline, err := applications.Offline(7, 11, created.ID, 3)
	if err != nil || offline.Version != 4 || offline.PublicationStatus != models.PublicationStatusOffline {
		t.Fatalf("Offline() = %#v, %v", offline, err)
	}
	if _, err := applications.Runtime(7, 11, created.ID); !errors.Is(err, ErrDataApplicationNotPublished) {
		t.Fatalf("offline Runtime() error = %v", err)
	}
	if _, err := applications.Offline(7, 11, created.ID, 4); !errors.Is(err, ErrDataApplicationNotPublished) {
		t.Fatalf("repeated Offline() error = %v", err)
	}
	if _, err := applications.Get(7, 12, created.ID); !errors.Is(err, ErrDataApplicationNotFound) {
		t.Fatalf("cross-owner Get() error = %v", err)
	}
}

func TestDataApplicationServiceUsesEmptyArraysWithoutViewParameters(t *testing.T) {
	repository := newMemoryDataApplicationRepository()
	view := dataApplicationSourceView(7, 11)
	view.ParameterDefinitions = datatypes.JSON(`[]`)
	view.QueryTemplate = datatypes.JSON(`{"select":["id","amount"],"fixed_filter":null,"parameter_filters":[],"order_by":[{"field":"id","direction":"asc"}],"page_limit":50,"format":"json"}`)
	view.DefaultParameterValues = datatypes.JSON(`{}`)
	repository.views[view.ID] = view
	applications := NewDataApplicationService(repository, &fakeDescriptorReader{descriptor: testDescriptor(false)})

	created, err := applications.Create(context.Background(), 7, 11, DescriptorRequest{}, models.DataApplicationCreateRequest{Name: "Orders", SourceViewIDs: []string{view.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Snapshot.Parameters == nil || created.Snapshot.ParameterBindings == nil || len(created.Snapshot.Parameters) != 0 || len(created.Snapshot.ParameterBindings) != 0 {
		t.Fatalf("empty application parameter collections = %#v, %#v", created.Snapshot.Parameters, created.Snapshot.ParameterBindings)
	}
}

func TestDataApplicationServiceRejectsComponentIdentityChanges(t *testing.T) {
	repository := newMemoryDataApplicationRepository()
	view := dataApplicationSourceView(7, 11)
	repository.views[view.ID] = view
	applications := NewDataApplicationService(repository, &fakeDescriptorReader{descriptor: testDescriptor(false)})
	created, err := applications.Create(context.Background(), 7, 11, DescriptorRequest{}, models.DataApplicationCreateRequest{Name: "Orders", SourceViewIDs: []string{view.ID}})
	if err != nil {
		t.Fatal(err)
	}
	changed := created.Snapshot
	changed.Components[0].ServiceRef.ServiceID = 24
	if _, err := applications.Update(context.Background(), 7, 11, created.ID, DescriptorRequest{}, models.DataApplicationUpdateRequest{Name: created.Name, Snapshot: changed, Version: 1}); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("changed ServiceReference Update() error = %v", err)
	}
}

func dataApplicationSourceView(tenantID, ownerUserID int64) models.View {
	input := testViewRequest()
	return models.View{
		ID: "9e95f345-d2c1-4c79-a582-12b65b1550bd", TenantID: tenantID, OwnerUserID: ownerUserID,
		Name: input.Name, Description: input.Description, ServiceType: input.ServiceRef.ServiceType, ServiceID: input.ServiceRef.ServiceID,
		ContractFingerprint: testFingerprint(), ParameterDefinitions: datatypes.JSON(`[{
			"key":"status","label":"Status","control_type":"select","required":false
		}]`),
		QueryTemplate:          datatypes.JSON(`{"select":["id","amount"],"fixed_filter":null,"parameter_filters":[{"parameter_key":"status","field":"status","operator":"eq"}],"order_by":[{"field":"id","direction":"asc"}],"page_limit":50,"format":"json"}`),
		DefaultParameterValues: datatypes.JSON(`{"status":"paid"}`), RendererType: models.RendererTypeTable,
		RendererConfig: datatypes.JSON(`{"columns":["id","amount"]}`), Version: 1,
	}
}
