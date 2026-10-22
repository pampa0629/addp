// Package conformance holds provider-independent protection assertions used by
// the existing native database gates. It is not a runtime execution route.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// QueryOutputProtection exercises an actual native PreparedQuery with the
// formal shared store. prepare selects the owned fixture fields id, phone,
// email, label and amount, with direct aliases or a base-table star mixed with direct field copies.
func QueryOutputProtection(t *testing.T, model plugin.EngineCatalogModelSpec, prepare func(context.Context) (plugin.PreparedQuery, error), openSession func(context.Context, plugin.PreparedQuery) (plugin.QueryReadSession, error)) {
	t.Helper()
	ctx := t.Context()
	original, err := prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := original.OutputLineage(ctx)
	if err != nil || len(lineage.Sources) != 1 {
		t.Fatal("hybrid source lineage", err)
	}
	source := lineage.Sources[0]
	outputNames := func(name string) []string {
		t.Helper()
		if source.IdentityOutput {
			return []string{name}
		}
		var outputs []string
		for _, b := range source.Bindings {
			if len(b.SourcePath) == 1 && b.SourcePath[0] == name && b.Transformation == plugin.QueryOutputTransformationDirect && len(b.OutputPath) == 1 {
				outputs = append(outputs, b.OutputPath[0])
			}
		}
		if len(outputs) == 0 {
			t.Fatal("missing fixture output for", name)
		}
		return outputs
	}
	t.Run("role_choices_same_execution", func(t *testing.T) { queryRoleChoiceProtection(t, model, prepare, source) })
	phoneOutputs, emailOutputs, labelOutputs := outputNames("phone"), outputNames("email"), outputNames("label")

	raw, err := original.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for owner, action := range map[string]string{"manager": "preview", "develop": "query", "service": "service_execute", "transfer": "export"} {
		for _, granted := range []bool{false, true} {
			if granted && owner != "manager" && owner != "develop" {
				continue
			}
			t.Run(owner+fmt.Sprint(granted), func(t *testing.T) {
				store := queryProtectionStore(t, owner)
				target, err := dataprotection.DataItemTargetFromCatalogPath(model, source.Path)
				if err != nil {
					t.Fatal(err)
				}
				hash, err := dataprotection.TableSchemaSnapshotHash(source.Fields)
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				actor := dataprotection.SubjectReference{}
				if granted {
					actor = dataprotection.SubjectReference{Type: "user", ID: "41"}
				}
				projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV5, ProjectionID: "hybrid-output", Revision: "00000000000000000001", ConsumerOwner: owner, Target: target, TargetEngineID: source.Path.EngineID, SourceSnapshotHash: hash, State: dataprotection.ProjectionStateActive, ValidFrom: now.Add(-time.Minute)}
				for _, definition := range []struct {
					name, typ string
					decision  dataprotection.Decision
				}{
					{"phone", "string", dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "●"}, InvalidValueEffect: dataprotection.EffectSuppress}},
					{"email", "string", dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmSM3V1, InvalidValueEffect: dataprotection.EffectSuppress}},
					{"label", "string", dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmConstantV1, Parameters: map[string]any{"value": "[保护]"}, InvalidValueEffect: dataprotection.EffectSuppress}},
					{"id", "bigint", dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmConstantV1, Parameters: map[string]any{"value": 7}, InvalidValueEffect: dataprotection.EffectSuppress}},
				} {
					component := dataprotection.Component{Key: definition.name, ValueType: definition.typ, Path: []dataprotection.PathSegment{{Name: definition.name, Container: "scalar"}}}
					component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(source.Fields, component)
					if err != nil {
						t.Fatal(err)
					}
					rule := dataprotection.Rule{Action: action, Component: component, Decision: definition.decision}
					if granted && definition.name == "phone" {
						rule.Authorizations = []dataprotection.TemporaryAuthorization{{Subject: actor, Effect: dataprotection.EffectAllow, ValidFrom: projection.ValidFrom, ValidUntil: now.Add(time.Hour)}}
					}
					projection.Rules = append(projection.Rules, rule)
				}
				applyProjection := func(previous, cursor string) {
					t.Helper()
					if err := projection.Seal(); err != nil {
						t.Fatal(err)
					}
					if err := store.ApplyBatch(ctx, 7, previous, &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: cursor, Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: cursor}, now); err != nil {
						t.Fatal(err)
					}
				}
				applyProjection("", "hybrid-1")
				rules := store.Gate(7, target, now).Projections[0].Rules
				expected, err := dataprotection.CloneQueryResult(raw)
				if err != nil {
					t.Fatal(err)
				}
				if err := dataprotection.ProtectQueryResultSource(expected, source, action, rules, dataprotection.SubjectReference{}); err != nil {
					t.Fatal(err)
				}
				prepared, err := prepare(ctx)
				if err != nil {
					t.Fatal(err)
				}
				protect, err := store.PrepareQueryProtection(ctx, 7, model, prepared, action, actor, nil, now)
				if err != nil {
					t.Fatal(err)
				}
				var actual *plugin.QueryResult
				if openSession == nil {
					actual, err = prepared.Execute(ctx)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					session, err := openSession(ctx, prepared)
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close(context.Background())
					observed, err := session.(plugin.ReadSessionEvidence).ReadSet(ctx)
					if err != nil || len(observed.Paths) != 1 {
						t.Fatal("hybrid cursor evidence", err)
					}
					actual = &plugin.QueryResult{}
					for {
						batch, err := session.ReadBatch(ctx, 2)
						if err != nil {
							t.Fatal(err)
						}
						if batch.Offset != int64(len(actual.Rows)) {
							t.Fatal("hybrid offset changed")
						}
						if actual.Fields == nil {
							actual.Fields = batch.Fields
							for _, field := range batch.Fields {
								actual.Columns = append(actual.Columns, field.Name)
							}
						}
						actual.Rows = append(actual.Rows, batch.Rows...)
						if len(batch.Rows) < 2 {
							break
						}
					}
				}
				if len(actual.Rows) != len(raw.Rows) {
					t.Fatal("native output changed cardinality")
				}
				for index, row := range actual.Rows {
					for _, output := range labelOutputs {
						if row[output] != "[保护]" {
							t.Fatal("native constant was not applied to every copy")
						}
					}
					for _, output := range emailOutputs {
						if row[output] != raw.Rows[index][output] {
							t.Fatal("Go SM3 input was altered in native SQL")
						}
					}
					for _, output := range phoneOutputs {
						if !granted && !sameJSON(row[output], expected.Rows[index][output]) {
							t.Fatal("phone copy was not masked in native SQL")
						}
						if granted && !sameJSON(row[output], raw.Rows[index][output]) {
							t.Fatal("native SQL erased an authorized original copy")
						}
					}
				}
				var snapshot *dataprotection.AuthorizedQueryResult
				if granted {
					snapshot, err = protect.CaptureAuthorized(actual, actor)
					if err != nil || snapshot == nil {
						t.Fatal("authorized capture", err)
					}
					if _, err := protect.CaptureAuthorized(actual, dataprotection.SubjectReference{Type: "user", ID: "42"}); !errors.Is(err, dataprotection.ErrDenied) {
						t.Fatal("actor could be replaced after execution")
					}
				}
				if err := protect.ApplyDefault(actual); err != nil {
					t.Fatal(err)
				}
				if !sameJSON(actual.Rows, expected.Rows) || !reflect.DeepEqual(actual.Columns, expected.Columns) {
					t.Fatal("hybrid output differs from the canonical Go result")
				}
				for index, field := range actual.Fields {
					if field.Name != expected.Fields[index].Name || field.Type != expected.Fields[index].Type {
						t.Fatal("private or changed output metadata")
					}
					if field.Name == "amount" && (field.Precision != 30 || field.Scale != 6) {
						t.Fatal("decimal metadata changed")
					}
				}
				if granted {
					read, err := store.ReadAuthorizedQueryResult(ctx, 7, snapshot, actor, action, nil)
					if err != nil {
						t.Fatal(err)
					}
					for _, output := range phoneOutputs {
						if !sameJSON(read.Rows[0][output], raw.Rows[0][output]) {
							t.Fatal("authorized original copy changed")
						}
					}
					for _, output := range emailOutputs {
						if !sameJSON(read.Rows[0][output], expected.Rows[0][output]) {
							t.Fatal("SM3 copy changed or was hashed twice")
						}
					}
					projection.Revision = "00000000000000000002"
					projection.Rules[0].Authorizations = nil
					applyProjection("hybrid-1", "hybrid-2")
					read, err = store.ReadAuthorizedQueryResult(ctx, 7, snapshot, actor, action, nil)
					if err != nil || !sameJSON(read.Rows, expected.Rows) {
						t.Fatal("revoked original did not return default protection", err)
					}
					encoded, _ := json.Marshal(actual)
					if strings.Contains(string(encoded), "fixture@example.test") {
						t.Fatal("original email entered preview")
					}
				}
				if _, err := prepared.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
					t.Fatal("hybrid plan executed twice")
				}
			})
		}
	}
	after, err := prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	unmodified, err := after.Execute(ctx)
	if err != nil || !sameJSON(unmodified, raw) {
		t.Fatal("hybrid protection changed source values", err)
	}
}

