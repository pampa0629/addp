package postgresql

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

var _ plugin.AnalyticalCompilerProvider = (*PostgreSQLPlugin)(nil)

func (p *PostgreSQLPlugin) AnalyticalCompiler() plugin.AnalyticalCompiler {
	if p.identity != nil {
		return nil
	}
	return sqlcompile.RelationalCompiler{CompilerID: plugin.CompilerIdentity{ID: "postgresql.relational_analytics", Version: "1"}, Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}, Scan: analyticalScanDialect{}}
}
