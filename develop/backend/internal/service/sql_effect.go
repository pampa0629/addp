package service

import commonquery "github.com/addp/common/query"

type SQLExecutionEffect = commonquery.Effect

const (
	SQLExecutionEffectRead           = commonquery.Read
	SQLExecutionEffectWrite          = commonquery.Write
	SQLExecutionEffectDDL            = commonquery.DDL
	SQLExecutionEffectExternalEffect = commonquery.ExternalEffect
)

func ClassifySQLExecutionEffect(sql string) (SQLExecutionEffect, error) {
	return commonquery.Classify(sql)
}
