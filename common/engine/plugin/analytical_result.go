package plugin

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query/plan"
)

var ErrAnalyticalResultInvalid = errors.New("invalid analytical result")
var ErrAnalyticalAssertion = errors.New("analytical assertion failed")
var ErrAnalyticalEvaluation = errors.New("analytical evaluation failed")

// EvaluationCheck is compiler-derived, not an owner business assertion.
type EvaluationCheck struct {
	Node plan.NodeID
	Code string
}

type AnalyticalEvaluationError struct{ Check EvaluationCheck }

func (e *AnalyticalEvaluationError) Error() string {
	return "analytical evaluation failed: " + e.Check.Code
}
func (e *AnalyticalEvaluationError) Unwrap() error { return ErrAnalyticalEvaluation }

// AnalyticalAssertionError contains only a plan-declared code, never source data.
type AnalyticalAssertionError struct{ Code string }

func (e *AnalyticalAssertionError) Error() string { return "analytical assertion failed: " + e.Code }
func (e *AnalyticalAssertionError) Unwrap() error { return ErrAnalyticalAssertion }

// AnalyticalResultLayout describes internal provider records, not owner output.
// Each assertion emits exactly one ok:<index> or fail:<index> record, regardless
// of root cardinality. One complete record is mandatory even without assertions.
// Data records carry "data". All other records have NULL business fields.
type AnalyticalResultLayout struct {
	ControlColumn string
	Assertions    []plan.Assertion
	Evaluations   []EvaluationCheck
}

func NewAnalyticalResultLayout(p plan.Plan, evaluations []EvaluationCheck) (AnalyticalResultLayout, error) {
	if err := plan.Validate(p); err != nil {
		return AnalyticalResultLayout{}, ErrAnalyticalInvalid
	}
	names := make(map[string]bool, len(p.Output.Fields))
	for _, f := range p.Output.Fields {
		names[strings.ToLower(f.Name)] = true
	}
	name := "__addp_record"
	for i := 0; names[strings.ToLower(name)]; i++ {
		name = "__addp_record_" + strconv.Itoa(i)
	}
	assertions := append([]plan.Assertion(nil), p.Assertions...)
	slices.SortFunc(assertions, func(a, b plan.Assertion) int {
		if d := strings.Compare(a.Code, b.Code); d != 0 {
			return d
		}
		return strings.Compare(string(a.Violation), string(b.Violation))
	})
	if len(evaluations) > plan.MaxNodes {
		return AnalyticalResultLayout{}, ErrAnalyticalInvalid
	}
	nodes := map[plan.NodeID]bool{}
	for _, node := range p.Nodes {
		nodes[node.ID] = true
	}
	seen := map[EvaluationCheck]bool{}
	for _, check := range evaluations {
		if !nodes[check.Node] || !plan.Symbol(check.Code) || seen[check] {
			return AnalyticalResultLayout{}, ErrAnalyticalInvalid
		}
		seen[check] = true
	}
	evaluations = append([]EvaluationCheck(nil), evaluations...)
	slices.SortFunc(evaluations, func(a, b EvaluationCheck) int {
		if d := strings.Compare(string(a.Node), string(b.Node)); d != 0 {
			return d
		}
		return strings.Compare(a.Code, b.Code)
	})
	return AnalyticalResultLayout{ControlColumn: name, Assertions: assertions, Evaluations: evaluations}, nil
}

func (q CompiledQuery) ResultLayout() AnalyticalResultLayout {
	l := q.layout
	l.Assertions = append([]plan.Assertion(nil), l.Assertions...)
	l.Evaluations = append([]EvaluationCheck(nil), l.Evaluations...)
	return l
}

// QueryRequest supplies values, never native syntax or caller pagination. The
// private compiled value survives request copies and is checked at SQL Prepare.
func (q CompiledQuery) QueryRequest(values map[string]plan.Literal, timeout time.Duration) (QueryRequest, error) {
	if q.fingerprint == "" || timeout < 0 {
		return QueryRequest{}, ErrAnalyticalInvalid
	}
	parameters := make(map[string]interface{}, len(q.parameters))
	declared := make(map[string]bool, len(q.parameters))
	for _, p := range q.parameters {
		declared[p.Name] = true
		v, exists := values[p.Name]
		if !exists {
			v = plan.Literal{Type: p.Type, Null: true}
		}
		v, err := v.Canonical()
		if err != nil || v.Type != p.Type || (v.Null && p.Required) {
			return QueryRequest{}, ErrAnalyticalInvalid
		}
		if !v.Null && len(p.Allowed) > 0 && !slices.Contains(p.Allowed, v) {
			return QueryRequest{}, ErrAnalyticalInvalid
		}
		parameters[p.Name], err = analyticalLiteralValue(v)
		if err != nil {
			return QueryRequest{}, err
		}
	}
	for name := range values {
		if !declared[name] {
			return QueryRequest{}, ErrAnalyticalInvalid
		}
	}
	return QueryRequest{EngineID: q.engineID, Language: q.language, Query: q.template,
		Options: QueryOptions{ReadOnly: true, Timeout: timeout, Parameters: parameters}, analytical: &q}, nil
}

