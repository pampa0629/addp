package projectionstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestPrepareQueryProtectionKeepsUnmanagedTenantOffReadSetPath(t *testing.T) {
	store, err := New(openProjectionStoreDB(t), "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, calls := preparedReadSet(t, plugin.DynamicSchemaCatalogModel(), "Outdoor", "Persons")
	protect, err := store.PrepareQueryProtection(context.Background(), 7, plugin.DynamicSchemaCatalogModel(), prepared, "query", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := protect(&plugin.QueryResult{}); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatalf("ReadSet calls = %d, want zero for unmanaged tenant", *calls)
	}
}

func TestPrepareQueryProtectionDeniesEnrollingManagedReadSet(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	model := plugin.DynamicSchemaCatalogModel()
	identity := models.GenerateItemFingerprint(11, "Outdoor.Persons")
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity}
	projection := enrollingProjection(t, "manager", target)
	if err := store.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{
			ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-1",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	prepared, calls := preparedReadSet(t, model, "Outdoor", "Persons")
	if _, err := store.PrepareQueryProtection(context.Background(), 7, model, prepared, "query", time.Now().UTC()); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("gate error = %v, want ErrDenied", err)
	}
	if *calls != 1 {
		t.Fatalf("ReadSet calls = %d, want one", *calls)
	}
}

func TestRequireCatalogPathUnmanagedUsesExactDataItemIdentity(t *testing.T) {
	store, err := New(openProjectionStoreDB(t), "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		t.Fatal(err)
	}
	projection := enrollingProjection(t, "develop", target)
	if err := store.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{
			ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-1",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireCatalogPathUnmanaged(context.Background(), 7, model, path, time.Now().UTC()); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("RequireCatalogPathUnmanaged() error = %v", err)
	}
	other := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Groups")
	if err := store.RequireCatalogPathUnmanaged(context.Background(), 7, model, other, time.Now().UTC()); err != nil {
		t.Fatalf("unmanaged path error = %v", err)
	}
}

func TestPrepareTableProtectionMasksManagedNativeRows(t *testing.T) {
	store, err := New(openProjectionStoreDB(t), "transfer", "transfer", nil)
	if err != nil {
		t.Fatal(err)
	}
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	projection := activeTableProjection(t, "transfer", "export", model, path, fields)
	if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-table", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "cursor-table",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	protect, err := store.PrepareTableProtection(t.Context(), 7, model, path, fields, "export", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	result := &plugin.QueryResult{Rows: []map[string]interface{}{{"userInfo": map[string]interface{}{"phone": "13661384499"}}}}
	if err := protect(result); err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0]["userInfo"].(map[string]interface{})["phone"]; got != "136****4499" {
		t.Fatalf("masked phone = %#v", got)
	}
}

func TestPrepareTableProtectionDoesNotValidateUnmanagedPathFields(t *testing.T) {
	store, err := New(openProjectionStoreDB(t), "transfer", "transfer", nil)
	if err != nil {
		t.Fatal(err)
	}
	model := plugin.DynamicSchemaCatalogModel()
	managedPath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
	projection := activeTableProjection(t, "transfer", "export", model, managedPath, fields)
	if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-managed", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "cursor-managed",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	unmanagedPath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Activities")
	protect, err := store.PrepareTableProtection(t.Context(), 7, model, unmanagedPath, nil, "export", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := protect(&plugin.QueryResult{}); err != nil {
		t.Fatal(err)
	}
}

func activeTableProjection(t *testing.T, owner, action string, model plugin.EngineCatalogModelSpec, path plugin.EngineCatalogPath, fields []datatype.FieldInfo) dataprotection.Projection {
	t.Helper()
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		t.Fatal(err)
	}
	componentPath := fields[len(fields)-1].Path
	segments := make([]dataprotection.PathSegment, len(componentPath))
	for index, name := range componentPath {
		container := "object"
		if index == len(componentPath)-1 {
			container = "scalar"
		}
		segments[index] = dataprotection.PathSegment{Name: name, Container: container}
	}
	component := dataprotection.Component{Key: fields[len(fields)-1].Name, Path: segments, ValueType: string(fields[len(fields)-1].Type)}
	component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV1, ProjectionID: "projection-table", Revision: "00000000000000000001",
		ConsumerOwner: owner, State: dataprotection.ProjectionStateActive, Target: target, SourceSnapshotHash: snapshot,
		Rules: []dataprotection.Rule{{Action: action, Component: component, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
			Parameters:         map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit"},
			InvalidValueEffect: dataprotection.EffectSuppress,
		}}}, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	return projection
}

func preparedReadSet(t *testing.T, model plugin.EngineCatalogModelSpec, database, collection string) (plugin.PreparedQuery, *int) {
	t.Helper()
	calls := 0
	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown},
		func(context.Context) (*plugin.QueryReadSet, error) {
			calls++
			return plugin.NewQueryReadSet(plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, database, plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, collection))
		},
		nil,
		func(context.Context) (*plugin.QueryResult, error) { return &plugin.QueryResult{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	return prepared, &calls
}
