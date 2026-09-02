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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGateDeniesExactManagedPathAndTracksOnlyAllowedRequest(t *testing.T) {
	store := serviceProjectionStore(t)
	installServiceProjection(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"))
	gate := NewGate(store)
	enginePlugin, err := plugin.Get("mongodb")
	if err != nil {
		t.Fatal(err)
	}
	model := enginePlugin.(plugin.EngineCatalogModelProvider).EngineCatalogModel()
	persons := plugin.EngineCatalogBranchLeafPath(model, 11,
		plugin.EngineCatalogTermDatabase, "Outdoor",
		plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	if _, err := gate.BeginCatalogPath(t.Context(), 7, enginePlugin, persons); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("Persons path gate error = %v, want ErrDenied", err)
	}
	if gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("denied request must not remain active")
	}

	groups := plugin.EngineCatalogBranchLeafPath(model, 11,
		plugin.EngineCatalogTermDatabase, "Outdoor",
		plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Groups")
	end, err := gate.BeginCatalogPath(t.Context(), 7, enginePlugin, groups)
	if err != nil {
		t.Fatal(err)
	}
	if !gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("allowed request must remain active until its read completes")
	}
	end()
	end()
	if gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("completed request remained active")
	}
}

func TestGateMasksManagedMongoQueryForServiceExecute(t *testing.T) {
	store := serviceProjectionStore(t)
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11,
		plugin.EngineCatalogTermDatabase, "Outdoor",
		plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	installActiveServiceProjection(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"), fields)
	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown},
		func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
		func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
			return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, IdentityOutput: true}}}, nil
		},
		func(context.Context) (*plugin.QueryResult, error) { return &plugin.QueryResult{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	enginePlugin, err := plugin.Get("mongodb")
	if err != nil {
		t.Fatal(err)
	}
	protect, end, err := NewGate(store).BeginPreparedQuery(t.Context(), 7, enginePlugin, prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	result := &plugin.QueryResult{Columns: []string{"userInfo"}, Rows: []map[string]interface{}{{
		"userInfo": map[string]interface{}{"phone": "13661384499"},
	}}}
	if err := protect(result); err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0]["userInfo"].(map[string]interface{})["phone"]; got != "136****4499" {
		t.Fatalf("masked phone = %#v", got)
	}
}

func TestGateAllowsUnmanagedPostgresQueryWithoutOutputLineage(t *testing.T) {
	store := serviceProjectionStore(t)
	installServiceProjection(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"))
	path := plugin.TabularItemPath(2, plugin.EngineCatalogTermSchema, "outdoor", "ods_outdoor_activities")
	lineageCalls := 0
	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown},
		func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
		func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
			lineageCalls++
			return nil, errors.New("unmanaged query must not resolve output lineage")
		},
		func(context.Context) (*plugin.QueryResult, error) { return &plugin.QueryResult{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	enginePlugin, err := plugin.Get("postgresql")
	if err != nil {
		t.Fatal(err)
	}
	protect, end, err := NewGate(store).BeginPreparedQuery(t.Context(), 7, enginePlugin, prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	if lineageCalls != 0 {
		t.Fatalf("OutputLineage calls = %d, want zero", lineageCalls)
	}
	if err := protect(&plugin.QueryResult{}); err != nil {
		t.Fatal(err)
	}
}

type recordingPurger struct{ calls int }

func (p *recordingPurger) PurgeProtectionDerivedData(context.Context, int64) error {
	p.calls++
	return nil
}

func TestAcknowledgementBarrierWaitsForOldRequestAndPurgesOncePerCursor(t *testing.T) {
	store := serviceProjectionStore(t)
	gate := NewGate(store)
	purger := &recordingPurger{}
	barrier := NewAcknowledgementBarrier(gate, purger)

	end, err := gate.BeginUnresolvedRead(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor-1"); err == nil {
		t.Fatal("barrier acknowledged while an old request was active")
	}
	if purger.calls != 0 {
		t.Fatalf("purge calls while blocked = %d", purger.calls)
	}
	end()
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor-1"); err != nil {
		t.Fatal(err)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor-1"); err != nil {
		t.Fatal(err)
	}
	if purger.calls != 1 {
		t.Fatalf("purge calls for one cursor = %d", purger.calls)
	}
	if err := barrier.ReadyToAcknowledge(t.Context(), 7, "cursor-2"); err != nil {
		t.Fatal(err)
	}
	if purger.calls != 2 {
		t.Fatalf("purge calls for two cursors = %d", purger.calls)
	}
}

func serviceProjectionStore(t *testing.T) *projectionstore.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS service").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.New(db, "service", "service", nil)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func installServiceProjection(t *testing.T, store *projectionstore.Store, identity string) {
	t.Helper()
	now := time.Now().UTC()
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV1,
		ProjectionID:  "projection-service-persons",
		Revision:      "00000000000000000001",
		ConsumerOwner: "service",
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
			ChangeID: "change-service-persons", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-service-persons",
	}, now); err != nil {
		t.Fatal(err)
	}
}

func installActiveServiceProjection(t *testing.T, store *projectionstore.Store, identity string, fields []datatype.FieldInfo) {
	t.Helper()
	now := time.Now().UTC()
	component := dataprotection.Component{
		Key:       "userInfo.phone",
		Path:      []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}},
		ValueType: string(datatype.FieldTypeString),
	}
	var err error
	component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV1, ProjectionID: "projection-service-active", Revision: "00000000000000000001",
		ConsumerOwner: "service", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity},
		SourceSnapshotHash: snapshot,
		Rules: []dataprotection.Rule{{Action: "service_execute", Component: component, Decision: dataprotection.Decision{
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
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-service-active", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "cursor-service-active",
	}, now); err != nil {
		t.Fatal(err)
	}
}
