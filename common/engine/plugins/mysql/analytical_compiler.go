package mysql

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

var _ plugin.AnalyticalCompilerProvider = (*MySQLPlugin)(nil)

func (p *MySQLPlugin) AnalyticalCompiler() plugin.AnalyticalCompiler {
	return sqlcompile.RelationalCompiler{CompilerID: plugin.CompilerIdentity{ID: "mysql.relational_analytics", Version: "1"}, Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}, Scan: analyticalScanDialect{}}
}
