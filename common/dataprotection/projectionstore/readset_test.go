package projectionstore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestPrepareQueryProtectionKeepsUnmanagedTenantOffReadSetPath(t *testing.T) {
	store, err := Migrate(openProjectionStoreDB(t), "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	initializeTenant(t, store, 7)
	prepared, calls := preparedReadSet(t, plugin.DynamicSchemaCatalogModel(), "Outdoor", "Persons")
	protect, err := store.PrepareQueryProtection(context.Background(), 7, plugin.DynamicSchemaCatalogModel(), prepared, "query", dataprotection.SubjectReference{}, nil, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := protect.Apply(&plugin.QueryResult{}); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatalf("ReadSet calls = %d, want zero for unmanaged tenant", *calls)
	}
}

func TestProtectionGateReasonsUseCurrentFacts(t *testing.T) {
	for _, name := range []string{"initial_sync", "enrolling", "future", "schema", "unsupported_operation"} {
		t.Run(name, func(t *testing.T) {
			store, err := Migrate(openProjectionStoreDB(t), "develop", "develop", nil)
			if err != nil {
				t.Fatal(err)
			}
			model := plugin.DynamicSchemaCatalogModel()
			path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
			fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
			now := time.Now().UTC()
			if name == "initial_sync" {
				_, err := store.PrepareTableProtection(t.Context(), 7, model, path, fields, "query", now)
				if !errors.Is(err, ErrNotInitialized) || !errors.Is(err, dataprotection.ErrNotReady) {
					t.Fatalf("lost initial sync: %v", err)
				}
				return
			}
			projection := activeTableProjection(t, "develop", "query", model, path, fields)
			want := dataprotection.ErrNotReady
			switch name {
			case "enrolling":
				projection.State, projection.SourceSnapshotHash, projection.Rules = dataprotection.ProjectionStateEnrolling, "", nil
			case "future":
				projection.ValidFrom, projection.ExpiresAt = now.Add(time.Hour), nil
				want = dataprotection.ErrNotYetValid
			case "schema":
				want = dataprotection.ErrSchemaMismatch
			case "unsupported_operation":
				want = dataprotection.ErrOperationUnsupported
			}
			if err := projection.Seal(); err != nil {
				t.Fatal(err)
			}
			installedAt := now
			if name == "future" {
				installedAt = projection.ValidFrom
			}
			if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
				SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
				Changes:       []dataprotection.ProjectionChange{{ChangeID: name, Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: name,
			}, installedAt); err != nil {
				t.Fatal(err)
			}
			if name == "schema" {
				fields[0].Type = datatype.FieldTypeInt
			}
			if name == "unsupported_operation" {
				err = store.RequireCatalogPathUnmanaged(t.Context(), 7, model, path, now)
			} else {
				_, err = store.PrepareTableProtection(t.Context(), 7, model, path, fields, "query", now)
			}
			if !errors.Is(err, want) {
				t.Fatalf("wrong protection reason: %v", err)
			}
		})
	}
}

func TestPreparedOutputRejectsExpiredProjection(t *testing.T) {
	for owner, action := range map[string]string{"manager": "preview", "develop": "query", "service": "service_execute", "transfer": "export"} {
		t.Run(owner, func(t *testing.T) {
			store, err := Migrate(openProjectionStoreDB(t), "manager", owner, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertPreparedOutputExpiry(t, store, owner, action)
		})
	}
}

// Reused by the real PostgreSQL gate. Historical protocol times avoid sleeps,
// clock overrides and direct writes to projection storage.
func assertPreparedOutputExpiry(t *testing.T, store *Store, owner, action string) {
	t.Helper()
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
	projection := activeTableProjection(t, owner, action, model, path, fields)
	projection.ValidFrom = time.Now().UTC().Add(-366 * 24 * time.Hour)
	projection.ExpiresAt = dataprotection.ProjectionDeadline(projection.ValidFrom.Add(365 * 24 * time.Hour))
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "expiry-" + owner, Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "expiry-" + owner,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	validAt := projection.ExpiresAt.Add(-time.Nanosecond)
	for _, at := range []time.Time{validAt, *projection.ExpiresAt, projection.ExpiresAt.Add(time.Nanosecond)} {
		gate := store.Gate(7, projection.Target, at)
		if !gate.Managed || gate.State != dataprotection.ProjectionStateActive || (gate.Err != nil) != !at.Before(*projection.ExpiresAt) {
			t.Fatalf("expiry gate at %s: managed=%t, state=%s, error=%v", at, gate.Managed, gate.State, gate.Err)
		}
	}
	prepared, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
		func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
		func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
			return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, IdentityOutput: true}}}, nil
		},
		func(context.Context) (*plugin.QueryResult, error) { return nil, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	staleProtectors := []*dataprotection.PreparedTableProtection{}
	for _, query := range []bool{false, true} {
		prepare := func(at time.Time) (*dataprotection.PreparedTableProtection, error) {
			if query {
				return store.PrepareQueryProtection(t.Context(), 7, model, prepared, action, dataprotection.SubjectReference{}, nil, at)
			}
			return store.PrepareTableProtection(t.Context(), 7, model, path, fields, action, at)
		}
		protect, err := prepare(validAt)
		if err != nil {
			t.Fatalf("prepare query=%t before expiry: %v", query, err)
		}
		staleProtectors = append(staleProtectors, protect)
		if _, err := prepare(*projection.ExpiresAt); !errors.Is(err, dataprotection.ErrDenied) || !errors.Is(err, dataprotection.ErrExpired) {
			t.Fatalf("prepare query=%t at expiry: %v", query, err)
		}
		for _, empty := range []bool{false, true} {
			result := &plugin.QueryResult{Columns: []string{"phone"}}
			if !empty {
				result.Rows = []map[string]interface{}{{"phone": "13661384499"}}
			}
			if err := protect.Apply(result); !errors.Is(err, dataprotection.ErrDenied) || !errors.Is(err, dataprotection.ErrExpired) {
				t.Fatalf("output query=%t empty=%t after expiry: %v", query, empty, err)
			}
			if len(result.Columns) != 1 || result.Columns[0] != "phone" || !empty && result.Rows[0]["phone"] != "13661384499" {
				t.Fatal("expired output must be rejected before changing any rows or columns")
			}
		}
	}
	if cursor, err := store.CurrentCursor(t.Context(), 7); err != nil || cursor != "expiry-"+owner {
		t.Fatalf("expiry changed the installed cursor: %q, %v", cursor, err)
	}
	if gate := store.Gate(7, projection.Target, time.Now().UTC()); !gate.Managed || gate.Err == nil {
		t.Fatal("expiry must not release the managed-resource marker")
	}

	// A later-expiring neighbor must not be transformed before the expired
	// source is checked; this matters for joins and multi-source queries.
	neighborPath := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Groups")
	neighbor := activeTableProjection(t, owner, action, model, neighborPath, fields)
	neighbor.ProjectionID = "neighbor-projection"
	neighbor.ValidFrom = projection.ValidFrom.Add(2 * 24 * time.Hour)
	neighbor.ExpiresAt = dataprotection.ProjectionDeadline(neighbor.ValidFrom.Add(365 * 24 * time.Hour))
	if err := neighbor.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "expiry-"+owner, &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "neighbor-" + owner, Operation: dataprotection.ChangeOperationUpsert, Projection: &neighbor}},
		NextCursor:    "neighbor-" + owner,
	}, validAt); err != nil {
		t.Fatal(err)
	}
	joined, err := plugin.NewPreparedQuery(
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
		func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(neighborPath, path) },
		func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
			return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{
				{Path: neighborPath, Fields: fields, Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{"neighbor_phone"}, Transformation: plugin.QueryOutputTransformationDirect}}},
				{Path: path, Fields: fields, IdentityOutput: true},
			}}, nil
		},
		func(context.Context) (*plugin.QueryResult, error) { return nil, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	joinedProtect, err := store.PrepareQueryProtection(t.Context(), 7, model, joined, action, dataprotection.SubjectReference{}, nil, validAt)
	if err != nil {
		t.Fatal(err)
	}
	joinedResult := &plugin.QueryResult{Columns: []string{"phone", "neighbor_phone"}, Rows: []map[string]interface{}{{"phone": "13661384499", "neighbor_phone": "13501206490"}}}
	if err := joinedProtect.Apply(joinedResult); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("multi-source expiry output: %v", err)
	}
	if joinedResult.Rows[0]["phone"] != "13661384499" || joinedResult.Rows[0]["neighbor_phone"] != "13501206490" {
		t.Fatal("all projections must be checked before changing the first source's output")
	}
	staleProtectors = append(staleProtectors, joinedProtect)

	// Another process may retain the expired local index. A formal newer
	// revision must restore only newly prepared reads, never the old closures.
	reader, err := Open(store.db, store.schema, owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gate := reader.Gate(7, projection.Target, time.Now().UTC()); !gate.Managed || gate.Err == nil {
		t.Fatal("reopened store must retain the expired managed projection")
	}
	replacement := activeTableProjection(t, owner, action, model, path, fields)
	replacement.Revision = "00000000000000000002"
	replacement.ExpiresAt = dataprotection.ProjectionDeadline(replacement.ValidFrom.Add(365 * 24 * time.Hour))
	replacement.Rules[0].Decision = dataprotection.Decision{
		Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmConstantV1,
		Parameters: map[string]interface{}{"value": "renewed-protected"}, InvalidValueEffect: dataprotection.EffectSuppress,
	}
	if err := replacement.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(t.Context(), 7, "neighbor-"+owner, &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "renewal-" + owner, Operation: dataprotection.ChangeOperationUpsert, Projection: &replacement}},
		NextCursor:    "renewal-" + owner,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, stale := range staleProtectors {
		for _, empty := range []bool{false, true} {
			result := &plugin.QueryResult{Columns: []string{"phone", "neighbor_phone"}}
			if !empty {
				result.Rows = []map[string]interface{}{{"phone": "13661384499", "neighbor_phone": "13501206490"}}
			}
			before := &plugin.QueryResult{Columns: []string{"phone", "neighbor_phone"}}
			if !empty {
				before.Rows = []map[string]interface{}{{"phone": "13661384499", "neighbor_phone": "13501206490"}}
			}
			if err := stale.Apply(result); !errors.Is(err, dataprotection.ErrDenied) || !reflect.DeepEqual(result, before) {
				t.Fatalf("replacement revived an old output protector: empty=%t, error=%v, result=%#v", empty, err, result)
			}
		}
	}
	for _, restarted := range []bool{false, true} {
		if restarted {
			reader, err = Open(store.db, store.schema, owner, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, query := range []bool{false, true} {
			var protect *dataprotection.PreparedTableProtection
			if query {
				protect, err = reader.PrepareQueryProtection(t.Context(), 7, model, prepared, action, dataprotection.SubjectReference{}, nil, time.Now().UTC())
			} else {
				protect, err = reader.PrepareTableProtection(t.Context(), 7, model, path, fields, action, time.Now().UTC())
			}
			if err != nil {
				t.Fatalf("prepare renewed output restarted=%t query=%t: %v", restarted, query, err)
			}
			for _, empty := range []bool{false, true} {
				result := &plugin.QueryResult{Columns: []string{"phone"}}
				if !empty {
					result.Rows = []map[string]interface{}{{"phone": "13661384499"}, {"phone": nil}}
				}
				if err := protect.Apply(result); err != nil {
					t.Fatalf("apply renewed output restarted=%t query=%t empty=%t: %v", restarted, query, empty, err)
				}
				if !reflect.DeepEqual(result.Columns, []string{"phone"}) || empty && len(result.Rows) != 0 || !empty && (len(result.Rows) != 2 || result.Rows[0]["phone"] != "renewed-protected" || result.Rows[1]["phone"] != nil) {
					t.Fatalf("renewed output must use the new decision and preserve null/empty results: %#v", result)
				}
			}
		}
		gate := reader.Gate(7, projection.Target, time.Now().UTC())
		if !gate.Managed || gate.Err != nil || len(gate.Projections) != 1 || gate.Projections[0].Revision != replacement.Revision {
			t.Fatalf("renewed gate restarted=%t: %#v", restarted, gate)
		}
		if cursor, err := reader.CurrentCursor(t.Context(), 7); err != nil || cursor != "renewal-"+owner {
			t.Fatalf("renewed cursor restarted=%t: %q, %v", restarted, cursor, err)
		}
	}
}

func TestPrepareQueryProtectionDeniesEnrollingManagedReadSet(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := Migrate(db, "manager", "manager", nil)
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
	if _, err := store.PrepareQueryProtection(context.Background(), 7, model, prepared, "query", dataprotection.SubjectReference{}, nil, time.Now().UTC()); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("gate error = %v, want ErrDenied", err)
	}
	if *calls != 1 {
		t.Fatalf("ReadSet calls = %d, want one", *calls)
	}
}

