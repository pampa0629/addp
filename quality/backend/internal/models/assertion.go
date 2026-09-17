package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

// AssertionExpr is a bounded, target-independent quality expression, not SQL.
type AssertionExpr struct {
	Op       string          `json:"op"`
	Args     []AssertionExpr `json:"args,omitempty"`
	Relation string          `json:"relation,omitempty"`
	Field    string          `json:"field,omitempty"`
	Value    json.RawMessage `json:"value,omitempty" swaggertype:"object"`
	Where    *AssertionExpr  `json:"where,omitempty"`
}

type AssertionConstraint struct {
	Assertion AssertionExpr `json:"assertion"`
}

type RelationBinding struct {
	Table  string            `json:"table"`
	Fields map[string]string `json:"fields"`
}

// AssertionInputs includes the root row under the empty role name.
type AssertionInputs map[string]map[string]bool

var assertionSymbol = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var assertionNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,29})(\.[0-9]{1,18})?$`)

func ParseAssertionConstraint(raw json.RawMessage) (AssertionConstraint, AssertionInputs, error) {
	var constraint AssertionConstraint
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&constraint); err != nil {
		return constraint, nil, err
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return constraint, nil, fmt.Errorf("invalid trailing assertion data")
	}
	inputs, err := constraint.Assertion.Inputs()
	return constraint, inputs, err
}

func (root AssertionExpr) Inputs() (AssertionInputs, error) {
	inputs := AssertionInputs{"": {}}
	nodes := 0
	var visit func(AssertionExpr, map[string]bool, int) (string, error)
	visit = func(e AssertionExpr, scope map[string]bool, depth int) (string, error) {
		nodes++
		if nodes > 128 || depth > 12 {
			return "", fmt.Errorf("assertion exceeds size or depth limit")
		}
		field := func(role, name string) error {
			if !assertionSymbol.MatchString(name) || (role != "" && !scope[role]) {
				return fmt.Errorf("invalid field symbol or relation scope")
			}
			inputs[role][name] = true
			return nil
		}
		collection := e.Op == "exists" || e.Op == "count" || e.Op == "count_distinct"
		if (e.Relation != "" && e.Op != "field" && !collection) || (e.Field != "" && e.Op != "field" && e.Op != "count_distinct") || (len(e.Value) != 0 && e.Op != "value" && e.Op != "number") || (e.Where != nil && !collection) {
			return "", fmt.Errorf("unexpected assertion operand")
		}
		if collection {
			if len(e.Args) != 0 || !assertionSymbol.MatchString(e.Relation) || scope[e.Relation] {
				return "", fmt.Errorf("invalid or shadowed relation role")
			}
			if inputs[e.Relation] == nil {
				inputs[e.Relation] = map[string]bool{}
			}
			if len(inputs) > 9 {
				return "", fmt.Errorf("too many assertion relations")
			}
			next := map[string]bool{}
			for role := range scope {
				next[role] = true
			}
			next[e.Relation] = true
			if e.Op == "count_distinct" {
				if !assertionSymbol.MatchString(e.Field) {
					return "", fmt.Errorf("invalid distinct field")
				}
				inputs[e.Relation][e.Field] = true
			}
			if e.Where != nil {
				kind, err := visit(*e.Where, next, depth+1)
				if err != nil {
					return "", err
				}
				if kind != "bool" {
					return "", fmt.Errorf("relation filter must be boolean")
				}
			}
			if e.Op == "exists" {
				return "bool", nil
			}
			return "number", nil
		}
		arity, kind := 0, ""
		switch e.Op {
		case "field":
			if err := field(e.Relation, e.Field); err != nil {
				return "", err
			}
			kind = "field"
		case "value", "number":
			if len(e.Value) > 4096 {
				return "", fmt.Errorf("assertion literal is too long")
			}
			var value interface{}
			decoder := json.NewDecoder(bytes.NewReader(e.Value))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				return "", fmt.Errorf("literal is required")
			}
			if e.Op == "number" {
				decimal, ok := value.(string)
				if !ok || !assertionNumber.MatchString(decimal) {
					return "", fmt.Errorf("number requires a decimal string with at most 30 integer and 18 fractional digits")
				}
				kind = "number"
				break
			}
			switch value.(type) {
			case nil:
				kind = "null"
			case bool:
				kind = "bool"
			case string:
				kind = "string"
			default:
				return "", fmt.Errorf("value must be text, boolean or null; use number for decimal literals")
			}
		case "today":
			kind = "date"
		case "and", "or":
			arity, kind = -1, "bool"
		case "not", "is_null", "not_null":
			arity, kind = 1, "bool"
		case "eq", "ne", "lt", "lte", "gt", "gte":
			arity, kind = 2, "bool"
		default:
			return "", fmt.Errorf("unsupported assertion operator %q", e.Op)
		}
		if (arity >= 0 && len(e.Args) != arity) || (arity == -1 && (len(e.Args) < 2 || len(e.Args) > 32)) {
			return "", fmt.Errorf("invalid assertion argument count")
		}
		kinds := make([]string, len(e.Args))
		for i, arg := range e.Args {
			var err error
			kinds[i], err = visit(arg, scope, depth+1)
			if err != nil {
				return "", err
			}
			if (e.Op == "and" || e.Op == "or" || e.Op == "not") && kinds[i] != "bool" {
				return "", fmt.Errorf("boolean argument required")
			}
		}
		if arity == 2 && kinds[0] != kinds[1] && kinds[0] != "field" && kinds[1] != "field" && kinds[0] != "null" && kinds[1] != "null" {
			return "", fmt.Errorf("incompatible assertion operands")
		}
		return kind, nil
	}
	kind, err := visit(root, map[string]bool{}, 1)
	if err != nil {
		return nil, err
	}
	if kind != "bool" {
		return nil, fmt.Errorf("assertion root must be boolean")
	}
	return inputs, nil
}