func sameJSON(left, right any) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && string(a) == string(b)
}

func queryProtectionStore(t *testing.T, owner string) *projectionstore.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:hybrid_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.Exec("ATTACH DATABASE ':memory:' AS hybrid").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(db, "hybrid", owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// QueryOutputExpressionProtection proves the existing four-owner gate rejects
// protected derived values before execution, including under a direct-field
// plaintext grant. Unprotected expressions and value-free controls still run.
// The fixture has one ordinary source with a string phone field.
func QueryOutputExpressionProtection(t *testing.T, model plugin.EngineCatalogModelSpec, prepare func(context.Context) (plugin.PreparedQuery, error), reject bool) {
	t.Helper()
	ctx := t.Context()
	original, err := prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := original.OutputLineage(ctx)
	if err != nil || len(lineage.Sources) != 1 {
		t.Fatal("expression lineage", err)
	}
	source := lineage.Sources[0]
	protectedDerived := false
	for _, binding := range source.Bindings {
		if len(binding.SourcePath) == 1 && binding.SourcePath[0] == "phone" && binding.Transformation == plugin.QueryOutputTransformationDerived {
			protectedDerived = true
		}
	}
	if (protectedDerived || source.OpaqueOutput) != reject {
		t.Fatal("expression lost or invented protected derived origins")
	}
	var expected *plugin.QueryResult
	if !reject {
		expected, err = original.Execute(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	for owner, action := range map[string]string{"manager": "preview", "develop": "query", "service": "service_execute", "transfer": "export"} {
		for _, granted := range []bool{false, true} {
			if granted && owner != "manager" && owner != "develop" {
				continue
			}
			t.Run(owner+fmt.Sprint(granted), func(t *testing.T) {
				store := queryProtectionStore(t, owner)
				target, err := dataprotection.DataItemTargetFromCatalogPath(model, source.Path)
				if err != nil {
					t.Fatal(err)
				}
				hash, err := dataprotection.TableSchemaSnapshotHash(source.Fields)
				if err != nil {
					t.Fatal(err)
				}
				component := dataprotection.Component{Key: "phone", ValueType: "string", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}}
				component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(source.Fields, component)
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				actor := dataprotection.SubjectReference{}
				rule := dataprotection.Rule{Action: action, Component: component, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "●"}, InvalidValueEffect: dataprotection.EffectSuppress}}
				if granted {
					actor = dataprotection.SubjectReference{Type: "user", ID: "41"}
					rule.Authorizations = []dataprotection.TemporaryAuthorization{{Subject: actor, Effect: dataprotection.EffectAllow, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}}
				}
				projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV5, ProjectionID: "expression-output", Revision: "00000000000000000001", ConsumerOwner: owner, Target: target, TargetEngineID: source.Path.EngineID, SourceSnapshotHash: hash, State: dataprotection.ProjectionStateActive, ValidFrom: now.Add(-time.Minute), Rules: []dataprotection.Rule{rule}}
				if err = projection.Seal(); err != nil {
					t.Fatal(err)
				}
				if err = store.ApplyBatch(ctx, 7, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: "expression-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "expression-1"}, now); err != nil {
					t.Fatal(err)
				}
				prepared, err := prepare(ctx)
				if err != nil {
					t.Fatal(err)
				}
				protect, err := store.PrepareQueryProtection(ctx, 7, model, prepared, action, actor, nil, now)
				if reject {
					if !errors.Is(err, dataprotection.ErrDenied) || protect != nil {
						t.Fatal("protected expression was not rejected at preparation", err)
					}
					return
				}
				if err != nil {
					t.Fatal("unprotected expression rejected", err)
				}
				result, err := prepared.Execute(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err = protect.Apply(result); err != nil {
					t.Fatal(err)
				}
				if !sameJSON(expected, result) {
					t.Fatal("unprotected expression changed")
				}
				if err = protect.ApplyDefault(result); err != nil || !sameJSON(expected, result) {
					t.Fatal("unprotected default preview changed", err)
				}
			})
		}
	}
}

