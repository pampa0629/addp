package projectionstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestAuthorizedQueryResultRechecksCurrentGrantWithoutMutatingSnapshot(t *testing.T) {
	for _, identity := range []bool{true, false} {
		store, err := Migrate(openProjectionStoreDB(t), "develop", "develop", nil)
		if err != nil {
			t.Fatal(err)
		}
		assertAuthorizedQueryResultLifecycle(t, store, identity)
	}
}

func assertAuthorizedQueryResultLifecycle(t *testing.T, store *Store, identity bool) {
	t.Helper()
	var err error
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{{Name: "email", Path: []string{"email"}, Type: datatype.FieldTypeString, Nullable: true}, {Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
	projection := activeTableProjection(t, "develop", "query", model, path, fields)
	projection.ExpiresAt = nil
	emailComponent := dataprotection.Component{Key: "email", Path: []dataprotection.PathSegment{{Name: "email", Container: "scalar"}}, ValueType: "string"}
	emailComponent.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, emailComponent)
	if err != nil {
		t.Fatal(err)
	}
	projection.Rules = append(projection.Rules, dataprotection.Rule{Action: "query", Component: emailComponent, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmSM3V1, Parameters: map[string]interface{}{}, InvalidValueEffect: dataprotection.EffectSuppress}})
	now := time.Now().UTC()
	deadline := now.Add(time.Hour)
	user := dataprotection.SubjectReference{Type: "user", ID: "41"}
	projection.Rules[0].Authorizations = []dataprotection.TemporaryAuthorization{{Subject: user, Effect: dataprotection.EffectAllow, ValidFrom: projection.ValidFrom, ValidUntil: deadline}}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	apply := func(cursor, previous string) {
		t.Helper()
		if err := store.ApplyBatch(t.Context(), 7, previous, &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: cursor, Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: cursor}, now); err != nil {
			t.Fatal(err)
		}
	}
	apply("history-1", "")
	output := "phone"
	source := plugin.QueryOutputSource{Path: path, Fields: fields, IdentityOutput: identity}
	if !identity {
		output = "contact"
		source.Bindings = []plugin.QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{output}, Transformation: plugin.QueryOutputTransformationDirect}}
		source.Bindings = append(source.Bindings, plugin.QueryOutputBinding{SourcePath: []string{"email"}, OutputPath: []string{"email"}, Transformation: plugin.QueryOutputTransformationDirect})
	}
	prepared, err := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil, func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) }, func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
		return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{source}}, nil
	}, func(context.Context) (*plugin.QueryResult, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	protect, err := store.PrepareQueryProtection(t.Context(), 7, model, prepared, "query", user, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	raw := &plugin.QueryResult{Columns: []string{"id", output, "email"}, Rows: []map[string]interface{}{{"id": "one", output: "13661384499", "email": "unapproved@example.test"}}}
	history, err := protect.CaptureAuthorized(raw, user)
	if err != nil || history == nil {
		t.Fatalf("capture: %v", err)
	}
	if _, err := protect.CaptureAuthorized(raw, dataprotection.SubjectReference{Type: "user", ID: "42"}); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("prepared actor changed: %v", err)
	}
	before, _ := json.Marshal(history)
	if strings.Contains(string(before), "unapproved@example.test") {
		t.Fatal("unapproved plaintext retained in private history")
	}
	emailHash := history.Result.Rows[0]["email"]
	if err := protect.ApplyDefault(raw); err != nil {
		t.Fatal(err)
	}
	masked := raw.Rows[0][output]
	if masked == "13661384499" {
		t.Fatal("default persisted result contains plaintext")
	}
	for _, test := range []struct {
		name    string
		subject dataprotection.SubjectReference
		at      time.Time
		want    interface{}
	}{
		{"authorized", user, now, "13661384499"},
		{"other user", dataprotection.SubjectReference{Type: "user", ID: "42"}, now, masked},
		{"exact expiry", user, deadline, masked},
		{"after expiry", user, deadline.Add(time.Second), masked},
	} {
		result, err := store.readAuthorizedQueryResultAt(t.Context(), 7, history, test.subject, "query", test.at)
		if err != nil || result.Rows[0][output] != test.want {
			t.Fatalf("%s: result=%v err=%v", test.name, result, err)
		}
		if result.Rows[0]["email"] != emailHash {
			t.Fatal("previously hashed output was hashed twice")
		}
	}
	after, _ := json.Marshal(history)
	if string(before) != string(after) {
		t.Fatal("history mutated by reads")
	}
	projection.Revision = "00000000000000000002"
	projection.Rules[0].Authorizations = nil
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	apply("history-2", "history-1")
	result, err := store.ReadAuthorizedQueryResult(t.Context(), 7, history, user, "query", nil)
	if err != nil || result.Rows[0][output] != masked {
		t.Fatalf("revocation: %v %v", result, err)
	}
	if result.Retained == nil || len(result.Retained.Sources[0].Source.Bindings) != 0 {
		t.Fatal("revoked raw bindings were retained")
	}
	projection.Revision = "00000000000000000003"
	projection.Rules[0].Authorizations = []dataprotection.TemporaryAuthorization{{Subject: user, Effect: dataprotection.EffectAllow, ValidFrom: now.Add(-time.Second), ValidUntil: deadline}}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	apply("history-3", "history-2")
	renewed, err := store.ReadAuthorizedQueryResult(t.Context(), 7, result.Retained, user, "query", nil)
	if err != nil || renewed.Rows[0][output] != masked || renewed.PlaintextExpiresAt != nil {
		t.Fatal("renewed grant resurrected a revoked retained original", err)
	}
	projection.Revision = "00000000000000000004"
	projection.Rules[0].Decision = dataprotection.Decision{Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	apply("history-4", "history-3")
	if _, err := store.ReadAuthorizedQueryResult(t.Context(), 7, history, user, "query", nil); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("changed default: %v", err)
	}
	if _, err := store.ReadAuthorizedQueryResult(t.Context(), 8, history, user, "query", nil); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("other tenant: %v", err)
	}
}

