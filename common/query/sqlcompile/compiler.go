package sqlcompile

import (
	"errors"

	"github.com/addp/common/engine/plugin"
)

// RelationalCompiler composes native primitives into one deterministic compiler.
// It performs no connection or instance discovery.
type RelationalCompiler struct {
	CompilerID plugin.CompilerIdentity
	Expression ExpressionDialect
	Result     ResultDialect
	Scan       ScanDialect
}

func (c RelationalCompiler) Identity() plugin.CompilerIdentity {
	return c.CompilerID
}
func (c RelationalCompiler) Check(r plugin.CompileRequest) (plugin.SupportReport, error) {
	_, err := CompileRelations(r, c.Expression, c.Result, c.Scan)
	if errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		return plugin.SupportReport{Diagnostics: []plugin.SupportDiagnostic{{Code: "unsupported_relation_plan"}}}, nil
	}
	return plugin.SupportReport{Supported: err == nil}, err
}
func (c RelationalCompiler) Compile(r plugin.CompileRequest) (plugin.CompiledQuery, error) {
	if err := r.Validate(); err != nil {
		return plugin.CompiledQuery{}, err
	}
	rendered, err := CompileRelations(r, c.Expression, c.Result, c.Scan)
	if err != nil {
		return plugin.CompiledQuery{}, err
	}
	return plugin.NewCompiledQuery(r, c.Identity(), "sql", rendered.SQL, rendered.Evaluations)
}
