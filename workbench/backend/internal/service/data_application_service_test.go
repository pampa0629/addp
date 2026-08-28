package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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

type staticDataApplicationAccessRules struct{ allowed bool }

func (rules staticDataApplicationAccessRules) CanExecuteDataApplication(_, _ int64, _ string, _ time.Time) (bool, error) {
	return rules.allowed, nil
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

func (r *memoryDataApplicationRepository) GetRuntimeApplication(tenantID int64, id string) (*models.DataApplication, *models.DataApplicationRevision, error) {
	application, ok := r.apps[id]
	if !ok || application.TenantID != tenantID {
		return nil, nil, repository.ErrDataApplicationNotFound
	}
	if application.PublicationStatus != models.PublicationStatusPublished || application.CurrentRevisionNumber == nil {
		return nil, nil, repository.ErrDataApplicationNotPublished
	}
	revisions := r.revisions[id]
	copyApplication := application
	copyRevision := revisions[len(revisions)-1]
	return &copyApplication, &copyRevision, nil
}

func TestDataApplicationServiceOwnsSnapshotAndImmutableRevision(t *testing.T) {
	repository := newMemoryDataApplicationRepository()
	view := dataApplicationSourceView(7, 11)
	repository.views[view.ID] = view
	descriptors := &fakeDescriptorReader{descriptor: testDescriptor(false)}
	applications := NewDataApplicationService(repository, descriptors, nil)

	created, err := applications.Create(context.Background(), 7, 11, DescriptorRequest{BearerToken: "user-token"}, models.DataApplicationCreateRequest{
		Name: "Order application", Description: "Published order view", SourceViewIDs: []string{view.ID},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Version != 1 || created.PublicationStatus != models.PublicationStatusUnpublished || created.Snapshot.Page.DisplayMode != models.ApplicationDisplayModeDesktop || created.Snapshot.Page.RefreshIntervalSeconds == nil || *created.Snapshot.Page.RefreshIntervalSeconds != models.ApplicationRefreshIntervalDisabled || len(created.Snapshot.Page.VisibleSections) != 3 || len(created.Snapshot.Components) != 1 || len(created.Snapshot.Parameters) != 1 {
		t.Fatalf("created application = %#v", created)
	}
	if created.Snapshot.Parameters == nil || created.Snapshot.ParameterBindings == nil || created.Snapshot.SelectionBindings == nil || created.Snapshot.Page.VisibleSections == nil || created.Snapshot.Page.Placements == nil {
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
	grantedApplications := NewDataApplicationService(repository, descriptors, staticDataApplicationAccessRules{allowed: true})
	if grantedRuntime, err := grantedApplications.Runtime(7, 91, created.ID); err != nil || grantedRuntime.ID != created.ID {
		t.Fatalf("granted Runtime() = %#v, %v", grantedRuntime, err)
	}
	deniedApplications := NewDataApplicationService(repository, descriptors, staticDataApplicationAccessRules{})
	if _, err := deniedApplications.Runtime(7, 92, created.ID); !errors.Is(err, ErrDataApplicationAccessDenied) {
		t.Fatalf("denied Runtime() error = %v", err)
	}

	draft := published.Snapshot
	draft.Page.Title = "Edited draft page"
	draft.Page.DisplayMode = models.ApplicationDisplayModeWallboard
	draft.Page.RefreshIntervalSeconds = applicationRefreshInterval(models.ApplicationRefreshInterval30Seconds)
	draft.Page.VisibleSections = []string{models.ApplicationVisibleSectionTitle}
	updated, err := applications.Update(context.Background(), 7, 11, created.ID, DescriptorRequest{BearerToken: "user-token"}, models.DataApplicationUpdateRequest{
		Name: "Edited draft", Description: published.Description, Snapshot: draft, Version: 2,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 3 || !updated.HasUnpublishedChanges || updated.PublicationStatus != models.PublicationStatusPublished || len(updated.Snapshot.Page.VisibleSections) != 1 || updated.Snapshot.Page.VisibleSections[0] != models.ApplicationVisibleSectionTitle {
		t.Fatalf("updated application = %#v", updated)
	}
	runtimeAfterEdit, err := applications.Runtime(7, 11, created.ID)
	if err != nil || runtimeAfterEdit.Name != "Order application" || runtimeAfterEdit.Snapshot.Page.Title == "Edited draft page" || runtimeAfterEdit.Snapshot.Page.DisplayMode != models.ApplicationDisplayModeDesktop || runtimeAfterEdit.Snapshot.Page.RefreshIntervalSeconds == nil || *runtimeAfterEdit.Snapshot.Page.RefreshIntervalSeconds != models.ApplicationRefreshIntervalDisabled || len(runtimeAfterEdit.Snapshot.Page.VisibleSections) != 3 {
		t.Fatalf("published revision changed with draft: runtime=%#v err=%v", runtimeAfterEdit, err)
	}

	invalidDisplayMode := cloneDataApplicationSnapshot(t, draft)
	invalidDisplayMode.Page.DisplayMode = "mobile"
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, invalidDisplayMode); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("invalid display mode error = %v", err)
	}

	invalidRefreshInterval := cloneDataApplicationSnapshot(t, draft)
	invalidRefreshInterval.Page.RefreshIntervalSeconds = applicationRefreshInterval(10)
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, invalidRefreshInterval); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("invalid refresh interval error = %v", err)
	}

	desktopRefresh := cloneDataApplicationSnapshot(t, draft)
	desktopRefresh.Page.DisplayMode = models.ApplicationDisplayModeDesktop
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, desktopRefresh); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("desktop refresh policy error = %v", err)
	}

	missingRefreshPolicy := cloneDataApplicationSnapshot(t, draft)
	missingRefreshPolicy.Page.RefreshIntervalSeconds = nil
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, missingRefreshPolicy); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("missing refresh policy error = %v", err)
	}

	missingVisibleSections := cloneDataApplicationSnapshot(t, draft)
	missingVisibleSections.Page.VisibleSections = nil
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, missingVisibleSections); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("missing visible sections error = %v", err)
	}

	invalidVisibleSections := cloneDataApplicationSnapshot(t, draft)
	invalidVisibleSections.Page.VisibleSections = []string{"legend"}
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, invalidVisibleSections); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("invalid visible sections error = %v", err)
	}

	duplicateVisibleSections := cloneDataApplicationSnapshot(t, draft)
	duplicateVisibleSections.Page.VisibleSections = []string{models.ApplicationVisibleSectionTitle, models.ApplicationVisibleSectionTitle}
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, duplicateVisibleSections); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("duplicate visible sections error = %v", err)
	}

	desktopHiddenSection := cloneDataApplicationSnapshot(t, created.Snapshot)
	desktopHiddenSection.Page.VisibleSections = []string{models.ApplicationVisibleSectionTitle}
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, desktopHiddenSection); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("desktop hidden section error = %v", err)
	}

	hiddenQueryActionsWithoutRefresh := cloneDataApplicationSnapshot(t, draft)
	hiddenQueryActionsWithoutRefresh.Page.RefreshIntervalSeconds = applicationRefreshInterval(models.ApplicationRefreshIntervalDisabled)
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, hiddenQueryActionsWithoutRefresh); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("hidden query actions without refresh error = %v", err)
	}

	requiredDefault := cloneDataApplicationSnapshot(t, draft)
	requiredDefault.Parameters[0].Required = true
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, requiredDefault); err != nil {
		t.Fatalf("hidden parameters with required default error = %v", err)
	}
	requiredDefault.Parameters[0].DefaultValue = nil
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, requiredDefault); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("hidden parameters without required default error = %v", err)
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
	applications := NewDataApplicationService(repository, &fakeDescriptorReader{descriptor: testDescriptor(false)}, nil)

	created, err := applications.Create(context.Background(), 7, 11, DescriptorRequest{}, models.DataApplicationCreateRequest{Name: "Orders", SourceViewIDs: []string{view.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Snapshot.Parameters == nil || created.Snapshot.ParameterBindings == nil || created.Snapshot.SelectionBindings == nil || len(created.Snapshot.Parameters) != 0 || len(created.Snapshot.ParameterBindings) != 0 || len(created.Snapshot.SelectionBindings) != 0 {
		t.Fatalf("empty application binding collections = %#v, %#v, %#v", created.Snapshot.Parameters, created.Snapshot.ParameterBindings, created.Snapshot.SelectionBindings)
	}
}

func TestDataApplicationServiceValidatesSelectionBindings(t *testing.T) {
	repository := newMemoryDataApplicationRepository()
	view := dataApplicationSourceView(7, 11)
	repository.views[view.ID] = view
	descriptor := testDescriptor(false)
	applications := NewDataApplicationService(repository, &fakeDescriptorReader{descriptor: descriptor}, nil)
	created, err := applications.Create(context.Background(), 7, 11, DescriptorRequest{}, models.DataApplicationCreateRequest{Name: "Orders", SourceViewIDs: []string{view.ID}})
	if err != nil {
		t.Fatal(err)
	}

	valid := cloneDataApplicationSnapshot(t, created.Snapshot)
	componentID := valid.Components[0].ID
	valid.Components[0].QueryTemplate.Select = append(valid.Components[0].QueryTemplate.Select, "status")
	valid.SelectionBindings = []models.DataApplicationSelectionBinding{{
		SourceComponentID: componentID,
		Assignments: []models.DataApplicationSelectionAssignment{{
			SourceField: "status", ApplicationParameterKey: "component_1.status",
		}},
	}}
	if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, valid); err != nil {
		t.Fatalf("valid selection binding error = %v", err)
	}
	updated, err := applications.Update(context.Background(), 7, 11, created.ID, DescriptorRequest{}, models.DataApplicationUpdateRequest{
		Name: created.Name, Snapshot: valid, Version: created.Version,
	})
	if err != nil || len(updated.Snapshot.SelectionBindings) != 1 {
		t.Fatalf("Update() selection binding = %#v, %v", updated, err)
	}
	published, err := applications.Publish(context.Background(), 7, 11, created.ID, DescriptorRequest{}, updated.Version)
	if err != nil {
		t.Fatalf("Publish() selection binding error = %v", err)
	}
	runtime, err := applications.Runtime(7, 11, created.ID)
	if err != nil || published.CurrentRevisionNumber == nil || len(runtime.Snapshot.SelectionBindings) != 1 {
		t.Fatalf("Runtime() selection binding = %#v, %v", runtime, err)
	}

	tests := []struct {
		name   string
		change func(*models.DataApplicationSnapshot)
	}{
		{name: "source field must be selected", change: func(snapshot *models.DataApplicationSnapshot) {
			snapshot.Components[0].QueryTemplate.Select = []string{"id", "amount"}
		}},
		{name: "source and target types must match", change: func(snapshot *models.DataApplicationSnapshot) {
			snapshot.SelectionBindings[0].Assignments[0].SourceField = "amount"
		}},
		{name: "application parameter must exist", change: func(snapshot *models.DataApplicationSnapshot) {
			snapshot.SelectionBindings[0].Assignments[0].ApplicationParameterKey = "missing"
		}},
		{name: "source component binding must be unique", change: func(snapshot *models.DataApplicationSnapshot) {
			snapshot.SelectionBindings = append(snapshot.SelectionBindings, snapshot.SelectionBindings[0])
		}},
		{name: "list operators are not selection targets", change: func(snapshot *models.DataApplicationSnapshot) {
			snapshot.Components[0].QueryTemplate.ParameterFilters[0].Operator = "in"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := cloneDataApplicationSnapshot(t, valid)
			test.change(&snapshot)
			if err := applications.validateSnapshot(context.Background(), DescriptorRequest{}, snapshot); !errors.Is(err, ErrInvalidDataApplication) {
				t.Fatalf("validateSnapshot() error = %v", err)
			}
		})
	}

	nullableDescriptor := testDescriptor(false)
	nullableDescriptor.OutputContract.Fields[2].Nullable = true
	nullableApplications := NewDataApplicationService(repository, &fakeDescriptorReader{descriptor: nullableDescriptor}, nil)
	required := cloneDataApplicationSnapshot(t, valid)
	required.Parameters[0].Required = true
	if err := nullableApplications.validateSnapshot(context.Background(), DescriptorRequest{}, required); !errors.Is(err, ErrInvalidDataApplication) {
		t.Fatalf("nullable source for required parameter error = %v", err)
	}
}

func cloneDataApplicationSnapshot(t *testing.T, snapshot models.DataApplicationSnapshot) models.DataApplicationSnapshot {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var cloned models.DataApplicationSnapshot
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func TestDecodeDataApplicationSnapshotCanonicalizesMissingDisplayMode(t *testing.T) {
	snapshot, err := decodeDataApplicationSnapshot([]byte(`{
		"schema_version":"addp.workbench_data_application/v1",
		"page":{"id":"page-a","title":"Page","placements":[]},
		"components":[],"parameters":[],"parameter_bindings":[],"selection_bindings":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Page.DisplayMode != models.ApplicationDisplayModeDesktop || snapshot.Page.RefreshIntervalSeconds == nil || *snapshot.Page.RefreshIntervalSeconds != models.ApplicationRefreshIntervalDisabled || !slices.Equal(snapshot.Page.VisibleSections, defaultApplicationVisibleSections()) {
		t.Fatalf("page = %#v", snapshot.Page)
	}

	snapshot, err = decodeDataApplicationSnapshot([]byte(`{
		"schema_version":"addp.workbench_data_application/v1",
		"page":{"id":"page-a","title":"Page","display_mode":"wallboard","refresh_interval_seconds":30,"visible_sections":["query_actions","title"],"placements":[]},
		"components":[],"parameters":[],"parameter_bindings":[],"selection_bindings":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(snapshot.Page.VisibleSections, []string{models.ApplicationVisibleSectionTitle, models.ApplicationVisibleSectionQueryActions}) {
		t.Fatalf("visible_sections = %#v", snapshot.Page.VisibleSections)
	}
}

func applicationRefreshInterval(value int) *int {
	return &value
}

func TestDataApplicationServiceRejectsComponentIdentityChanges(t *testing.T) {
	repository := newMemoryDataApplicationRepository()
	view := dataApplicationSourceView(7, 11)
	repository.views[view.ID] = view
	applications := NewDataApplicationService(repository, &fakeDescriptorReader{descriptor: testDescriptor(false)}, nil)
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
