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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// QueryOutputProtection exercises an actual native PreparedQuery with the
// formal shared store. prepare selects id, phone AS contact, email AS mail,
// label AS public_label, amount from one owned fixture table.
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
				db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:hybrid_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
				if err != nil {
					t.Fatal(err)
				}
				sqlDB, err := db.DB()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = sqlDB.Close() })
				if err := db.Exec("ATTACH DATABASE ':memory:' AS hybrid").Error; err != nil {
					t.Fatal(err)
				}
				store, err := projectionstore.Migrate(db, "hybrid", owner, nil)
				if err != nil {
					t.Fatal(err)
				}
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
				projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV4, ProjectionID: "hybrid-output", Revision: "00000000000000000001", ConsumerOwner: owner, Target: target, TargetEngineID: source.Path.EngineID, SourceSnapshotHash: hash, State: dataprotection.ProjectionStateActive, ValidFrom: now.Add(-time.Minute)}
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
				protect, err := store.PrepareQueryProtection(ctx, 7, model, prepared, action, actor, now)
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
					if row["public_label"] != "[保护]" || row["mail"] != raw.Rows[index]["mail"] {
						t.Fatal("native constant or Go SM3 partition was not respected")
					}
					if !granted && !sameJSON(row["contact"], expected.Rows[index]["contact"]) {
						t.Fatal("phone was not masked in native SQL")
					}
					if granted && !sameJSON(row["contact"], raw.Rows[index]["contact"]) {
						t.Fatal("native SQL erased an authorized original")
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
					read, err := store.ReadAuthorizedQueryResult(ctx, 7, snapshot, actor, action)
					if err != nil {
						t.Fatal(err)
					}
					if !sameJSON(read.Rows[0]["contact"], raw.Rows[0]["contact"]) || !sameJSON(read.Rows[0]["mail"], expected.Rows[0]["mail"]) {
						t.Fatal("original capture or single SM3 changed")
					}
					projection.Revision = "00000000000000000002"
					projection.Rules[0].Authorizations = nil
					applyProjection("hybrid-1", "hybrid-2")
					read, err = store.ReadAuthorizedQueryResult(ctx, 7, snapshot, actor, action)
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
