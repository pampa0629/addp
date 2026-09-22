package tidb

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/analytical"
)

var _ plugin.AnalyticalCompilerProvider = (*Plugin)(nil)

func (p *Plugin) AnalyticalCompiler() plugin.AnalyticalCompiler {
	return analytical.NewMySQLCompatibleCompiler(analytical.MySQLCompatibleCompilerOptions{
		CompilerID:     plugin.CompilerIdentity{ID: "tidb.relational_analytics", Version: "1"},
		CatalogModel:   p.EngineCatalogModel(),
		IsSystemSchema: p.isSystemSchema,
	})
}
