package mysql

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationMySQLOutputRewrite(t *testing.T) {
	db, provider, server, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	conn := plugin.ConnectionInfo{}
	for key, value := range server {
		conn[key] = value
	}
	conn["database"] = database
	table := mysqlDialect().QualifiedTable(database, "output_rewrite")
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id BIGINT PRIMARY KEY,phone VARCHAR(100),amount DECIMAL(30,6)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	for index, value := range []any{"张三李四王五", "a", "", nil, "abcdef", "x.y", "🧭甲乙丙丁"} {
		if _, err := db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (?,?,?)", index+1, value, "9007199254740993.123456"); err != nil {
			t.Fatal(err)
		}
	}
	allSQL := "SELECT id,phone,amount FROM " + table + " ORDER BY id"
	before, err := provider.ExecuteSQL(ctx, conn, allSQL, plugin.QueryOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"contact DESC", "2 DESC", "p.phone ASC", "p.id"} {
		query := "SELECT p.id,p.phone AS contact,p.amount FROM " + table + " p JOIN " + table + " q ON p.id=q.id WHERE p.id>=? AND q.id<=? ORDER BY " + order + " LIMIT ? OFFSET ?"
		args := []any{1, 7, 5, 1}
		for _, operation := range []plugin.QueryOutputTransform{
			{Column: "contact", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "●", InvalidValue: "omit"},
			{Column: "contact", Kind: "keep_prefix_suffix", MaskRune: "#", InvalidValue: "omit"},
			{Column: "contact", Kind: "constant_text", Replacement: "'; SELECT secret; --\\甲"},
			{Column: "contact", Kind: "omit"},
		} {
			t.Run(order+"/"+operation.Kind+fmt.Sprint(operation.Prefix), func(t *testing.T) {
				expected, err := provider.ExecuteSQL(ctx, conn, query, plugin.QueryOptions{ReadOnly: true, Args: args})
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
				prepared, err := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true, Args: args}})
				if err != nil {
					t.Fatal(err)
				}
				originalReadSet, err := prepared.ReadSet(ctx)
				if err != nil {
					t.Fatal(err)
				}
				originalLineage, err := prepared.OutputLineage(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := prepared.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, []plugin.QueryOutputTransform{operation}); err != nil {
					t.Fatal(err)
				}
				if current, err := prepared.ReadSet(ctx); err != nil || !reflect.DeepEqual(current, originalReadSet) {
					t.Fatal("physical output changed logical read set", err)
				}
				if current, err := prepared.OutputLineage(ctx); err != nil || !reflect.DeepEqual(current, originalLineage) {
					t.Fatal("physical output changed logical lineage", err)
				}
				actual, err := prepared.Execute(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual.Columns, expected.Columns) || !reflect.DeepEqual(actual.Rows, expected.Rows) {
					t.Fatal("MySQL output differs in values, nulls, missing fields, sorting or pagination")
				}
				if len(actual.Fields) != len(expected.Fields) {
					t.Fatal("private metadata leaked")
				}
				for index, field := range actual.Fields {
					if field.Name != expected.Fields[index].Name || field.Type != expected.Fields[index].Type {
						t.Fatalf("output type changed: %+v", field)
					}
					if field.Name != "contact" && !reflect.DeepEqual(field, expected.Fields[index]) {
						t.Fatal("unmodified native metadata changed")
					}
				}
				if _, err := prepared.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
					t.Fatal("rewritten plan executed twice", err)
				}
			})
		}
	}
	for _, effect := range []string{"omit", "reject"} {
		prepared, err := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: "SELECT phone FROM " + table + " ORDER BY id", Options: plugin.QueryOptions{ReadOnly: true}})
		if err != nil {
			t.Fatal(err)
		}
		if err := prepared.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, []plugin.QueryOutputTransform{{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "*", InvalidValue: effect}}); err != nil {
			t.Fatal(err)
		}
		result, err := prepared.Execute(ctx)
		if effect == "reject" {
			if err == nil || result != nil {
				t.Fatal("invalid values returned partial result")
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	after, err := provider.ExecuteSQL(ctx, conn, allSQL, plugin.QueryOptions{ReadOnly: true})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rewrite altered source values or metadata", err)
	}
}
