package mysql

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/analytical"
)

func (p *MySQLPlugin) TextPredicateDialect() plugin.TextPredicateDialect {
	return analytical.MySQLCompatibleExpressionDialect{}
}
