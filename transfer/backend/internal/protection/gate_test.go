package protection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/mongodb"
	_ "github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/models"
	commonmodels "github.com/addp/common/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeEngineGetter struct {
	engine *commonmodels.Engine
}

func (g fakeEngineGetter) GetEngineForTenant(context.Context, uint, uint) (*commonmodels.Engine, error) {
	return g.engine, nil
}

func TestSourceLocatorUsesTheSingleTaskSourceContract(t *testing.T) {
	locator, err := SourceLocator(map[string]interface{}{
		"source": map[string]interface{}{"locator": " addp://engine/11/path/Outdoor/Persons?type=collection "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if locator != "addp://engine/11/path/Outdoor/Persons?type=collection" {
		t.Fatalf("locator = %q", locator)
	}
	for _, config := range []map[string]interface{}{
		nil,
		{"source": map[string]interface{}{}},
		{"source": map[string]interface{}{"locator": ""}},
	} {
		if _, err := SourceLocator(config); err == nil {
			t.Fatalf("SourceLocator(%#v) must fail", config)
		}
	}
}

func TestGateDeniesOnlyTheExactManagedTransferSource(t *testing.T) {
	store := transferProjectionStore(t)
	installTransferProjection(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"))
	gate := NewGate(store, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "mongodb"}})

	err := gate.RequireLocator(t.Context(), 7, "addp://engine/11/path/Outdoor/Persons?type=collection")
	if !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("Persons gate error = %v, want ErrDenied", err)
	}
	if err := gate.RequireLocator(t.Context(), 7, "addp://engine/11/path/Outdoor/Groups?type=collection"); err != nil {
		t.Fatalf("Groups gate error = %v", err)
	}
}

func TestGatePreparesBoundedNativeAndQueryExportProtection(t *testing.T) {
	store := transferProjectionStore(t)
	model := plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, "outdoor", plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, "persons")
	fields := []datatype.FieldInfo{
		{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	installActiveTransferProjection(t, store, model, path, fields)
	gate := NewGate(store, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "postgresql"}})
	bound, err := gate.PrepareBoundedTableProtection(t.Context(), 7, map[string]interface{}{
		"source": map[string]interface{}{"locator": "addp://engine/11/path/outdoor/persons?type=table"},
	})
	if err != nil {
		t.Fatal(err)
	}
	nativeProtect, err := bound.PrepareCatalogTableProtection(t.Context(), path, fields)
	if err != nil {
		t.Fatal(err)
	}
	native := &plugin.QueryResult{Rows: []map[string]interface{}{{"phone": "13661384499"}}}
	if err := nativeProtect(native); err != nil {
		t.Fatal(err)
	}
	if got := native.Rows[0]["phone"]; got != "136****4499" {
		t.Fatalf("native phone = %#v", got)
	}

	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageComplete},
		func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
		func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
			return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, Bindings: []plugin.QueryOutputBinding{{
				SourcePath: []string{"phone"}, OutputPath: []string{"contact_phone"}, Transformation: plugin.QueryOutputTransformationDirect,
			}}}}}, nil
		},
		func(context.Context) (*plugin.QueryResult, error) { return nil, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	queryProtect, err := bound.PrepareQueryProtection(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	query := &plugin.QueryResult{Columns: []string{"contact_phone"}, Rows: []map[string]interface{}{{"contact_phone": "13501206490"}}}
	if err := queryProtect(query); err != nil {
		t.Fatal(err)
	}
	if got := query.Rows[0]["contact_phone"]; got != "135****6490" {
		t.Fatalf("query phone = %#v", got)
	}
}

func TestGatePreparesBoundedMongoEncodedRecordExportProtection(t *testing.T) {
	store := transferProjectionStore(t)
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	installActiveTransferProjectionForComponent(t, store, model, path, fields, dataprotection.Component{
		Key: "userInfo.phone", Path: []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString),
	})
	gate := NewGate(store, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "mongodb"}})
	protect, err := gate.PrepareBoundedEncodedRecordProtection(t.Context(), 7, map[string]interface{}{
		"source": map[string]interface{}{"locator": "addp://engine/11/path/Outdoor/Persons?type=collection"},
	}, fields)
	if err != nil {
		t.Fatal(err)
	}
	document := map[string]interface{}{"userInfo": map[string]interface{}{"phone": "13661384499"}}
	if err := protect(document); err != nil {
		t.Fatal(err)
	}
	if got := document["userInfo"].(map[string]interface{})["phone"]; got != "136****4499" {
		t.Fatalf("protected phone = %#v", got)
	}
}

func transferProjectionStore(t *testing.T) *projectionstore.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.New(db, "transfer", "transfer", nil)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func installTransferProjection(t *testing.T, store *projectionstore.Store, identity string) {
	t.Helper()
	now := time.Now().UTC()
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2,
		ProjectionID:  "projection-transfer-persons",
		Revision:      "00000000000000000001",
		ConsumerOwner: "transfer",
		State:         dataprotection.ProjectionStateEnrolling,
		Target: dataprotection.ResourceReference{
			OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity,
		},
		Rules: []dataprotection.Rule{}, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{
			ChangeID: "change-transfer-persons", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-transfer-persons",
	}, now); err != nil {
		t.Fatal(err)
	}
}

func installActiveTransferProjection(t *testing.T, store *projectionstore.Store, model plugin.EngineCatalogModelSpec, path plugin.EngineCatalogPath, fields []datatype.FieldInfo) {
	installActiveTransferProjectionForComponent(t, store, model, path, fields, dataprotection.Component{
		Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString),
	})
}

func installActiveTransferProjectionForComponent(t *testing.T, store *projectionstore.Store, model plugin.EngineCatalogModelSpec, path plugin.EngineCatalogPath, fields []datatype.FieldInfo, component dataprotection.Component) {
	t.Helper()
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		t.Fatal(err)
	}
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
		SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: "projection-transfer-active", Revision: "00000000000000000001",
		ConsumerOwner: "transfer", State: dataprotection.ProjectionStateActive, Target: target, SourceSnapshotHash: snapshot,
		Rules: []dataprotection.Rule{{Action: exportAction, Component: component, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
			Parameters:         map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit"},
			InvalidValueEffect: dataprotection.EffectSuppress,
		}}}, ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-transfer-active", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "cursor-transfer-active",
	}, now); err != nil {
		t.Fatal(err)
	}
}