func TestPrepareQueryProtectionUsesExactSubjectForMetadataAndOutput(t *testing.T) {
	for _, effect := range []string{dataprotection.EffectMask, dataprotection.EffectSuppress} {
		for _, test := range []struct {
			name                    string
			subject                 dataprotection.SubjectReference
			expired, empty, allowed bool
		}{
			{name: "matching user", subject: dataprotection.SubjectReference{Type: "user", ID: "41"}, allowed: true},
			{name: "other user", subject: dataprotection.SubjectReference{Type: "user", ID: "42"}},
			{name: "empty subject"},
			{name: "expired permission", subject: dataprotection.SubjectReference{Type: "user", ID: "41"}, expired: true},
			{name: "authorized empty page", subject: dataprotection.SubjectReference{Type: "user", ID: "41"}, allowed: true, empty: true},
			{name: "protected empty page", empty: true},
		} {
			t.Run(effect+"/"+test.name, func(t *testing.T) {
				store, err := Migrate(openProjectionStoreDB(t), "manager", "manager", nil)
				if err != nil {
					t.Fatal(err)
				}
				model := plugin.DynamicSchemaCatalogModel()
				path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
				fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
				projection := activeTableProjection(t, "manager", "preview", model, path, fields)
				now := time.Now().UTC()
				validUntil := now.Add(time.Minute)
				if test.expired {
					validUntil = now.Add(-time.Second)
				}
				if effect == dataprotection.EffectSuppress {
					projection.Rules[0].Decision = dataprotection.Decision{Effect: effect, InvalidValueEffect: effect}
				}
				projection.Rules[0].Authorizations = []dataprotection.TemporaryAuthorization{{Subject: dataprotection.SubjectReference{Type: "user", ID: "41"}, Effect: dataprotection.EffectAllow, ValidFrom: projection.ValidFrom, ValidUntil: validUntil}}
				if err := projection.Seal(); err != nil {
					t.Fatal(err)
				}
				if err := store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{
					SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
					Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-subject", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
					NextCursor:    "cursor-subject",
				}, now); err != nil {
					t.Fatal(err)
				}
				prepared, err := plugin.NewPreparedQuery(
					&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
					func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
					func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
						return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{"contact"}, Transformation: plugin.QueryOutputTransformationDirect}}}}}, nil
					},
					func(context.Context) (*plugin.QueryResult, error) {
						result := &plugin.QueryResult{Columns: []string{"id", "contact"}}
						if !test.empty {
							result.Rows = []map[string]interface{}{{"id": 1, "contact": "13661384499"}}
						}
						return result, nil
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				protect, err := store.PrepareQueryProtection(t.Context(), 7, model, prepared, "preview", test.subject, nil, now)
				if err != nil {
					t.Fatal(err)
				}
				wantDerived := []string{"contact"}
				if test.allowed {
					wantDerived = []string{}
				}
				if !reflect.DeepEqual(protect.DerivedFields, wantDerived) {
					t.Fatalf("derived = %v, want %v", protect.DerivedFields, wantDerived)
				}
				result, err := prepared.Execute(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if err := protect.Apply(result); err != nil {
					t.Fatal(err)
				}
				wantColumns := []string{"id", "contact"}
				if !test.allowed && effect == dataprotection.EffectSuppress {
					wantColumns = []string{"id"}
				}
				if !reflect.DeepEqual(result.Columns, wantColumns) {
					t.Fatalf("columns = %v, want %v", result.Columns, wantColumns)
				}
				if !test.empty {
					value, exists := result.Rows[0]["contact"]
					switch {
					case test.allowed:
						if value != "13661384499" {
							t.Fatalf("original = %v", value)
						}
					case effect == dataprotection.EffectSuppress:
						if exists {
							t.Fatal("suppressed field is present")
						}
					default:
						if value != "136****4499" {
							t.Fatalf("masked = %v", value)
						}
					}
				}
			})
		}
	}
}

