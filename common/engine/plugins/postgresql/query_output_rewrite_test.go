package postgresql

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pgquery "github.com/pganalyze/pg_query_go/v6"
)

func TestPostgresOutputRewriteDoesNotCertifyProtocolCompatibleEngines(t *testing.T) {
	provider := NewProtocolCompatiblePlugin(ProtocolIdentity{EngineType: "separate-engine"})
	if result, err := provider.CompileQueryOutput("SELECT phone FROM public.people", nil, []plugin.QueryOutputTransform{{Column: "phone", Kind: "omit"}}); result != nil || !errors.Is(err, plugin.ErrQueryOutputTransformUnsupported) {
		t.Fatal("protocol reuse silently enabled unverified native rewriting", err)
	}
}

func TestPostgresOutputRewriteRetainsOriginalOrderingAndBoundValues(t *testing.T) {
	for _, order := range []string{"contact", "2", "phone"} {
		query := `SELECT id, phone AS contact FROM public.people WHERE id >= $1 ORDER BY ` + order + ` DESC NULLS LAST LIMIT 5 OFFSET 2`
		plan, err := rewritePostgresQueryOutput(query, []any{3}, []plugin.QueryOutputTransform{{Column: "contact", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 2, MaskRune: "●", InvalidValue: "omit"}})
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := pgquery.Parse(plan.query)
		if err != nil {
			t.Fatal(err)
		}
		stmt := parsed.GetStmts()[0].GetStmt().GetSelectStmt()
		if order != "phone" && stmt.GetSortClause()[0].GetSortBy().GetNode().GetCoalesceExpr() == nil {
			t.Fatal("output alias/ordinal must sort the original input expression")
		}
		if !reflect.DeepEqual(plan.args, []any{3, "●"}) || strings.Contains(plan.query, "●") ||
			!strings.Contains(plan.query, "WHERE id >= $1") || !strings.Contains(plan.query, "LIMIT 5 OFFSET 2") {
			t.Fatal("rewrite changed original predicates/pagination or inlined a configured value")
		}
	}
}

func TestPostgresOutputRewriteQuotesIdentifiersAndReplacement(t *testing.T) {
	column := `c"; --`
	value := `'; SELECT private_data; --`
	plan, err := rewritePostgresQueryOutput(`SELECT "phone" AS "c""; --" FROM "public"."people"`, nil,
		[]plugin.QueryOutputTransform{{Column: column, Kind: "constant_text", Replacement: value}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.query, value) || !reflect.DeepEqual(plan.args, []any{value}) || !reflect.DeepEqual(plan.columns, []string{column}) {
		t.Fatal("identifier or parameter identity was lost")
	}
}

func TestPostgresOutputRewriteNeverCapturesAnUnboundOriginalParameter(t *testing.T) {
	for _, fixture := range []struct {
		query string
		args  []any
	}{
		{query: `SELECT phone FROM people WHERE id=$1`},
		{query: `SELECT phone FROM people WHERE id=$2`, args: []any{7}},
		{query: `SELECT phone FROM people WHERE id=$2`, args: []any{7, 8}},
		{query: `SELECT phone FROM people`, args: []any{7}},
	} {
		if _, err := rewritePostgresQueryOutput(fixture.query, fixture.args, []plugin.QueryOutputTransform{{Column: "phone", Kind: "constant_text", Replacement: "hidden"}}); !errors.Is(err, errPostgresOutputRewriteUnsupported) {
			t.Fatal("rewrite captured a missing original parameter or made an originally invalid binding valid")
		}
	}
}

func TestPostgresOutputRewriteRejectsUnprovedSemantics(t *testing.T) {
	for _, query := range []string{
		`SELECT DISTINCT phone FROM people`, `SELECT phone FROM people GROUP BY phone`,
		`SELECT phone FROM people UNION ALL SELECT phone FROM others`, `SELECT * FROM people`,
		`SELECT phone, phone FROM people`, `SELECT upper(phone) AS phone FROM people`,
		`SELECT phone INTO copy FROM people`, `SELECT phone FROM people FOR UPDATE`,
		`WITH x AS (SELECT phone FROM people) SELECT phone FROM x`,
		`SELECT phone FROM people; SELECT phone FROM others`,
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := rewritePostgresQueryOutput(query, nil, []plugin.QueryOutputTransform{{Column: "phone", Kind: "omit"}}); !errors.Is(err, errPostgresOutputRewriteUnsupported) {
				t.Fatalf("unproved query must refuse: %v", err)
			}
		})
	}
	for _, operation := range []plugin.QueryOutputTransform{
		{Column: "phone", Kind: "sm3"}, {Column: "missing", Kind: "omit"},
		{Column: "phone", Kind: "keep_prefix_suffix", Prefix: -1, MaskRune: "*", InvalidValue: "omit"},
		{Column: "phone", Kind: "keep_prefix_suffix", MaskRune: "ab", InvalidValue: "omit"},
		{Column: "phone", Kind: "keep_prefix_suffix", MaskRune: "*", InvalidValue: "allow"},
		{Column: "phone", Kind: "constant_text", Replacement: "\x00"},
	} {
		if _, err := rewritePostgresQueryOutput(`SELECT phone FROM people`, nil, []plugin.QueryOutputTransform{operation}); !errors.Is(err, errPostgresOutputRewriteUnsupported) {
			t.Fatalf("unproved operation must refuse: %v", err)
		}
	}
}

