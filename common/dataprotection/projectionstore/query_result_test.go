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
	protect, err := store.PrepareQueryProtection(t.Context(), 7, model, prepared, "query", dataprotection.SubjectReference{}, now)
	if err != nil {
		t.Fatal(err)
	}
	raw := &plugin.QueryResult{Columns: []string{"id", output, "email"}, Rows: []map[string]interface{}{{"id": "one", output: "13661384499", "email": "unapproved@example.test"}}}
	history, err := protect.CaptureAuthorized(raw, user)
	if err != nil || history == nil {
		t.Fatalf("capture: %v", err)
	}
	before, _ := json.Marshal(history)
	if strings.Contains(string(before), "unapproved@example.test") {
		t.Fatal("unapproved plaintext retained in private history")
	}
	emailHash := history.Result.Rows[0]["email"]
	if err := protect.Apply(raw); err != nil {
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
	result, err := store.ReadAuthorizedQueryResult(t.Context(), 7, history, user, "query")
	if err != nil || result.Rows[0][output] != masked {
		t.Fatalf("revocation: %v %v", result, err)
	}
	projection.Revision = "00000000000000000003"
	projection.Rules[0].Decision = dataprotection.Decision{Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	apply("history-3", "history-2")
	if _, err := store.ReadAuthorizedQueryResult(t.Context(), 7, history, user, "query"); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("changed default: %v", err)
	}
	if _, err := store.ReadAuthorizedQueryResult(t.Context(), 8, history, user, "query"); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("other tenant: %v", err)
	}
}

func TestAuthorizedIdentityKeepsTypedArrayTraversal(t *testing.T) {
	now := time.Now().UTC()
	user := dataprotection.SubjectReference{Type: "user", ID: "41"}
	rule := dataprotection.Rule{Action: "query", Component: dataprotection.Component{Key: "contacts.phone", Path: []dataprotection.PathSegment{{Name: "contacts", Container: "array"}, {Name: "phone", Container: "scalar"}}, ValueType: "string"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, InvalidValueEffect: dataprotection.EffectSuppress}, Authorizations: []dataprotection.TemporaryAuthorization{{Subject: user, Effect: dataprotection.EffectAllow, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}}}
	row := &plugin.QueryResult{Rows: []map[string]interface{}{{"contacts": []interface{}{map[string]interface{}{"phone": "13661384499", "label": "home"}}}}}
	history, err := captureAuthorizedQuery(row, []querySourceProtection{{source: plugin.QueryOutputSource{IdentityOutput: true}, rules: []dataprotection.Rule{rule}}}, user, 7, "query")
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
