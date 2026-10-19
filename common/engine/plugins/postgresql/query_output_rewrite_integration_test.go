package postgresql

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/conformance"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationResolvePostgresQueryOutputRewrite(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	table := fmt.Sprintf("output_rewrite_%d", time.Now().UnixNano())
	qualified := `"common_pg_it"."` + table + `"`
	t.Cleanup(func() {
		defer db.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, `DROP TABLE IF EXISTS `+qualified); err != nil {
			t.Errorf("rewrite fixture cleanup failed: %v", err)
		}
		var exists bool
		if err := db.QueryRowContext(cleanup, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_tables WHERE schemaname=$1 AND tablename=$2)`, "common_pg_it", table).Scan(&exists); err != nil || exists {
			t.Errorf("rewrite fixture residual check failed: exists=%v error=%v", exists, err)
		}
	})
	createPostgresPrepareBaseTable(t, ctx, db, "common_pg_it", table, `id bigint PRIMARY KEY, phone text, amount numeric(30,6), email text, label text`)
	for index, value := range []any{"张三李四王五", "a", "", nil, "abcdef", "x.y", "🧭甲乙丙丁"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO `+qualified+` VALUES ($1,$2,$3,$4,$5)`, index+1, value, "9007199254740993.123456", "fixture@example.test", "fixture label"); err != nil {
			t.Fatal(err)
		}
	}
	allSQL := `SELECT id, phone, amount FROM ` + qualified + ` ORDER BY id`
	before, err := pg.ExecuteSQL(ctx, conn, allSQL, plugin.QueryOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"contact DESC NULLS LAST", "2 DESC NULLS FIRST", "p.phone ASC NULLS LAST", "p.id"} {
		query := `SELECT p.id, p.phone AS contact, p.amount FROM ` + qualified + ` p JOIN ` + qualified + ` q ON p.id=q.id WHERE p.id >= $1 AND q.id <= $2 ORDER BY ` + order + ` LIMIT 5 OFFSET 1`
		originalArgs := []any{1, 7}
		prepared, err := pg.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true, Args: originalArgs}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := prepared.ReadSet(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := prepared.OutputLineage(ctx); err != nil {
			t.Fatal(err)
		}
		for _, operation := range []plugin.QueryOutputTransform{
			{Column: "contact", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "●", InvalidValue: "omit"},
			{Column: "contact", Kind: "keep_prefix_suffix", Prefix: 0, Suffix: 0, MaskRune: "#", InvalidValue: "omit"},
			{Column: "contact", Kind: "constant_text", Replacement: `'; SELECT secret; --`},
			{Column: "contact", Kind: "omit"},
		} {
			t.Run(order+"/"+operation.Kind+fmt.Sprint(operation.Prefix), func(t *testing.T) {
				expected, err := pg.ExecuteSQL(ctx, conn, query, plugin.QueryOptions{ReadOnly: true, Args: originalArgs})
				if err != nil {
					t.Fatal(err)
				}
				decision := dataprotection.Decision{Effect: dataprotection.EffectMask, InvalidValueEffect: dataprotection.EffectSuppress}
				switch operation.Kind {
				case "keep_prefix_suffix":
					decision.Algorithm = dataprotection.AlgorithmKeepPrefixSuffixV2
					decision.Parameters = map[string]any{"prefix_runes": operation.Prefix, "suffix_runes": operation.Suffix, "mask_rune": operation.MaskRune}
				case "constant_text":
					decision.Algorithm = dataprotection.AlgorithmConstantV1
					decision.Parameters = map[string]any{"value": operation.Replacement}
				case "omit":
					decision.Effect = dataprotection.EffectSuppress
				}
				rules := []dataprotection.Rule{{Action: "query", Component: dataprotection.Component{Key: "contact", ValueType: "string", Path: []dataprotection.PathSegment{{Name: "contact", Container: "scalar"}}}, Decision: decision}}
				source := plugin.QueryOutputSource{IdentityOutput: true, Fields: []datatype.FieldInfo{{Name: "contact", Type: datatype.FieldTypeString}}}
				if err := dataprotection.ProtectQueryResultSource(expected, source, "query", rules, dataprotection.SubjectReference{}); err != nil {
					t.Fatal(err)
				}
				plan, err := rewritePostgresQueryOutput(query, originalArgs, []plugin.QueryOutputTransform{operation})
				if err != nil {
					t.Fatal(err)
				}
				actual, err := pg.ExecuteSQL(ctx, conn, plan.query, plugin.QueryOptions{ReadOnly: true, Args: plan.args})
				if err != nil {
					t.Fatal(err)
				}
				for _, invalid := range plan.invalid {
					for _, row := range actual.Rows {
						if row[invalid.flag] == true && row[invalid.column] != nil {
							t.Fatal("native SQL returned plaintext for an invalid value")
						}
					}
				}
				if err := plan.finish(actual); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual.Columns, expected.Columns) || !reflect.DeepEqual(actual.Fields, expected.Fields) || !reflect.DeepEqual(actual.Rows, expected.Rows) {
					t.Fatal("native rewrite differs from canonical protection in values, missing fields, order or pagination")
				}
				for _, mode := range []string{"execute", "stream"} {
					t.Run(mode, func(t *testing.T) {
						request := plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true, Args: originalArgs}}
						bound, err := pg.PrepareQuery(ctx, conn, request)
						if err != nil {
							t.Fatal(err)
						}
						readSet, err := bound.ReadSet(ctx)
						if err != nil {
							t.Fatal(err)
						}
						lineage, err := bound.OutputLineage(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if err := bound.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, []plugin.QueryOutputTransform{operation}); err != nil {
							t.Fatal(err)
						}
						if current, err := bound.ReadSet(ctx); err != nil || !reflect.DeepEqual(current, readSet) {
							t.Fatal("rewrite changed logical read set", err)
						}
						if current, err := bound.OutputLineage(ctx); err != nil || !reflect.DeepEqual(current, lineage) {
							t.Fatal("rewrite changed logical output lineage", err)
						}
						var output *plugin.QueryResult
						if mode == "execute" {
							output, err = bound.Execute(ctx)
							if err != nil {
								t.Fatal(err)
							}
						} else {
							session, err := pg.OpenQueryReadSession(ctx, bound)
							if err != nil {
								t.Fatal(err)
							}
							defer session.Close(context.Background())
							if observed, err := session.(plugin.ReadSessionEvidence).ReadSet(ctx); err != nil || !reflect.DeepEqual(observed, readSet) {
								t.Fatal("physical cursor lost actual source evidence", err)
							}
							output = &plugin.QueryResult{}
							for {
								batch, err := session.ReadBatch(ctx, 2)
								if err != nil {
									t.Fatal(err)
								}
								if batch.Offset != int64(len(output.Rows)) {
									t.Fatal("rewrite changed batch offset")
								}
								if output.Fields == nil {
									output.Fields = batch.Fields
									for _, field := range batch.Fields {
										output.Columns = append(output.Columns, field.Name)
									}
								} else if !reflect.DeepEqual(output.Fields, batch.Fields) {
									t.Fatal("batch output metadata changed")
								}
								output.Rows = append(output.Rows, batch.Rows...)
								if len(batch.Rows) < 2 {
									break
								}
							}
						}
						if !reflect.DeepEqual(output.Columns, expected.Columns) || !reflect.DeepEqual(output.Fields, expected.Fields) || !reflect.DeepEqual(output.Rows, expected.Rows) {
							t.Fatalf("%s prepared output differs from canonical protection", mode)
						}
						if _, err := bound.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
							t.Fatal("rewritten plan executed twice", err)
						}
					})
				}
			})
		}
	}
	denied, err := rewritePostgresQueryOutput(`SELECT phone FROM `+qualified+` ORDER BY id`, nil,
		[]plugin.QueryOutputTransform{{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "*", InvalidValue: "reject"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := pg.ExecuteSQL(ctx, conn, denied.query, plugin.QueryOptions{ReadOnly: true, Args: denied.args})
	if err != nil || denied.finish(result) == nil {
		t.Fatal("short invalid values must reject the whole result")
	}
	for _, mode := range []string{"execute", "stream"} {
		bound, err := pg.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: `SELECT phone FROM ` + qualified + ` ORDER BY id`, Options: plugin.QueryOptions{ReadOnly: true}})
		if err != nil {
			t.Fatal(err)
		}
		if err := bound.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, []plugin.QueryOutputTransform{{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "*", InvalidValue: "reject"}}); err != nil {
			t.Fatal(err)
		}
		if mode == "execute" {
			if output, err := bound.Execute(ctx); err == nil || output != nil {
				t.Fatal("reject returned a partial ordinary result")
			}
		} else {
			session, err := pg.OpenQueryReadSession(ctx, bound)
			if err != nil {
				t.Fatal(err)
			}
			if batch, err := session.ReadBatch(ctx, 2); err == nil || batch != nil {
				t.Fatal("reject returned a partial batch")
			}
			if _, err := session.ReadBatch(ctx, 2); err == nil {
				t.Fatal("rejected session remained readable")
			}
			if err := session.Close(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, mode := range []string{"execute", "stream"} {
		t.Run("shared protection "+mode, func(t *testing.T) {
			var session func(context.Context, plugin.PreparedQuery) (plugin.QueryReadSession, error)
			if mode == "stream" {
				session = pg.OpenQueryReadSession
			}
			conformance.QueryOutputProtection(t, pg.EngineCatalogModel(), func(ctx context.Context) (plugin.PreparedQuery, error) {
				return pg.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: `SELECT id,phone AS contact,email AS mail,label AS public_label,amount FROM ` + qualified + ` ORDER BY id`, Options: plugin.QueryOptions{ReadOnly: true}})
			}, session)
		})
	}
	after, err := pg.ExecuteSQL(ctx, conn, allSQL, plugin.QueryOptions{ReadOnly: true})
	if err != nil || !reflect.DeepEqual(before.Columns, after.Columns) || !reflect.DeepEqual(before.Rows, after.Rows) {
		t.Fatal("rewrite changed source values")
	}
}