func validateAnalyticalQueryRequest(provider SQLQueryRuntimeProvider, r QueryRequest) error {
	q := r.analytical
	if q == nil {
		return nil
	}
	p, ok := provider.(AnalyticalCompilerProvider)
	if !ok {
		return ErrAnalyticalPlanChanged
	}
	compiler := p.AnalyticalCompiler()
	if compiler == nil || compiler.Identity() != q.compiler {
		return ErrAnalyticalPlanChanged
	}
	if r.EngineID != q.engineID || r.Query != q.template || r.Language != q.language || r.TargetPath != nil ||
		!r.Options.ReadOnly || r.Options.Timeout < 0 || r.Options.Limit != 0 || r.Options.Offset != 0 || r.Options.Describe || r.Options.Spatial ||
		len(r.Options.Args) != 0 || (r.Options.EngineType != "" && r.Options.EngineType != provider.Type()) ||
		(r.Options.EngineID != 0 && r.Options.EngineID != q.engineID) {
		return ErrAnalyticalInvalid
	}
	// Values are public QueryOptions fields. Revalidate them after any caller edits.
	if len(r.Options.Parameters) != len(q.parameters) {
		return ErrAnalyticalInvalid
	}
	for _, p := range q.parameters {
		raw, exists := r.Options.Parameters[p.Name]
		if !exists {
			return ErrAnalyticalInvalid
		}
		v, err := analyticalValueLiteral(p.Type, raw)
		if err != nil || (v.Null && p.Required) || (!v.Null && len(p.Allowed) > 0 && !slices.Contains(p.Allowed, v)) {
			return ErrAnalyticalInvalid
		}
		canonical, err := analyticalLiteralValue(v)
		if err != nil || !reflect.DeepEqual(raw, canonical) {
			return ErrAnalyticalInvalid
		}
	}
	return nil
}

func (q CompiledQuery) normalizeResult(raw *QueryResult) (*QueryResult, error) {
	columns := make([]string, 0, len(q.output.Fields)+1)
	for _, f := range q.output.Fields {
		columns = append(columns, f.Name)
	}
	columns = append(columns, q.layout.ControlColumn)
	if raw == nil || !slices.Equal(raw.Columns, columns) {
		return nil, ErrAnalyticalResultInvalid
	}
	seen := map[string]bool{}
	expected := map[string]bool{}
	for i := range q.layout.Assertions {
		expected["assert:"+strconv.Itoa(i+1)] = true
	}
	for i := range q.layout.Evaluations {
		expected["eval:"+strconv.Itoa(i+1)] = true
	}
	failed := map[string]bool{}
	result := &QueryResult{Columns: columns[:len(columns)-1], Rows: make([]map[string]interface{}, 0)}
	for _, row := range raw.Rows {
		if len(row) != len(columns) {
			return nil, ErrAnalyticalResultInvalid
		}
		for _, c := range columns {
			if _, exists := row[c]; !exists {
				return nil, ErrAnalyticalResultInvalid
			}
		}
		marker, ok := row[q.layout.ControlColumn].(string)
		if !ok {
			return nil, ErrAnalyticalResultInvalid
		}
		if marker == "data" {
			continue
		}
		key := marker
		if marker != "complete" {
			prefix, code, found := strings.Cut(marker, ":")
			if !found || (prefix != "ok" && prefix != "fail" && prefix != "eval_ok" && prefix != "eval_fail") {
				return nil, ErrAnalyticalResultInvalid
			}
			key = "assert:" + code
			if strings.HasPrefix(prefix, "eval_") {
				key = "eval:" + code
			}
			if !expected[key] {
				return nil, ErrAnalyticalResultInvalid
			}
			failed[key] = prefix == "fail" || prefix == "eval_fail"
		}
		if seen[key] {
			return nil, ErrAnalyticalResultInvalid
		}
		seen[key] = true
		for _, f := range q.output.Fields {
			if row[f.Name] != nil {
				return nil, ErrAnalyticalResultInvalid
			}
		}
	}
	if !seen["complete"] {
		return nil, ErrAnalyticalResultInvalid
	}
	for key := range expected {
		if !seen[key] {
			return nil, ErrAnalyticalResultInvalid
		}
	}
	for i, check := range q.layout.Evaluations {
		if failed["eval:"+strconv.Itoa(i+1)] {
			return nil, &AnalyticalEvaluationError{Check: check}
		}
	}
	for i, a := range q.layout.Assertions {
		if failed["assert:"+strconv.Itoa(i+1)] {
			return nil, &AnalyticalAssertionError{Code: a.Code}
		}
	}
	for _, row := range raw.Rows {
		if row[q.layout.ControlColumn] != "data" {
			continue
		}
		out := make(map[string]interface{}, len(q.output.Fields))
		for _, f := range q.output.Fields {
			v, err := analyticalValueLiteral(f.Type, row[f.Name])
			if err != nil || (v.Null && !f.Nullable) {
				return nil, ErrAnalyticalResultInvalid
			}
			out[f.Name], err = analyticalLiteralValue(v)
			if err != nil {
				return nil, ErrAnalyticalResultInvalid
			}
		}
		result.Rows = append(result.Rows, out)
	}
	return result, nil
}

