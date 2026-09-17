package mysql

import "github.com/addp/common/engine/plugin"

type textPredicateDialect struct{}

func (textPredicateDialect) Contains(value, substring string) string {
	return "(LOCATE(CAST(" + substring + " AS binary), CAST(" + value + " AS binary)) > 0)"
}

func (p *MySQLPlugin) TextPredicateDialect() plugin.TextPredicateDialect {
	return textPredicateDialect{}
}
