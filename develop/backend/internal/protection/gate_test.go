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

func TestGateDeniesExactManagedPreparedQueryAndAllowsNeighbor(t *testing.T) {
	store := developProjectionStore(t)
	installDevelopProjection(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"))
	gate := NewGate(store)
	enginePlugin, err := plugin.Get("mongodb")
	if err != nil {
		t.Fatal(err)
	}
	model := enginePlugin.(plugin.EngineCatalogModelProvider).EngineCatalogModel()

	persons := preparedMongoRead(t, model, "Persons")
	if _, _, err := gate.BeginPreparedQuery(t.Context(), 7, enginePlugin, persons); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("Persons query gate error = %v, want ErrDenied", err)
	}
	if gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("denied query must not remain active")
	}
	groups := preparedMongoRead(t, model, "Groups")
	protect, end, err := gate.BeginPreparedQuery(t.Context(), 7, enginePlugin, groups)
	if err != nil {
		t.Fatalf("Groups query gate error = %v", err)
	}
	if !gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("allowed query must remain active until its read finishes")
	}
	if err := protect(&plugin.QueryResult{}); err != nil {
		t.Fatalf("unmanaged result protection error = %v", err)
	}
	end()
	end()
	if gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("finished query must be removed exactly once")
	}
}

func TestGateAllowsUnmanagedPostgresReadSetWhenTenantHasMongoProtection(t *testing.T) {
	store := developProjectionStore(t)
	installDevelopProjection(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"))
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
		t.Fatalf("OutputLineage calls = %d, want zero for an unmanaged PostgreSQL source", lineageCalls)
	}
	if err := protect(&plugin.QueryResult{}); err != nil {
		t.Fatal(err)
	}
}

func TestGateMasksManagedQueryResultFromProviderLineage(t *testing.T) {
	store := developProjectionStore(t)
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	installActiveDevelopProjection(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"), fields)
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
	result := &plugin.QueryResult{Columns: []string{"userInfo"}, Rows: []map[string]interface{}{
		{"userInfo": map[string]interface{}{"phone": "13661384499"}},
		{"userInfo": map[string]interface{}{"phone": "invalid"}},
	}}
	if err := protect(result); err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0]["userInfo"].(map[string]interface{})["phone"]; got != "136****4499" {
		t.Fatalf("masked phone = %#v", got)
	}
	if _, exists := result.Rows[1]["userInfo"].(map[string]interface{})["phone"]; exists {
		t.Fatal("invalid phone must be suppressed")
	}
}

func TestGateRejectsDerivedProtectedOutput(t *testing.T) {
	store := developProjectionStore(t)
	path := plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "outdoor", "persons")
	fields := []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString, Nullable: true}}
	installActiveDevelopProjection(t, store, models.GenerateItemFingerprint(11, "outdoor.persons"), fields)
	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown},
		func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
		func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
			return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, Bindings: []plugin.QueryOutputBinding{{
				SourcePath: []string{"phone"}, OutputPath: []string{"prefix"}, Transformation: plugin.QueryOutputTransformationDerived,
			}}}}}, nil
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
	if err := protect(&plugin.QueryResult{Columns: []string{"prefix"}, Rows: []map[string]interface{}{{"prefix": "136"}}}); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("derived output protection error = %v", err)
	}
}

func preparedMongoRead(t *testing.T, model plugin.EngineCatalogModelSpec, collection string) plugin.PreparedQuery {
	t.Helper()
	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown},
		func(context.Context) (*plugin.QueryReadSet, error) {
			return plugin.NewQueryReadSet(plugin.EngineCatalogBranchLeafPath(
				model, 11,
				plugin.EngineCatalogTermDatabase, "Outdoor",
				plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, collection,
			))
		},
		nil,
		func(context.Context) (*plugin.QueryResult, error) { return &plugin.QueryResult{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func developProjectionStore(t *testing.T) *projectionstore.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.New(db, "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func installDevelopProjection(t *testing.T, store *projectionstore.Store, identity string) {
	t.Helper()
	now := time.Now().UTC()
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2,
		ProjectionID:  "projection-develop-persons",
		Revision:      "00000000000000000001",
		ConsumerOwner: "develop",
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
			ChangeID: "change-develop-persons", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-develop-persons",
	}, now); err != nil {
		t.Fatal(err)
	}
}

func installActiveDevelopProjection(t *testing.T, store *projectionstore.Store, identity string, fields []datatype.FieldInfo) {
	t.Helper()
	now := time.Now().UTC()
	component := dataprotection.Component{Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString)}
	if len(fields) > 1 {
		component = dataprotection.Component{Key: "userInfo.phone", Path: []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString)}
	}
	fingerprint, err := dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	component.SchemaFingerprint = fingerprint
	snapshot, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: "projection-develop-active", Revision: "00000000000000000001",
		ConsumerOwner: "develop", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity},
		SourceSnapshotHash: snapshot,
		Rules: []dataprotection.Rule{{Action: "query", Component: component, Decision: dataprotection.Decision{
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
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-develop-active", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "cursor-develop-active",
	}, now); err != nil {
		t.Fatal(err)
	}
}