func TestAuthorizedIdentityKeepsTypedArrayTraversal(t *testing.T) {
	now := time.Now().UTC()
	user := dataprotection.SubjectReference{Type: "user", ID: "41"}
	rule := dataprotection.Rule{Action: "query", Component: dataprotection.Component{Key: "contacts.phone", Path: []dataprotection.PathSegment{{Name: "contacts", Container: "array"}, {Name: "phone", Container: "scalar"}}, ValueType: "string"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, InvalidValueEffect: dataprotection.EffectSuppress}, Authorizations: []dataprotection.TemporaryAuthorization{{Subject: user, Effect: dataprotection.EffectAllow, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}}}
	row := &plugin.QueryResult{Rows: []map[string]interface{}{{"contacts": []interface{}{map[string]interface{}{"phone": "13661384499", "label": "home"}}}}}
	history, err := captureAuthorizedQuery(row, []querySourceProtection{{source: plugin.QueryOutputSource{IdentityOutput: true}, rules: []dataprotection.Rule{rule}}}, user, 7, "query", nil)
	if err != nil || history == nil {
		t.Fatalf("capture array: %v", err)
	}
	rule.Authorizations = nil
	source, rules := authorizedOutputRules(history.Sources[0].Source, []dataprotection.Rule{rule})
	if !source.IdentityOutput || len(source.Bindings) != 0 || len(rules) != 1 {
		t.Fatal("identity traversal flattened")
	}
	if err := dataprotection.ProtectQueryResultSourceAt(&history.Result, source, "query", rules, user, now); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(history.Result)
	if strings.Contains(string(encoded), "13661384499") || !strings.Contains(string(encoded), "136****4499") || !strings.Contains(string(encoded), "home") {
		t.Fatal("nested array output not protected")
	}
}

// Preparation fixes the maximum raw-output eligibility. Wall-clock progress
// may narrow that eligibility but must never expand it for an existing plan.
func TestQueryGrantCannotExpandDuringExecution(t *testing.T) {
	store, err := Migrate(openProjectionStoreDB(t), "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertQueryGrantExecutionBoundary(t, store)
}

func assertQueryGrantExecutionBoundary(t *testing.T, store *Store) {
	t.Helper()
	now := time.Now().UTC()
	preparedAt := now.Add(-2 * time.Minute)
	subject := dataprotection.SubjectReference{Type: "user", ID: "41"}
	model := plugin.DynamicSchemaCatalogModel()
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
	for index, test := range []struct {
		name        string
		from, until time.Time
	}{
		{"starts after preparation", now.Add(-time.Minute), now.Add(time.Hour)},
		{"expires before output", now.Add(-3 * time.Minute), now.Add(-time.Minute)},
	} {
		t.Run(test.name, func(t *testing.T) {
			tenant := int64(21 + index)
			path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
			projection := activeTableProjection(t, "develop", "query", model, path, fields)
			projection.ValidFrom = now.Add(-5 * time.Minute)
			projection.ExpiresAt = nil
			projection.Rules[0].Authorizations = []dataprotection.TemporaryAuthorization{{Subject: subject, Effect: dataprotection.EffectAllow, ValidFrom: test.from, ValidUntil: test.until}}
			if err := projection.Seal(); err != nil {
				t.Fatal(err)
			}
			if err := store.ApplyBatch(t.Context(), tenant, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: "timing-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "timing-1"}, now); err != nil {
				t.Fatal(err)
			}
			prepared, err := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
				func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
				func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
					return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, IdentityOutput: true}}}, nil
				},
				func(context.Context) (*plugin.QueryResult, error) { return nil, nil })
			if err != nil {
				t.Fatal(err)
			}
			protect, err := store.PrepareQueryProtection(t.Context(), tenant, model, prepared, "query", subject, nil, preparedAt)
			if err != nil {
				t.Fatal(err)
			}
			result := &plugin.QueryResult{Columns: []string{"phone"}, Rows: []map[string]interface{}{{"phone": "13661384499"}}}
			snapshot, err := protect.CaptureAuthorized(result, subject)
			if err != nil || snapshot != nil {
				t.Fatalf("late/expired grant captured: snapshot=%t err=%v", snapshot != nil, err)
			}
			if err := protect.Apply(result); err != nil {
				t.Fatal(err)
			}
			if result.Rows[0]["phone"] != "136****4499" {
				t.Fatalf("grant expanded or outlived deadline: %v", result.Rows)
			}
		})
	}
}