func TestRequireCatalogPathUnmanagedUsesExactDataItemIdentity(t *testing.T) {
	store, err := Migrate(openProjectionStoreDB(t), "develop", "develop", nil)
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
	store, err := Migrate(openProjectionStoreDB(t), "transfer", "transfer", nil)
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
	if protect.ValidateSourceSchema == nil || protect.ValidateSourceSchema(fields) != nil {
		t.Fatal("managed native protection must bind its current-schema validator")
	}
	drifted := append(append([]datatype.FieldInfo(nil), fields...), datatype.FieldInfo{Name: "extra", Type: datatype.FieldTypeInt})
	if !errors.Is(protect.ValidateSourceSchema(drifted), dataprotection.ErrSchemaMismatch) {
		t.Fatal("native schema validator accepted an added unrelated field")
	}
	if !errors.Is(protect.ValidateSourceSchema(nil), dataprotection.ErrSchemaMismatch) {
		t.Fatal("missing live structure must retain the schema-mismatch reason")
	}
	result := &plugin.QueryResult{Rows: []map[string]interface{}{{"userInfo": map[string]interface{}{"phone": "13661384499"}}}}
	if err := protect.Apply(result); err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0]["userInfo"].(map[string]interface{})["phone"]; got != "136****4499" {
		t.Fatalf("masked phone = %#v", got)
	}
}

func TestPrepareTableProtectionDoesNotValidateUnmanagedPathFields(t *testing.T) {
	store, err := Migrate(openProjectionStoreDB(t), "transfer", "transfer", nil)
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
	if protect.ValidateSourceSchema != nil {
		t.Fatal("unmanaged source must not require a live schema read")
	}
	if err := protect.Apply(&plugin.QueryResult{}); err != nil {
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
		TargetEngineID: path.EngineID,
		SchemaVersion:  dataprotection.ProjectionSchemaV5, ProjectionID: "projection-table", Revision: "00000000000000000001",
		ConsumerOwner: owner, State: dataprotection.ProjectionStateActive, Target: target, SourceSnapshotHash: snapshot,
		Rules: []dataprotection.Rule{{Action: action, Component: component, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2,
			Parameters:         map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
			InvalidValueEffect: dataprotection.EffectSuppress,
		}}}, ValidFrom: now.Add(-time.Minute), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
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
		&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
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