func queryRoleChoiceProtection(t *testing.T, model plugin.EngineCatalogModelSpec, prepare func(context.Context) (plugin.PreparedQuery, error), source plugin.QueryOutputSource) {
	ctx := t.Context()
	now := time.Now().UTC()
	actor := dataprotection.SubjectReference{Type: "user", ID: "41"}
	target, err := dataprotection.DataItemTargetFromCatalogPath(model, source.Path)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := dataprotection.TableSchemaSnapshotHash(source.Fields)
	if err != nil {
		t.Fatal(err)
	}
	defaultMask := dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "●"}, InvalidValueEffect: dataprotection.EffectSuppress}
	sm3 := dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmSM3V1, Parameters: map[string]any{}, InvalidValueEffect: dataprotection.EffectSuppress}
	projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV5, ProjectionID: "role-choice", Revision: "00000000000000000001", ConsumerOwner: "develop", Target: target, TargetEngineID: source.Path.EngineID, SourceSnapshotHash: hash, State: dataprotection.ProjectionStateActive, ValidFrom: now.Add(-time.Minute)}
	selection := &dataprotection.QueryRoleSelection{RoleKeys: []string{"custom.analytics", "custom.viewer"}, ExpiresAt: now.Add(time.Minute)}
	chosenRules := []dataprotection.Rule{}
	for _, name := range []string{"phone", "email", "label"} {
		component := dataprotection.Component{Key: name, ValueType: "string", Path: []dataprotection.PathSegment{{Name: name, Container: "scalar"}}}
		component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(source.Fields, component)
		if err != nil {
			t.Fatal(err)
		}
		decision := defaultMask
		if name == "email" {
			decision = sm3
		}
		rule := dataprotection.Rule{Action: "query", Component: component, Decision: decision}
		chosen := rule
		if name != "label" {
			policy, option := uuid.NewString(), uuid.NewString()
			alternative := sm3
			if name == "email" {
				alternative = defaultMask
			}
			rule.RoleOptions = &dataprotection.RoleMaskingOptions{PolicyID: policy, AssessmentRevision: 1, PolicyRevision: 1, Options: []dataprotection.RoleMaskingOption{{ID: option, Name: "analytic mask", RoleKeys: []string{"custom.analytics"}, Decision: alternative}}}
			rule.Authorizations = []dataprotection.TemporaryAuthorization{{Subject: actor, Effect: dataprotection.EffectAllow, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}}
			selection.Choices = append(selection.Choices, dataprotection.FieldRoleChoice{Target: target, ComponentKey: name, Selection: dataprotection.RoleOptionSelection{PolicyID: policy, AssessmentRevision: 1, PolicyRevision: 1, OptionID: option}})
			chosen.Decision = alternative
		}
		chosenRules = append(chosenRules, chosen)
		projection.Rules = append(projection.Rules, rule)
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	store := queryProtectionStore(t, "develop")
	if err := store.ApplyBatch(ctx, 7, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: "roles1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "roles1"}, now); err != nil {
		t.Fatal(err)
	}
	original, err := prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := original.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	expectedLive, _ := dataprotection.CloneQueryResult(raw)
	expectedDefault, _ := dataprotection.CloneQueryResult(raw)
	if err := dataprotection.ProtectQueryResultSource(expectedLive, source, "query", chosenRules, actor); err != nil {
		t.Fatal(err)
	}
	if err := dataprotection.ProtectQueryResultSource(expectedDefault, source, "query", projection.Rules, dataprotection.SubjectReference{}); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	protect, err := store.PrepareQueryProtection(ctx, 7, model, prepared, "query", actor, selection, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := protect.CaptureAuthorized(result, actor)
	if err != nil || snapshot == nil {
		t.Fatal("capture", err)
	}
	live, cloneErr := dataprotection.CloneQueryResult(result)
	if cloneErr != nil {
		t.Fatal(cloneErr)
	}
	if err := protect.Apply(live); err != nil || !sameJSON(live, snapshot.Result) {
		t.Fatal("direct chosen Apply diverged from the captured live result", err)
	}
	// Native rewrites may change native type spelling (MySQL VARCHAR -> LONGTEXT).
	// Semantic output types and exact Provider metadata must survive capture, not be guessed from source fields.
	if !sameJSON(snapshot.Result.Rows, expectedLive.Rows) || !sameJSON(snapshot.Result.Columns, expectedLive.Columns) || !sameJSON(snapshot.Result.Fields, result.Fields) {
		t.Fatal("chosen output or Provider metadata changed during capture")
	}
	for i, field := range snapshot.Result.Fields {
		expected := expectedLive.Fields[i]
		if field.Name != expected.Name || field.Type != expected.Type || field.Precision != expected.Precision || field.Scale != expected.Scale {
			t.Fatal("chosen output lost its semantic type or precision")
		}
	}
	if err := protect.ApplyDefault(result); err != nil || (!sameJSON(result.Rows, expectedDefault.Rows) || !sameJSON(result.Columns, expectedDefault.Columns) || !sameJSON(result.Fields, snapshot.Result.Fields)) {
		t.Fatal("history is not the default protected preview", err)
	}
	for range 2 {
		read, err := store.ReadAuthorizedQueryResult(ctx, 7, snapshot, actor, "query", selection)
		if err != nil || !sameJSON(read.QueryResult, snapshot.Result) {
			t.Fatal("masked snapshot was remasked or requery required unexpectedly", err)
		}
	}
	revoked := *selection
	revoked.RoleKeys = []string{"custom.viewer"}
	if _, err := store.ReadAuthorizedQueryResult(ctx, 7, snapshot, actor, "query", &revoked); err == nil {
		t.Fatal("revoked role delivered selected result")
	}
	if _, err := store.ReadAuthorizedQueryResult(ctx, 7, snapshot, dataprotection.SubjectReference{Type: "user", ID: "42"}, "query", selection); err == nil {
		t.Fatal("other actor delivered selected result")
	}
	projection.Revision = "00000000000000000002"
	projection.Rules[0].RoleOptions.PolicyRevision = 2
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(ctx, 7, "roles1", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: "roles2", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "roles2"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAuthorizedQueryResult(ctx, 7, snapshot, actor, "query", selection); err == nil {
		t.Fatal("obsolete policy revision delivered selected result")
	}
}
