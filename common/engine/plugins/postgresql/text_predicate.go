package postgresql

import "github.com/addp/common/engine/plugin"

type textPredicateDialect struct{}

func (textPredicateDialect) Contains(value, substring string) string {
	return "(pg_catalog.strpos(CAST(" + value + " AS text) COLLATE \"C\", CAST(" + substring + " AS text) COLLATE \"C\") > 0)"
}

func (p *PostgreSQLPlugin) TextPredicateDialect() plugin.TextPredicateDialect {
	if p.identity != nil {
		return nil
	}
	return textPredicateDialect{}
}