func TestPostgresOutputRewritePrivateFlagsAndInvalidValues(t *testing.T) {
	operation := plugin.QueryOutputTransform{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "#", InvalidValue: "omit"}
	plan, err := rewritePostgresQueryOutput(`SELECT id AS __addp_output_invalid_0, phone FROM people`, nil, []plugin.QueryOutputTransform{operation})
	if err != nil {
		t.Fatal(err)
	}
	flag := plan.invalid[0].flag
	if flag == "__addp_output_invalid_0" {
		t.Fatal("private flag must not shadow an actual output")
	}
	fields := []datatype.FieldInfo{{Name: "__addp_output_invalid_0", Type: datatype.FieldTypeBigInt}, {Name: "phone", Type: datatype.FieldTypeString}, {Name: flag, Type: datatype.FieldTypeBool}}
	result := &plugin.QueryResult{Columns: []string{"__addp_output_invalid_0", "phone", flag}, Fields: fields, Rows: []map[string]any{
		{"__addp_output_invalid_0": 1, "phone": nil, flag: true},
		{"__addp_output_invalid_0": 2, "phone": nil, flag: false},
		{"__addp_output_invalid_0": 3, "phone": "a#c", flag: false},
	}}
	if err := plan.finish(result); err != nil {
		t.Fatal(err)
	}
	if _, exists := result.Rows[0]["phone"]; exists || len(result.Columns) != 2 || len(result.Fields) != 2 || len(result.Rows[0]) != 1 || result.Rows[1]["phone"] != nil || result.Rows[2]["phone"] != "a#c" {
		t.Fatal("invalid values, NULL or private-column stripping changed")
	}
	plan.invalid[0].reject = true
	for _, invalid := range []any{true, nil, "true"} {
		result := &plugin.QueryResult{Columns: []string{"__addp_output_invalid_0", "phone", flag}, Fields: fields, Rows: []map[string]any{{flag: invalid}}}
		if !errors.Is(plan.finish(result), errPostgresOutputRewriteUnsupported) {
			t.Fatal("invalid or missing evidence must reject")
		}
	}
	if !errors.Is(plan.finish(&plugin.QueryResult{Columns: []string{"phone"}}), errPostgresOutputRewriteUnsupported) {
		t.Fatal("changed output structure must reject")
	}
}

func TestPostgresOutputRewriteKeepsPrivateFlagsUniqueAcrossFieldsAndOriginalOutputs(t *testing.T) {
	plan, err := rewritePostgresQueryOutput(`SELECT id AS __addp_output_invalid_0, phone, email FROM people`, nil, []plugin.QueryOutputTransform{
		{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "#", InvalidValue: "omit"},
		{Column: "email", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "*", InvalidValue: "omit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.invalid[0].flag == plan.invalid[1].flag || plan.invalid[0].flag == "__addp_output_invalid_0" || plan.invalid[1].flag == "__addp_output_invalid_0" {
		t.Fatal("private flags collide with another field or an original output")
	}
	result := &plugin.QueryResult{
		Columns: []string{"__addp_output_invalid_0", "phone", "email", plan.invalid[0].flag, plan.invalid[1].flag},
		Fields: []datatype.FieldInfo{
			{Name: "__addp_output_invalid_0", Type: datatype.FieldTypeBigInt}, {Name: "phone", Type: datatype.FieldTypeString}, {Name: "email", Type: datatype.FieldTypeString},
			{Name: plan.invalid[0].flag, Type: datatype.FieldTypeBool}, {Name: plan.invalid[1].flag, Type: datatype.FieldTypeBool},
		},
		Rows: []map[string]any{{"__addp_output_invalid_0": 8, "phone": nil, "email": "a*b", plan.invalid[0].flag: true, plan.invalid[1].flag: false}},
	}
	if err := plan.finish(result); err != nil || !reflect.DeepEqual(result.Rows, []map[string]any{{"__addp_output_invalid_0": 8, "email": "a*b"}}) {
		t.Fatal("private flag stripping mixed field validity or removed a user column")
	}
	if !reflect.DeepEqual(result.Fields, []datatype.FieldInfo{{Name: "__addp_output_invalid_0", Type: datatype.FieldTypeBigInt}, {Name: "phone", Type: datatype.FieldTypeString}, {Name: "email", Type: datatype.FieldTypeString}}) {
		t.Fatal("private metadata escaped or actual output types changed")
	}
}

func TestPostgresOutputRewriteRejectsMissingOrInconsistentOutputTypes(t *testing.T) {
	plan, err := rewritePostgresQueryOutput(`SELECT phone FROM people`, nil, []plugin.QueryOutputTransform{{Column: "phone", Kind: "keep_prefix_suffix", Prefix: 1, Suffix: 1, MaskRune: "*", InvalidValue: "omit"}})
	if err != nil {
		t.Fatal(err)
	}
	flag := plan.invalid[0].flag
	for _, fields := range [][]datatype.FieldInfo{
		nil,
		{{Name: "different", Type: datatype.FieldTypeString}, {Name: flag, Type: datatype.FieldTypeBool}},
		{{Name: "phone", Type: datatype.FieldTypeString}, {Name: flag, Type: datatype.FieldTypeString}},
	} {
		result := &plugin.QueryResult{Columns: []string{"phone", flag}, Fields: fields, Rows: []map[string]any{{"phone": "a*c", flag: false}}}
		if !errors.Is(plan.finish(result), errPostgresOutputRewriteUnsupported) {
			t.Fatal("missing, misnamed or incorrectly typed metadata must reject")
		}
	}
}
