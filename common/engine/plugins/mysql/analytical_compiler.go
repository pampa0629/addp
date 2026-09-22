package mysql

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/analytical"
)

var _ plugin.AnalyticalCompilerProvider = (*MySQLPlugin)(nil)

func (p *MySQLPlugin) AnalyticalCompiler() plugin.AnalyticalCompiler {
	return analytical.NewMySQLCompatibleCompiler(analytical.MySQLCompatibleCompilerOptions{
		CompilerID:     plugin.CompilerIdentity{ID: "mysql.relational_analytics", Version: "1"},
		CatalogModel:   p.EngineCatalogModel(),
		IsSystemSchema: p.isSystemSchema,
	})
}
