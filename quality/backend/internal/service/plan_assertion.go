package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/quality/internal/models"
)

type planAssertionParams struct {
	Table     string                            `json:"table"`
	Fields    map[string]string                 `json:"fields,omitempty"`
	Relations map[string]models.RelationBinding `json:"relations,omitempty"`
	Assertion models.AssertionExpr              `json:"assertion"`
}

func (p planAssertionParams) validate(aliases map[string]struct{}) error {
	inputs, err := p.Assertion.Inputs()
	if err != nil {
		return err
	}
	if len(p.Relations) != len(inputs)-1 {
		return fmt.Errorf("assertion relation bindings do not match inputs")
	}
	for role, symbols := range inputs {
		binding := models.RelationBinding{Table: p.Table, Fields: p.Fields}
		if role != "" {
			binding = p.Relations[role]
		}
		if _, ok := aliases[binding.Table]; !ok {
			return fmt.Errorf("assertion table alias is not bound")
		}
		if len(binding.Fields) != len(symbols) {
			return fmt.Errorf("assertion fields do not match inputs")
		}
		for symbol := range symbols {
			column := binding.Fields[symbol]
			if column == "" || len(column) > 200 || strings.TrimSpace(column) != column {
				return fmt.Errorf("assertion field binding is missing or invalid")
			}
		}
	}
	return nil
}

// compileAssertion only emits operators from the validated finite vocabulary.
// Physical identifiers use the same quoting and catalog checks as other rules.
func compileAssertion(p planAssertionParams, tableSQL func(string) (string, map[string]struct{}, error), columnSQL func(map[string]struct{}, string) (string, error)) (planRowQuery, []interface{}, error) {
	aliases := map[string]struct{}{p.Table: {}}
	for _, b := range p.Relations {
		aliases[b.Table] = struct{}{}
	}
	if err := p.validate(aliases); err != nil {
		return planRowQuery{}, nil, err
	}
	bindings := map[string]models.RelationBinding{"": {Table: p.Table, Fields: p.Fields}}
	for role, binding := range p.Relations {
		bindings[role] = binding
	}
	tables := map[string]string{}
	columns := map[string]map[string]string{}
	for role, binding := range bindings {
		table, available, err := tableSQL(binding.Table)
		if err != nil {
			return planRowQuery{}, nil, err
		}
		tables[role], columns[role] = table, map[string]string{}
		for symbol, column := range binding.Fields {
			quoted, err := columnSQL(available, column)
			if err != nil {
				return planRowQuery{}, nil, err
			}
			columns[role][symbol] = quoted
		}
	}
	args := []interface{}{}
	sequence := 0
	var emit func(models.AssertionExpr, map[string]string) string
	emit = func(e models.AssertionExpr, scope map[string]string) string {
		switch e.Op {
		case "field":
			return scope[e.Relation] + "." + columns[e.Relation][e.Field]
		case "today":
			return "CURRENT_DATE"
		case "value", "number":
			var value interface{}
			decoder := json.NewDecoder(strings.NewReader(string(e.Value)))
			decoder.UseNumber()
			_ = decoder.Decode(&value)
			// Decimal literals stay strings through JSONMap snapshots and browser
			// round trips, so no IEEE-754 conversion can alter a frozen constraint.
			cast := ""
			switch value.(type) {
			case bool:
				cast = "::boolean"
			case string:
				cast = "::text"
			}
			if e.Op == "number" {
				cast = "::numeric"
			}
			args = append(args, value)
			return fmt.Sprintf("$%d%s", len(args), cast)
		case "exists", "count", "count_distinct":
			sequence++
			alias := fmt.Sprintf("q%d", sequence)
			next := map[string]string{}
			for role, value := range scope {
				next[role] = value
			}
			next[e.Relation] = alias
			where := "TRUE"
			if e.Where != nil {
				where = emit(*e.Where, next)
			}
			from := " FROM " + tables[e.Relation] + " AS " + alias + " WHERE (" + where + ") IS TRUE"
			if e.Op == "exists" {
				return "EXISTS (SELECT 1" + from + ")"
			}
			count := "*"
			if e.Op == "count_distinct" {
				count = "DISTINCT " + alias + "." + columns[e.Relation][e.Field]
			}
			return "(SELECT COUNT(" + count + ")" + from + ")"
		}
		parts := make([]string, len(e.Args))
		for i, child := range e.Args {
			parts[i] = emit(child, scope)
		}
		switch e.Op {
		case "not":
			return "(NOT (" + parts[0] + "))"
		case "is_null":
			return "(" + parts[0] + " IS NULL)"
		case "not_null":
			return "(" + parts[0] + " IS NOT NULL)"
		case "and":
			return "(" + strings.Join(parts, " AND ") + ")"
		case "or":
			return "(" + strings.Join(parts, " OR ") + ")"
		default:
			op := map[string]string{"eq": "IS NOT DISTINCT FROM", "ne": "IS DISTINCT FROM", "lt": "<", "lte": "<=", "gt": ">", "gte": ">="}[e.Op]
			return "(" + parts[0] + " " + op + " " + parts[1] + ")"
		}
	}
	assertion := emit(p.Assertion, map[string]string{"": "q0"})
	return planRowQuery{From: tables[""] + " AS q0", Failure: "(" + assertion + ") IS NOT TRUE", Qualifier: "q0."}, args, nil
}
