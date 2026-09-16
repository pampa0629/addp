package plan

import (
	"encoding/json"
	"fmt"

	"github.com/addp/common/datatype"
)

// AnalyzeExpression reuses the full plan's expression/type validator. The
// scope contains only directly visible logical inputs; native bindings belong
// to the compiler. Unused declarations are allowed here because one expression
// need not consume every parameter of the enclosing plan.
func AnalyzeExpression(e Expr, scope map[NodeID][]datatype.FieldInfo, parameters []Parameter) (datatype.FieldInfo, error) {
	bad := func(err error) (datatype.FieldInfo, error) { return datatype.FieldInfo{}, err }
	input := struct {
		Expr       Expr
		Scope      map[NodeID][]datatype.FieldInfo
		Parameters []Parameter
	}{e, scope, parameters}
	if err := boundedValue(input); err != nil {
		return bad(err)
	}
	if len(scope) > MaxNodes {
		return bad(fmt.Errorf("expression scope exceeds budget"))
	}
	for id, fields := range scope {
		if !Symbol(string(id)) {
			return bad(fmt.Errorf("invalid expression input"))
		}
		if err := validateFields(fields); err != nil {
			return bad(err)
		}
	}
	params, err := validateParameters(parameters)
	if err != nil {
		return bad(err)
	}
	v := validator{parameters: params, used: map[string]bool{}}
	f, err := v.expr(e, scope, 0)
	if err != nil {
		return bad(err)
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > MaxBytes {
		return bad(fmt.Errorf("expression encoding exceeds budget"))
	}
	return f, nil
}
