package postgresql

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
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
	createPostgresPrepareBaseTable(t, ctx, db, "common_pg_it", table, `id bigint PRIMARY KEY, phone text, amount numeric(30,6)`)
	for index, value := range []any{"张三李四王五", "a", "", nil, "abcdef", "x.y", "🧭甲乙丙丁"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO `+qualified+` VALUES ($1,$2,$3)`, index+1, value, "9007199254740993.123456"); err != nil {
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
		for _, operation := range []postgresOutputTransform{
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
				plan, err := rewritePostgresQueryOutput(query, originalArgs, []postgresOutputTransform{operation})
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
			})
		}
	}
	denied, err := rewritePostgresQueryOutput(`SELECT phone FROM `+qualified+` ORDER BY id`, nil,
		[]postgresOutputTransform{{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "*", InvalidValue: "reject"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := pg.ExecuteSQL(ctx, conn, denied.query, plugin.QueryOptions{ReadOnly: true, Args: denied.args})
	if err != nil || denied.finish(result) == nil {
		t.Fatal("short invalid values must reject the whole result")
	}
	after, err := pg.ExecuteSQL(ctx, conn, allSQL, plugin.QueryOptions{ReadOnly: true})
	if err != nil || !reflect.DeepEqual(before.Columns, after.Columns) || !reflect.DeepEqual(before.Rows, after.Rows) {
		t.Fatal("rewrite changed source values")
	}
}
