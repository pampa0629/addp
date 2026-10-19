package mysql

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/dolthub/vitess/go/vt/sqlparser"
)

func TestMySQLOutputRewriteRetainsSourceOrderingAndReordersBoundValues(t *testing.T) {
	for _, order := range []string{"contact", "2", "p.phone"} {
		compiled, err := (&MySQLPlugin{}).CompileQueryOutput("SELECT p.id,p.phone AS contact FROM business.people p WHERE p.id>? ORDER BY "+order+" LIMIT ? OFFSET ?", []any{1, 5, 2}, []plugin.QueryOutputTransform{{Column: "contact", Kind: "constant_text", Replacement: "'; SELECT secret; --"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(compiled.Args, []any{"'; SELECT secret; --", 1, 2, 5}) || strings.Contains(compiled.Query, "secret") {
			t.Fatal("algorithm values entered SQL or displaced predicate/page args")
		}
		parsed, err := sqlparser.Parse(compiled.Query)
		if err != nil {
			t.Fatal(err)
		}
		column, ok := parsed.(*sqlparser.Select).OrderBy[0].Expr.(*sqlparser.ColName)
		if !ok || column.Qualifier.Name.String() != "p" || column.Name.String() != "phone" {
			t.Fatal("order used transformed alias instead of qualified input")
		}
	}
	compiled, err := (&MySQLPlugin{}).CompileQueryOutput("SELECT id,phone FROM business.people ORDER BY phone", nil, []plugin.QueryOutputTransform{{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 2, MaskRune: "●", InvalidValue: "omit"}}, nil)
	if err != nil || !strings.Contains(compiled.Query, "business.people.phone") {
		t.Fatal("single-source column was not qualified", err)
	}
}

func TestMySQLOutputRewriteRejectsUnprovenRequests(t *testing.T) {
	for _, query := range []string{
		"SELECT * FROM s.t", "SELECT DISTINCT phone FROM s.t", "SELECT phone FROM s.t GROUP BY phone", "SELECT phone FROM s.t UNION ALL SELECT phone FROM s.t",
		"WITH x AS (SELECT phone FROM s.t) SELECT phone FROM x", "SELECT lower(phone) FROM s.t", "SELECT phone FROM s.t WHERE phone='value'", "SELECT phone FROM s.t FOR UPDATE",
		"SELECT phone FROM s.t a JOIN s.t b ON a.id=b.id", "SELECT phone,phone AS PHONE FROM s.t", "SELECT phone FROM s.t ORDER BY phone+1", "SELECT phone FROM s.t WHERE id=?",
		"SELECT phone FROM s.t WHERE id=:v1", "SELECT phone FROM s.t; SELECT phone FROM s.t", "SELECT /*+ SET_VAR(sql_mode='') */ phone FROM s.t",
		"SELECT x.phone FROM (SELECT DISTINCT phone FROM s.t) x", "SELECT x.phone FROM (SELECT phone FROM s.t GROUP BY phone) x", "SELECT phone FROM s.t WHERE id=(SELECT id FROM s.u)",
		"SELECT phone FROM s.t WHERE id=1 || id=2", "SELECT phone FROM s.t WHERE NOT id=1", "SELECT phone FROM s.t WHERE !id", "SELECT phone FROM s.t WHERE id+1>2",
	} {
		if result, err := (&MySQLPlugin{}).CompileQueryOutput(query, nil, []plugin.QueryOutputTransform{{Column: "phone", Kind: "omit"}}, nil); result != nil || !errors.Is(err, plugin.ErrQueryOutputTransformUnsupported) {
			t.Fatalf("unproven request accepted: %s", query)
		}
	}
	for _, operation := range []plugin.QueryOutputTransform{
		{Column: "phone", Kind: "sm3"}, {Column: "missing", Kind: "omit"}, {Column: "phone", Kind: "keep_prefix_suffix", MaskRune: "**", InvalidValue: "omit"}, {Column: "phone", Kind: "constant_text", Replacement: "\x00"},
	} {
		if _, err := (&MySQLPlugin{}).CompileQueryOutput("SELECT phone FROM s.t", nil, []plugin.QueryOutputTransform{operation}, nil); !errors.Is(err, plugin.ErrQueryOutputTransformUnsupported) {
			t.Fatal("unsupported operation accepted")
		}
	}
	if _, err := (&MySQLPlugin{}).CompileQueryOutput("SELECT phone FROM s.t", []any{1}, []plugin.QueryOutputTransform{{Column: "phone", Kind: "omit"}}, nil); err == nil {
		t.Fatal("unused positional argument accepted")
	}
}

func TestMySQLOutputRewritePrivateFlagCannotBeMissingOrForged(t *testing.T) {
	compiled, err := (&MySQLPlugin{}).CompileQueryOutput("SELECT phone FROM s.t", nil, []plugin.QueryOutputTransform{{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "*", InvalidValue: "omit"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const flag = "__addp_output_invalid_0"
	for _, value := range []any{nil, true, "1", int64(2)} {
		result := &plugin.QueryResult{Columns: []string{"phone", flag}, Fields: []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}, {Name: flag, Type: datatype.FieldTypeBigInt}}, Rows: []map[string]any{{"phone": nil, flag: value}}}
		if !errors.Is(compiled.Finalize(result), plugin.ErrQueryOutputTransformUnsupported) {
			t.Fatal("invalid flag accepted", value)
		}
	}
}

func TestMySQLOutputRewritePreservesDerivedServiceInputs(t *testing.T) {
	query := "SELECT contact FROM (SELECT id,phone AS contact FROM business.people WHERE id>? ORDER BY id LIMIT ?) AS addp_source WHERE id<=? ORDER BY contact LIMIT ? OFFSET ?"
	compiled, err := (&MySQLPlugin{}).CompileQueryOutput(query, []any{1, 20, 50, 5, 2}, []plugin.QueryOutputTransform{{Column: "contact", Kind: "constant_text", Replacement: "[保护]"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(compiled.Args, []any{"[保护]", 1, 20, 50, 2, 5}) {
		t.Fatalf("nested input arguments changed: %v", compiled.Args)
	}
	parsed, err := sqlparser.Parse(compiled.Query)
	if err != nil {
		t.Fatal(err)
	}
	statement := parsed.(*sqlparser.Select)
	order := statement.OrderBy[0].Expr.(*sqlparser.ColName)
	if order.Qualifier.Name.String() != "addp_source" || order.Name.String() != "contact" {
		t.Fatal("derived output sorting changed")
	}
	inner := statement.From[0].(*sqlparser.AliasedTableExpr).Expr.(*sqlparser.Subquery).Select.(*sqlparser.Select)
	if _, ok := inner.SelectExprs[1].(*sqlparser.AliasedExpr).Expr.(*sqlparser.ColName); !ok {
		t.Fatal("inner projection was transformed")
	}
}

func TestMySQLOutputRewriteExpandsOnlyProvenSingleTableWildcard(t *testing.T) {
	lineage := &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: plugin.TabularItemPath(9, plugin.EngineCatalogTermDatabase, "business", "people"), Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, NativeType: "bigint"}, {Name: "phone", Type: datatype.FieldTypeString, NativeType: "text"}}}}}
	operations := []plugin.QueryOutputTransform{{Column: "phone", Kind: "constant_text", Replacement: "private replacement"}}
	for _, query := range []string{"SELECT * FROM business.people ORDER BY 2", "SELECT p.* FROM business.people p ORDER BY 2"} {
		compiled, err := (&MySQLPlugin{}).CompileQueryOutput(query, nil, operations, lineage)
		if err != nil || compiled.ValidateExecution == nil || strings.Contains(compiled.Query, "private replacement") || strings.Contains(compiled.Query, "*") {
			t.Fatalf("missing guarded expansion: %v", err)
		}
		if len(compiled.Args) != 1 || compiled.Args[0] != "private replacement" {
			t.Fatal("lost bound algorithm value")
		}
	}
	for _, query := range []string{"SELECT *,phone FROM business.people", "SELECT p.* FROM business.people p JOIN business.people q ON p.id=q.id", "SELECT x.* FROM (SELECT id,phone FROM business.people) x", "SELECT x.phone FROM (SELECT * FROM business.people) x", "SELECT q.* FROM business.people p", "SELECT * FROM business.other"} {
		if _, err := (&MySQLPlugin{}).CompileQueryOutput(query, nil, operations, lineage); !errors.Is(err, plugin.ErrQueryOutputTransformUnsupported) {
			t.Fatalf("unproven wildcard accepted: %s (%v)", query, err)
		}
	}
	if _, err := (&MySQLPlugin{}).CompileQueryOutput("SELECT * FROM business.people", nil, operations, nil); !errors.Is(err, plugin.ErrQueryOutputTransformUnsupported) {
		t.Fatal("missing frozen facts accepted", err)
	}
}