// No float path: drivers must preserve exact decimal/integer values. Date
// values are interpreted in their own calendar, never converted across zones.
func analyticalValueLiteral(typ datatype.FieldType, raw interface{}) (plan.Literal, error) {
	v := plan.Literal{Type: typ, Null: raw == nil}
	if raw == nil {
		return v.Canonical()
	}
	switch x := raw.(type) {
	case string:
		v.Text = x
	case []byte:
		v.Text = string(x)
	case int64:
		if typ != datatype.FieldTypeInt && typ != datatype.FieldTypeBigInt && typ != datatype.FieldTypeDecimal && typ != datatype.FieldTypeBool {
			return v, ErrAnalyticalResultInvalid
		}
		v.Text = strconv.FormatInt(x, 10)
	case bool:
		if typ != datatype.FieldTypeBool {
			return v, ErrAnalyticalResultInvalid
		}
		v.Text = strconv.FormatBool(x)
	case time.Time:
		if typ != datatype.FieldTypeDate || x.Hour() != 0 || x.Minute() != 0 || x.Second() != 0 || x.Nanosecond() != 0 {
			return v, ErrAnalyticalResultInvalid
		}
		v.Text = x.Format("2006-01-02")
	default:
		return v, ErrAnalyticalResultInvalid
	}
	if typ == datatype.FieldTypeBool {
		if v.Text == "1" {
			v.Text = "true"
		}
		if v.Text == "0" {
			v.Text = "false"
		}
	}
	return v.Canonical()
}

func analyticalLiteralValue(v plan.Literal) (interface{}, error) {
	if v.Null {
		return nil, nil
	}
	switch v.Type {
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		return strconv.ParseInt(v.Text, 10, 64)
	case datatype.FieldTypeBool:
		return strconv.ParseBool(v.Text)
	case datatype.FieldTypeDecimal:
		integer, fraction, _ := strings.Cut(v.Text, ".")
		return integer + "." + fraction + strings.Repeat("0", 18-len(fraction)), nil
	case datatype.FieldTypeString, datatype.FieldTypeDate:
		return v.Text, nil
	default:
		return nil, fmt.Errorf("%w: scalar type", ErrAnalyticalInvalid)
	}
}

// Retain control-field dependencies as derived dependencies with no published
// output path. Never omit assertion-only sources from the provider's lineage.
func (q CompiledQuery) normalizeLineage(l *QueryOutputLineage) *QueryOutputLineage {
	l = l.Clone()
	if l == nil {
		return nil
	}
	for i := range l.Sources {
		for j := range l.Sources[i].Bindings {
			b := &l.Sources[i].Bindings[j]
			if len(b.OutputPath) > 0 && b.OutputPath[0] == q.layout.ControlColumn {
				b.OutputPath = nil
				b.Transformation = QueryOutputTransformationDerived
			}
		}
	}
	return l
}
