package semantic

import (
	"fmt"
	"slices"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/operators"
	"cel.dev/cel-go/common/types"
)

type compiledRule struct {
	definition Rule
	properties map[string]Property
	env        *cel.Env
	program    cel.Program
}

func compile(r Rule, properties map[string]Property) (*compiledRule, error) {
	opts := []cel.EnvOption{cel.ClearMacros(), cel.ParserRecursionLimit(32), cel.ParserExpressionSizeLimit(maxText)}
	declared := map[string]bool{}
	enums := map[string][]string{}
	for _, input := range r.Inputs {
		typ := cel.StringType
		if properties[input.PropertyID].Kind == Boolean {
			typ = cel.BoolType
		}
		opts = append(opts, cel.Variable(input.Variable, typ))
		declared[input.Variable] = true
		enums[input.Variable] = properties[input.PropertyID].Enum
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	parsed, issues := env.Parse(r.Expression)
	if issues.Err() != nil {
		return nil, fmt.Errorf("invalid_expression: %w", issues.Err())
	}
	used := map[string]bool{}
	nodes := 0
	if err := checkExpression(parsed.NativeRep().Expr(), declared, used, &nodes, 0); err != nil {
		return nil, err
	}
	if err := checkEnumLiterals(parsed.NativeRep().Expr(), enums); err != nil {
		return nil, err
	}
	for name := range declared {
		if !used[name] {
			return nil, fmt.Errorf("unused_input: %s", name)
		}
	}
	checked, issues := env.Check(parsed)
	if issues.Err() != nil {
		return nil, fmt.Errorf("invalid_expression_type: %w", issues.Err())
	}
	if checked.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("non_boolean_rule")
	}
	program, err := env.Program(checked, cel.EvalOptions(cel.OptPartialEval), cel.CostLimit(maxCost), cel.InterruptCheckFrequency(1))
	if err != nil {
		return nil, err
	}
	return &compiledRule{r, properties, env, program}, nil
}

// Check the parsed tree before type checking: the CEL environment contains
// builtins that are deliberately not part of ADDP's v1 language contract.
func checkExpression(e ast.Expr, declared, used map[string]bool, nodes *int, depth int) error {
	*nodes += 1
	if *nodes > 256 || depth > 32 {
		return fmt.Errorf("expression_complexity_limit")
	}
	switch e.Kind() {
	case ast.LiteralKind:
		if e.AsLiteral().Type() != types.StringType && e.AsLiteral().Type() != types.BoolType {
			return fmt.Errorf("unsupported_literal")
		}
	case ast.IdentKind:
		name := e.AsIdent()
		if !declared[name] {
			return fmt.Errorf("undeclared_input: %s", name)
		}
		used[name] = true
	case ast.CallKind:
		call := e.AsCall()
		if call.IsMemberFunction() {
			return fmt.Errorf("unsupported_function")
		}
		args := call.Args()
		switch call.FunctionName() {
		case operators.LogicalNot:
			if len(args) != 1 {
				return fmt.Errorf("invalid_arity")
			}
		case operators.LogicalAnd, operators.LogicalOr, operators.Equals, operators.NotEquals:
			if len(args) != 2 {
				return fmt.Errorf("invalid_arity")
			}
		case operators.In:
			if len(args) != 2 || args[1].Kind() != ast.ListKind {
				return fmt.Errorf("literal_set_required")
			}
			list := args[1].AsList()
			if list.Size() == 0 || list.Size() > 64 || len(list.OptionalIndices()) != 0 {
				return fmt.Errorf("invalid_literal_set")
			}
			seen := map[string]bool{}
			for _, element := range list.Elements() {
				if element.Kind() != ast.LiteralKind || element.AsLiteral().Type() != types.StringType {
					return fmt.Errorf("string_set_required")
				}
				value := string(element.AsLiteral().(types.String))
				if seen[value] {
					return fmt.Errorf("duplicate_set_member")
				}
				seen[value] = true
			}
			*nodes += 1 + list.Size()
			if *nodes > 256 {
				return fmt.Errorf("expression_complexity_limit")
			}
			return checkExpression(args[0], declared, used, nodes, depth+1)
		default:
			return fmt.Errorf("unsupported_function: %s", call.FunctionName())
		}
		for _, arg := range args {
			if err := checkExpression(arg, declared, used, nodes, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported_expression")
	}
	return nil
}

// Enum facts and literal conditions share one value domain. A typo in a finite
// value must fail compilation, rather than publish an always-false predicate.
// Called only after the v1 grammar check has established the tree shape.
func checkEnumLiterals(e ast.Expr, enums map[string][]string) error {
	if e.Kind() != ast.CallKind {
		return nil
	}
	call := e.AsCall()
	args := call.Args()
	check := func(variable, literal ast.Expr) error {
		if variable.Kind() != ast.IdentKind || literal.Kind() != ast.LiteralKind || literal.AsLiteral().Type() != types.StringType {
			return nil
		}
		allowed := enums[variable.AsIdent()]
		if len(allowed) > 0 && !slices.Contains(allowed, string(literal.AsLiteral().(types.String))) {
			return fmt.Errorf("unknown_enum_literal: %s", variable.AsIdent())
		}
		return nil
	}
	switch call.FunctionName() {
	case operators.Equals, operators.NotEquals:
		if err := check(args[0], args[1]); err != nil {
			return err
		}
		if err := check(args[1], args[0]); err != nil {
			return err
		}
	case operators.In:
		for _, literal := range args[1].AsList().Elements() {
			if err := check(args[0], literal); err != nil {
				return err
			}
		}
	}
	for _, arg := range args {
		if err := checkEnumLiterals(arg, enums); err != nil {
			return err
		}
	}
	return nil
}
