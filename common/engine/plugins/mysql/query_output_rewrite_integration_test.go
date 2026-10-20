package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/conformance"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	mysqlDriver "github.com/go-sql-driver/mysql"
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
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id BIGINT PRIMARY KEY,phone VARCHAR(100),amount DECIMAL(30,6),email VARCHAR(100),label VARCHAR(100)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	for index, value := range []any{"张三李四王五", "a", "", nil, "abcdef", "x.y", "🧭甲乙丙丁"} {
		if _, err := db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (?,?,?,?,?)", index+1, value, "9007199254740993.123456", "fixture@example.test", "fixture label"); err != nil {
			t.Fatal(err)
		}
	}
	allSQL := "SELECT id,phone,amount FROM " + table + " ORDER BY id"
	before, err := provider.ExecuteSQL(ctx, conn, allSQL, plugin.QueryOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("single_table_wildcard", func(t *testing.T) {
		operations := []plugin.QueryOutputTransform{
			{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "●", InvalidValue: "omit"},
			{Column: "email", Kind: "constant_text", Replacement: "[protected]"},
			{Column: "label", Kind: "omit"},
		}
		for _, projection := range []string{"*", "p.*", "p.*,p.phone AS copied_phone,p.email AS copied_email,p.label AS copied_label", "p.phone AS copied_phone,p.email AS copied_email,p.label AS copied_label,p.*", "page_wrapper_star", "page_wrapper_direct"} {
			operations := append([]plugin.QueryOutputTransform(nil), operations...)
			if strings.Contains(projection, "copied_phone") {
				for _, op := range operations[:3] {
					copy := op
					copy.Column = "copied_" + op.Column
					operations = append(operations, copy)
				}
			}
			query := "SELECT " + projection + " FROM " + table + " p WHERE id>=1 ORDER BY 2 DESC LIMIT 5 OFFSET 1"
			if strings.HasPrefix(projection, "page_wrapper_") {
				outer := "w.*"
				if projection == "page_wrapper_direct" {
					outer = "w.id,w.phone,w.amount,w.email,w.label"
				}
				query = "SELECT " + outer + " FROM (SELECT p.* FROM " + table + " p WHERE id>=1 ORDER BY 2 DESC LIMIT 5 OFFSET 1) w WHERE w.id<=6 ORDER BY w.phone DESC,w.id LIMIT 3 OFFSET 1"
			}
			expected, err := provider.ExecuteSQL(ctx, conn, query, plugin.QueryOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			rules := []dataprotection.Rule{}
			for _, op := range operations {
				decision := dataprotection.Decision{Effect: dataprotection.EffectMask, InvalidValueEffect: dataprotection.EffectSuppress}
				switch op.Kind {
				case "keep_prefix_suffix":
					decision.Algorithm = dataprotection.AlgorithmKeepPrefixSuffixV2
					decision.Parameters = map[string]any{"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "●"}
				case "constant_text":
					decision.Algorithm = dataprotection.AlgorithmConstantV1
					decision.Parameters = map[string]any{"value": op.Replacement}
				case "omit":
					decision.Effect = dataprotection.EffectSuppress
				}
				rules = append(rules, dataprotection.Rule{Action: "query", Component: dataprotection.Component{Key: op.Column, ValueType: "string", Path: []dataprotection.PathSegment{{Name: op.Column, Container: "scalar"}}}, Decision: decision})
			}
			if err := dataprotection.ProtectQueryResultSource(expected, plugin.QueryOutputSource{IdentityOutput: true, Fields: expected.Fields}, "query", rules, dataprotection.SubjectReference{}); err != nil {
				t.Fatal(err)
			}

			for _, mode := range []string{"execute"} {
				t.Run(projection+"/"+mode, func(t *testing.T) {
					prepared, err := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}})
					if err != nil {
						t.Fatal(err)
					}
					if err := prepared.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, operations); err != nil {
						t.Fatal(err)
					}
					var actual *plugin.QueryResult
					actual, err = prepared.Execute(ctx)

					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(actual.Columns, expected.Columns) || !reflect.DeepEqual(actual.Rows, expected.Rows) {
						t.Fatal("wildcard differs from canonical values, missing fields, sorting or pagination")
					}
					for i, field := range actual.Fields {
						if field.Name != "phone" && field.Name != "email" && field.Name != "copied_phone" && field.Name != "copied_email" && !reflect.DeepEqual(field, expected.Fields[i]) {
							t.Fatal("wildcard changed unmodified native metadata")
						}
					}
					if _, err := prepared.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
						t.Fatal("wildcard executed twice", err)
					}
				})
			}
		}
	})

	t.Run("wildcard_preflight_holds_metadata_until_transaction_end", func(t *testing.T) {
		query := "SELECT * FROM " + table
		prepared, err := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}})
		if err != nil {
			t.Fatal(err)
		}
		lineage, err := prepared.OutputLineage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := provider.CompileQueryOutput(query, nil, []plugin.QueryOutputTransform{{Column: "phone", Kind: "constant_text", Replacement: "[protected]"}}, lineage)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := compiled.ValidateExecution(ctx, tx); err != nil {
			t.Fatal(err)
		}
		ddl, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer ddl.Close()
		if _, err := ddl.ExecContext(ctx, "SET SESSION lock_wait_timeout=1"); err != nil {
			t.Fatal(err)
		}
		defer ddl.ExecContext(ctx, "SET SESSION lock_wait_timeout=DEFAULT")
		_, lockErr := ddl.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN blocked_secret text")
		var native *mysqlDriver.MySQLError
		if !errors.As(lockErr, &native) || native.Number != 1205 {
			t.Fatal("preflight did not retain metadata lock", lockErr)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if _, err := ddl.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN blocked_secret text"); err != nil {
			t.Fatal("transaction end did not release metadata lock", err)
		}
		if _, err := ddl.ExecContext(ctx, "ALTER TABLE "+table+" DROP COLUMN blocked_secret"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("wildcard_refuses_invisible_columns", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN invisible_secret VARCHAR(40) INVISIBLE"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := db.ExecContext(ctx, "ALTER TABLE "+table+" DROP COLUMN invisible_secret"); err != nil {
				t.Error(err)
			}
		}()
		prepared, err := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: "SELECT * FROM " + table + " WHERE id<0", Options: plugin.QueryOptions{ReadOnly: true}})
		if err != nil {
			t.Fatal(err)
		}
		if err := prepared.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, []plugin.QueryOutputTransform{{Column: "phone", Kind: "constant_text", Replacement: "[protected]"}}); err != nil {
			t.Fatal(err)
		}
		if result, err := prepared.Execute(ctx); result != nil || !errors.Is(err, plugin.ErrQueryOutputTransformUnsupported) {
			t.Fatal("invisible column changed wildcard semantics", err)
		}
	})
	t.Run("wildcard_rejects_schema_drift_even_without_rows", func(t *testing.T) {
		for _, change := range []struct{ name, apply, restore string }{{"add", "ADD COLUMN extra_secret text", "DROP COLUMN extra_secret"},
			{"rename", "RENAME COLUMN email TO renamed_email", "RENAME COLUMN renamed_email TO email"},
			{"type", "MODIFY COLUMN email VARCHAR(120)", "MODIFY COLUMN email VARCHAR(100)"},
			{"nullable", "MODIFY COLUMN email VARCHAR(100) NOT NULL", "MODIFY COLUMN email VARCHAR(100) NULL"}} {
			for _, mode := range []string{"execute"} {
				t.Run(change.name+"/"+mode, func(t *testing.T) {
					prepared, err := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: "SELECT * FROM " + table + " WHERE id<0", Options: plugin.QueryOptions{ReadOnly: true}})
					if err != nil {
						t.Fatal(err)
					}
					if err := prepared.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, []plugin.QueryOutputTransform{{Column: "phone", Kind: "constant_text", Replacement: "[protected]"}}); err != nil {
						t.Fatal(err)
					}
					if _, err := db.ExecContext(ctx, "ALTER TABLE "+table+" "+change.apply); err != nil {
						t.Fatal(err)
					}
					result, e := prepared.Execute(ctx)
					err = e
					if result != nil {
						t.Fatal("changed schema returned result")
					}

					if _, restoreErr := db.ExecContext(ctx, "ALTER TABLE "+table+" "+change.restore); restoreErr != nil {
						t.Fatal(restoreErr)
					}
					if !errors.Is(err, plugin.ErrQueryOutputTransformUnsupported) {
						t.Fatal("changed wildcard schema was not refused", err)
					}
				})
			}
		}
	})
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

	t.Run("shared expression protection", func(t *testing.T) {
		for _, test := range []struct {
			name, sql string
			reject    bool
		}{
			{"substring", "SELECT substring(phone,1,100) AS value FROM " + table, true},
			{"coalesce", "SELECT coalesce(phone,NULL) AS value FROM " + table, true},
			{"case", "SELECT CASE WHEN phone IS NULL THEN NULL ELSE phone END AS value FROM " + table, true},
			{"cast", "SELECT CAST(phone AS CHAR) AS value FROM " + table, true},
			{"arithmetic", "SELECT phone+0 AS value FROM " + table, true},
			{"aggregate", "SELECT count(phone) AS value FROM " + table, true},
			{"derived_alias", "SELECT w.value FROM (SELECT substring(phone,1,100) AS value FROM " + table + ") w", true},
			{"cte_alias", "WITH w AS (SELECT coalesce(phone,NULL) AS value FROM " + table + ") SELECT value FROM w", true},
			{"union", "SELECT phone AS value FROM " + table + " UNION ALL SELECT label AS other FROM " + table, true},
			{"scalar_subquery", "SELECT (SELECT phone FROM " + table + " LIMIT 1) AS value FROM " + table, true},
			{"default_expression", "SELECT DEFAULT(phone) AS value FROM " + table, true},
			{"unprotected", "SELECT id+1 AS value FROM " + table + " ORDER BY id", false},
			{"constant", "SELECT 1 AS value FROM " + table, false},
			{"count_star", "SELECT count(*) AS value FROM " + table, false},
			{"row_filter_only", "SELECT id FROM " + table + " WHERE phone IS NOT NULL ORDER BY phone,id", false},
		} {
			t.Run(test.name, func(t *testing.T) {
				conformance.QueryOutputExpressionProtection(t, provider.EngineCatalogModel(), func(ctx context.Context) (plugin.PreparedQuery, error) {
					return provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: test.sql, Options: plugin.QueryOptions{ReadOnly: true}})
				}, test.reject)
			})
		}
	})

	t.Run("shared protection", func(t *testing.T) {
		conformance.QueryOutputProtection(t, provider.EngineCatalogModel(), func(ctx context.Context) (plugin.PreparedQuery, error) {
			return provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: "SELECT id,phone AS contact,email AS mail,label AS public_label,amount FROM " + table + " ORDER BY id", Options: plugin.QueryOptions{ReadOnly: true}})
		}, nil)
	})
	t.Run("shared protection wildcard", func(t *testing.T) {
		conformance.QueryOutputProtection(t, provider.EngineCatalogModel(), func(ctx context.Context) (plugin.PreparedQuery, error) {
			return provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: "SELECT p.* FROM " + table + " p ORDER BY id", Options: plugin.QueryOptions{ReadOnly: true}})
		}, nil)
	})
	t.Run("shared protection mixed wildcard copies", func(t *testing.T) {
		conformance.QueryOutputProtection(t, provider.EngineCatalogModel(), func(ctx context.Context) (plugin.PreparedQuery, error) {
			return provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: "SELECT p.phone AS copied_phone,p.*,p.email AS copied_email,p.label AS copied_label,p.id AS copied_id FROM " + table + " p ORDER BY copied_phone,id", Options: plugin.QueryOptions{ReadOnly: true}})
		}, nil)
	})
	t.Run("shared protection through wildcard page", func(t *testing.T) {
		conformance.QueryOutputProtection(t, provider.EngineCatalogModel(), func(ctx context.Context) (plugin.PreparedQuery, error) {
			query := "SELECT w.* FROM (SELECT p.phone AS copied_phone,p.*,p.email AS copied_email,p.label AS copied_label,p.id AS copied_id FROM " + table + " p WHERE p.id>=? ORDER BY p.phone DESC,p.id LIMIT ?) w WHERE w.id<=? ORDER BY w.copied_phone,w.id LIMIT ? OFFSET ?"
			return provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true, Args: []any{1, 6, 6, 4, 1}}})
		}, nil)
	})
	t.Run("shared protection through service wrapper", func(t *testing.T) {
		conformance.QueryOutputProtection(t, provider.EngineCatalogModel(), func(ctx context.Context) (plugin.PreparedQuery, error) {
			query := "SELECT addp_source.id,addp_source.contact,addp_source.mail,addp_source.public_label,addp_source.amount FROM (SELECT id,phone AS contact,email AS mail,label AS public_label,amount FROM " + table + " WHERE id>=? ORDER BY id LIMIT ?) AS addp_source WHERE addp_source.id<=? ORDER BY addp_source.id LIMIT ? OFFSET ?"
			return provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 93, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true, Args: []any{1, 7, 7, 7, 0}}})
		}, nil)
	})
	after, err := provider.ExecuteSQL(ctx, conn, allSQL, plugin.QueryOptions{ReadOnly: true})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rewrite altered source values or metadata", err)
	}
}
