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
	_ "github.com/addp/common/engine/plugins/neo4j"
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
	if _, _, err := gate.BeginPreparedQuery(t.Context(), 7, enginePlugin, persons); !errors.Is(err, dataprotection.ErrDenied) || !errors.Is(err, ErrRequired) {
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
		&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
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
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
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
		&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
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
	if err := protect(&plugin.QueryResult{Columns: []string{"prefix"}, Rows: []map[string]interface{}{{"prefix": "136"}}}); !errors.Is(err, dataprotection.ErrDenied) || !errors.Is(err, ErrResultRequired) || !errors.Is(err, ErrRequired) {
		t.Fatalf("derived output protection error = %v", err)
	}
}

func preparedMongoRead(t *testing.T, model plugin.EngineCatalogModelSpec, collection string) plugin.PreparedQuery {
	t.Helper()
	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
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
	store, err := projectionstore.Migrate(db, "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func installDevelopProjection(t *testing.T, store *projectionstore.Store, identity string) {
	t.Helper()
	now := time.Now().UTC()
	projection := dataprotection.Projection{
		TargetEngineID: 11,
		SchemaVersion:  dataprotection.ProjectionSchemaV5,
		ProjectionID:   "projection-develop-persons",
		Revision:       "00000000000000000001",
		ConsumerOwner:  "develop",
		State:          dataprotection.ProjectionStateEnrolling,
		Target: dataprotection.ResourceReference{
			OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity,
		},
		Rules: []dataprotection.Rule{}, ValidFrom: now.Add(-time.Minute), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
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
	installActiveDevelopProjectionAt(t, store, identity, fields, time.Now().UTC())
}

func installActiveDevelopProjectionAt(t *testing.T, store *projectionstore.Store, identity string, fields []datatype.FieldInfo, now time.Time) {
	t.Helper()
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
		TargetEngineID: 11,
		SchemaVersion:  dataprotection.ProjectionSchemaV5, ProjectionID: "projection-develop-active", Revision: "00000000000000000001",
		ConsumerOwner: "develop", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity},
		SourceSnapshotHash: snapshot,
		Rules: []dataprotection.Rule{{Action: "query", Component: component, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2,
			Parameters:         map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
			InvalidValueEffect: dataprotection.EffectSuppress,
		}}}, ValidFrom: now.Add(-time.Minute), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
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

func TestGateRejectsExpiredActiveProjectionBeforeQueryExecution(t *testing.T) {
	store := developProjectionStore(t)
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true}, {Name: "phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true}}
	installActiveDevelopProjectionAt(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"), fields, time.Now().UTC().Add(-2*time.Hour))
	executions := 0
	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
		func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
		func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
			return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, IdentityOutput: true}}}, nil
		},
		func(context.Context) (*plugin.QueryResult, error) { executions++; return &plugin.QueryResult{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := plugin.Get("mongodb")
	if err != nil {
		t.Fatal(err)
	}
	gate := NewGate(store)
	protect, end, err := gate.BeginPreparedQuery(t.Context(), 7, provider, prepared)
	if !errors.Is(err, dataprotection.ErrDenied) || protect != nil || end != nil || executions != 0 || gate.HasActiveExecutionsForTenant(7) {
		t.Fatalf("expired query admission: error=%v, protector=%t, end=%t, executions=%d, active=%t", err, protect != nil, end != nil, executions, gate.HasActiveExecutionsForTenant(7))
	}
	if !store.Gate(7, dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: models.GenerateItemFingerprint(11, "Outdoor.Persons")}, time.Now().UTC()).Managed {
		t.Fatal("expired resource was released")
	}
}

func TestGraphGateUsesPreparedScopeAndRetainsInflightBarrier(t *testing.T) {
	store := developProjectionStore(t)
	installDevelopProjection(t, store, models.GenerateItemFingerprint(11, "Outdoor.Persons"))
	enginePlugin, err := plugin.Get("neo4j")
	if err != nil {
		t.Fatal(err)
	}
	gate := NewGate(store)
	plan, err := plugin.NewPreparedGraphQuery(&plugin.QueryAnalysis{Language: "cypher", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, func(context.Context) (*plugin.QueryEngineReadScope, error) { return plugin.NewQueryEngineReadScope(13) }, nil, func(context.Context) (*plugin.GraphQueryResult, error) { return &plugin.GraphQueryResult{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	end, err := gate.BeginPreparedGraphQuery(t.Context(), 7, enginePlugin, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("graph read bypassed inflight barrier")
	}
	if _, err := plan.ExecuteGraph(t.Context()); err != nil {
		t.Fatal(err)
	}
	end()
	if gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("completed graph read remained active")
	}
	unknown, err := plugin.NewPreparedGraphQuery(&plugin.QueryAnalysis{Language: "cypher", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil, nil, func(context.Context) (*plugin.GraphQueryResult, error) {
		t.Fatal("unproved graph executed")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.BeginPreparedGraphQuery(t.Context(), 7, enginePlugin, unknown); !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatalf("unknown graph admitted: %v", err)
	}
	if gate.HasActiveExecutionsForTenant(7) {
		t.Fatal("denied graph retained inflight lease")
	}
}

// The physical plan must see the verified execution actor before it decides
// which columns to mask; its persistent preview still uses default rules.
func TestGateBindsExecutionActorAndPersistsOnlyDefaultPreview(t *testing.T) {
	for _, mode := range []string{"live", "expired", "default deny", "conflicting actor", "changed actor"} {
		t.Run(mode, func(t *testing.T) {
			store := developProjectionStore(t)
			now := time.Now().UTC()
			provider, err := plugin.Get("postgresql")
			if err != nil {
				t.Fatal(err)
			}
			model := provider.(plugin.EngineCatalogModelProvider).EngineCatalogModel()
			path := plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "outdoor", "persons")
			fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
			component := dataprotection.Component{Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: "string"}
			component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
			if err != nil {
				t.Fatal(err)
			}
			schemaHash, err := dataprotection.TableSchemaSnapshotHash(fields)
			if err != nil {
				t.Fatal(err)
			}
			target, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
			if err != nil {
				t.Fatal(err)
			}
			actor := dataprotection.SubjectReference{Type: "user", ID: "41"}
			decision := dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, InvalidValueEffect: dataprotection.EffectSuppress}
			if mode == "default deny" {
				decision = dataprotection.Decision{Effect: dataprotection.EffectDeny, InvalidValueEffect: dataprotection.EffectDeny}
			}
			deadline := now.Add(time.Hour)
			if mode == "expired" {
				deadline = now.Add(-time.Minute)
			}
			projection := dataprotection.Projection{TargetEngineID: 11, SchemaVersion: dataprotection.ProjectionSchemaV5, ProjectionID: "actor-bound-query", Revision: "00000000000000000001", ConsumerOwner: "develop", State: dataprotection.ProjectionStateActive, Target: target, SourceSnapshotHash: schemaHash, ValidFrom: now.Add(-time.Hour), Rules: []dataprotection.Rule{{Action: "query", Component: component, Decision: decision, Authorizations: []dataprotection.TemporaryAuthorization{{Subject: actor, Effect: dataprotection.EffectAllow, ValidFrom: now.Add(-30 * time.Minute), ValidUntil: deadline}}}}}
			if err := projection.Seal(); err != nil {
				t.Fatal(err)
			}
			if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: "actor-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "actor-1"}, now); err != nil {
				t.Fatal(err)
			}
			executions := 0
			base, err := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageComplete}, nil,
				func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
				func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
					return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{"contact"}, Transformation: plugin.QueryOutputTransformationDirect}}}}}, nil
				},
				func(context.Context) (*plugin.QueryResult, error) {
					executions++
					return &plugin.QueryResult{Columns: []string{"contact"}, Fields: []datatype.FieldInfo{{Name: "contact", Type: datatype.FieldTypeString}}, Rows: []map[string]interface{}{{"contact": "13661384499"}}}, nil
				})
			if err != nil {
				t.Fatal(err)
			}
			prepared := &actorBoundTestQuery{PreparedQuery: base}
			ctx, capture := WithQueryCapture(t.Context(), actor)
			if mode == "conflicting actor" {
				ctx = WithInteractiveQuerySubject(ctx, dataprotection.SubjectReference{Type: "user", ID: "42"})
			}
			gate := NewGate(store)
			protect, end, err := gate.BeginPreparedQuery(ctx, 7, provider, prepared)
			if mode == "conflicting actor" {
				if !errors.Is(err, ErrRequired) || protect != nil || end != nil || executions != 0 || gate.HasActiveExecutionsForTenant(7) {
					t.Fatalf("conflicting actor admitted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer end()
			if mode == "expired" {
				if len(prepared.operations) != 1 {
					t.Fatal("expired actor bypassed native mask")
				}
			} else if len(prepared.operations) != 0 {
				t.Fatal("verified live actor was masked in the physical plan")
			}
			result, err := prepared.Execute(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "changed actor" {
				capture.Subject = dataprotection.SubjectReference{Type: "user", ID: "42"}
				if err := protect(result); !errors.Is(err, dataprotection.ErrDenied) || capture.Result != nil {
					t.Fatalf("actor substitution admitted: %v", err)
				}
				return
			}
			if err := protect(result); err != nil {
				t.Fatal(err)
			}
			if mode == "default deny" {
				if len(result.Rows) != 0 || len(result.Columns) != 0 || len(result.Fields) != 0 {
					t.Fatal("denied default preview retained data or metadata")
				}
			} else if result.Rows[0]["contact"] != "136****4499" {
				t.Fatal("persistent preview retained raw value")
			}
			if mode == "expired" {
				if capture.Result != nil {
					t.Fatal("expired grant cached plaintext")
				}
			} else {
				if capture.Result == nil || capture.Result.Result.Rows[0]["contact"] != "13661384499" {
					t.Fatal("verified live actor lost its ephemeral result")
				}
				live, err := gate.ReadQueryResult(ctx, 7, capture.Result, actor, nil)
				if err != nil || live.Rows[0]["contact"] != "13661384499" {
					t.Fatalf("current authorized read failed: %v", err)
				}
			}
			if _, err := prepared.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) || executions != 1 {
				t.Fatalf("query executed more than once: %v", err)
			}
		})
	}
}

type actorBoundTestQuery struct {
	plugin.PreparedQuery
	operations []plugin.QueryOutputTransform
}

func (q *actorBoundTestQuery) BindOutputTransforms(_ context.Context, operations []plugin.QueryOutputTransform) error {
	q.operations = append([]plugin.QueryOutputTransform(nil), operations...)
	return nil
}
